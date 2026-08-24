package arena

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
)

// DefaultChunkBytes is the chunk size the platform currently returns (64 MiB).
// The server's value always wins; this only exists so --dry-run can plan an
// upload without opening a session.
const DefaultChunkBytes int64 = 64 * 1024 * 1024

// UploadRequest declares what an artifact supports.
//
// Games and PlayerCounts are frozen into the resulting immutable version, and
// the counts are exact — declaring 4 does not imply 3.
type UploadRequest struct {
	Name         string   `json:"name"`
	Games        []string `json:"games"`
	PlayerCounts []int    `json:"playerCounts"`
	Size         int64    `json:"size"`
}

// UploadSession is the response to opening an upload.
type UploadSession struct {
	UploadID    string `json:"uploadId"`
	ChunkBytes  int64  `json:"chunkBytes"`
	TotalChunks int    `json:"totalChunks"`
}

// ChunkAck acknowledges one accepted chunk.
type ChunkAck struct {
	ReceivedBytes int64 `json:"receivedBytes"`
	NextChunk     int   `json:"nextChunk"`
}

// Validate checks an upload request against the rules the platform enforces,
// plus this harness's own naming convention, so an obviously bad request never
// costs a round trip. UploadArtifact calls it too, so no CLI flag can route
// around it.
func (r UploadRequest) Validate() error {
	if r.Name == "" {
		return fmt.Errorf("bot name is required")
	}
	if len(r.Games) == 0 {
		return fmt.Errorf("declare at least one game")
	}
	if len(r.PlayerCounts) == 0 {
		return fmt.Errorf("declare at least one table size")
	}
	for _, count := range r.PlayerCounts {
		if count < 2 || count > 6 {
			return fmt.Errorf("table size %d is out of range (the arena seats 2 to 6)", count)
		}
	}
	if r.Size <= 0 {
		return fmt.Errorf("artifact size must be positive")
	}
	if r.Size > MaxArtifactBytes {
		return fmt.Errorf("artifact is %d bytes, over the 300 MiB limit", r.Size)
	}

	// The name is checked last so the platform's own rules keep their say first.
	// It is ours rather than the server's, but it is the only place a generation
	// can be recorded, so a name that disagrees with what the upload declares is
	// a mistake worth catching before it costs a validation slot.
	name, err := ParseBotName(r.Name)
	if err != nil {
		return err
	}
	if len(r.Games) != 1 {
		return fmt.Errorf("declare exactly one game: %q names %q (see docs/naming.md)", r.Name, name.Game)
	}
	if r.Games[0] != name.Game {
		return fmt.Errorf("%q names game %q, but the upload declares %q", r.Name, name.Game, r.Games[0])
	}
	if duplicate, ok := firstDuplicate(r.PlayerCounts); ok {
		return fmt.Errorf("table size %d is declared twice", duplicate)
	}
	if !sameCounts(r.PlayerCounts, name.Counts()) {
		declared := joinCounts(r.PlayerCounts)
		if token := seatTokenFor(r.PlayerCounts); token != "" {
			return fmt.Errorf("%q says seats %q (%s), but the upload declares %s — that seat set is named %q",
				r.Name, name.Seats, joinCounts(name.Counts()), declared, token)
		}
		return fmt.Errorf("%q says seats %q (%s), but the upload declares %s, which no seat token describes",
			r.Name, name.Seats, joinCounts(name.Counts()), declared)
	}
	return nil
}

// PlanChunks reports how many chunks an upload of size bytes needs.
func PlanChunks(size, chunkBytes int64) int {
	if size <= 0 || chunkBytes <= 0 {
		return 0
	}
	return int((size + chunkBytes - 1) / chunkBytes)
}

// UploadArtifact runs the platform's four-step chunked upload.
//
// On any failure the session is cancelled rather than retried: the server
// discards malformed or interrupted streams, so resuming inside a dead session
// would silently produce a corrupt artifact. The only correct recovery is a
// fresh upload. See docs/arena/http-api.md.
func (c *Client) UploadArtifact(
	ctx context.Context,
	request UploadRequest,
	path string,
	progress func(sent, total int64),
) (json.RawMessage, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open artifact: %w", err)
	}
	defer file.Close()

	session, err := c.openUpload(ctx, request)
	if err != nil {
		return nil, err
	}

	result, err := c.sendChunks(ctx, session, request.Size, file, progress)
	if err != nil {
		// Best effort: the session is already unusable, so a failure to cancel
		// must not mask the original error.
		_ = c.CancelUpload(context.WithoutCancel(ctx), session.UploadID)
		return nil, err
	}
	return result, nil
}

func (c *Client) openUpload(ctx context.Context, request UploadRequest) (*UploadSession, error) {
	var session UploadSession
	if err := c.postJSON(ctx, "/api/bot-uploads", request, &session); err != nil {
		return nil, fmt.Errorf("open upload session: %w", err)
	}
	if session.UploadID == "" {
		return nil, fmt.Errorf("open upload session: server returned no uploadId")
	}
	if session.ChunkBytes <= 0 {
		return nil, fmt.Errorf("open upload session: server returned chunkBytes=%d", session.ChunkBytes)
	}
	return &session, nil
}

func (c *Client) sendChunks(
	ctx context.Context,
	session *UploadSession,
	size int64,
	file io.Reader,
	progress func(sent, total int64),
) (json.RawMessage, error) {
	if progress != nil {
		progress(0, size)
	}

	for index := 0; index < session.TotalChunks; index++ {
		start := int64(index) * session.ChunkBytes
		end := min(start+session.ChunkBytes, size)

		chunk := make([]byte, end-start)
		if _, err := io.ReadFull(file, chunk); err != nil {
			return nil, fmt.Errorf("read chunk %d: %w", index, err)
		}

		ack, err := c.putChunk(ctx, session.UploadID, index, chunk)
		if err != nil {
			return nil, fmt.Errorf("chunk %d: %w", index, err)
		}
		// The platform's own client asserts both of these and aborts otherwise;
		// a mismatch means the server and we disagree about what it holds.
		if ack.ReceivedBytes != end || ack.NextChunk != index+1 {
			return nil, fmt.Errorf(
				"chunk %d: server reported inconsistent progress (receivedBytes=%d want %d, nextChunk=%d want %d)",
				index, ack.ReceivedBytes, end, ack.NextChunk, index+1)
		}
		if progress != nil {
			progress(ack.ReceivedBytes, size)
		}
	}

	var result json.RawMessage
	path := "/api/bot-uploads/" + url.PathEscape(session.UploadID) + "/complete"
	if err := c.postJSON(ctx, path, nil, &result); err != nil {
		return nil, fmt.Errorf("finalize upload: %w", err)
	}
	return result, nil
}

func (c *Client) putChunk(ctx context.Context, uploadID string, index int, chunk []byte) (*ChunkAck, error) {
	var ack ChunkAck
	path := fmt.Sprintf("/api/bot-uploads/%s/chunks/%d", url.PathEscape(uploadID), index)
	err := c.do(ctx, http.MethodPut, path, bytes.NewReader(chunk), "application/octet-stream", &ack)
	if err != nil {
		return nil, err
	}
	return &ack, nil
}

// CancelUpload discards an upload session.
func (c *Client) CancelUpload(ctx context.Context, uploadID string) error {
	path := "/api/bot-uploads/" + url.PathEscape(uploadID)
	return c.do(ctx, http.MethodDelete, path, nil, "", nil)
}
