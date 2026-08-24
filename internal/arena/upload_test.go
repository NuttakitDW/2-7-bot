package arena

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanChunks(t *testing.T) {
	const chunk = DefaultChunkBytes
	tests := []struct {
		name  string
		size  int64
		chunk int64
		want  int
	}{
		{"empty", 0, chunk, 0},
		{"one byte", 1, chunk, 1},
		{"exactly one chunk", chunk, chunk, 1},
		{"one byte over", chunk + 1, chunk, 2},
		{"typical go binary", 2_959_672, chunk, 1},
		{"largest allowed", MaxArtifactBytes, chunk, 5},
		{"negative size", -1, chunk, 0},
		{"zero chunk size", 100, 0, 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := PlanChunks(test.size, test.chunk); got != test.want {
				t.Errorf("PlanChunks(%d, %d) = %d, want %d", test.size, test.chunk, got, test.want)
			}
		})
	}
}

func TestUploadRequestValidate(t *testing.T) {
	valid := UploadRequest{Name: "nutt-27td-fl-hu-h1", Games: []string{"27td-fl"}, PlayerCounts: []int{2}, Size: 100}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}

	tests := []struct {
		name    string
		mutate  func(*UploadRequest)
		wantErr string
	}{
		{"no name", func(r *UploadRequest) { r.Name = "" }, "name is required"},
		{"no games", func(r *UploadRequest) { r.Games = nil }, "at least one game"},
		{"no counts", func(r *UploadRequest) { r.PlayerCounts = nil }, "at least one table size"},
		{"seat count too low", func(r *UploadRequest) { r.PlayerCounts = []int{1} }, "out of range"},
		{"seat count too high", func(r *UploadRequest) { r.PlayerCounts = []int{7} }, "out of range"},
		{"zero size", func(r *UploadRequest) { r.Size = 0 }, "must be positive"},
		{"over limit", func(r *UploadRequest) { r.Size = MaxArtifactBytes + 1 }, "over the 300 MiB limit"},
		{"retired name", func(r *UploadRequest) { r.Name = "nutt-27td-fl" }, "retired"},
		{"another owner", func(r *UploadRequest) { r.Name = "swit-27td-fl-hu-h1" }, `must start with "nutt-"`},
		{"two games", func(r *UploadRequest) { r.Games = []string{"27td-fl", "badugi-fl"} }, "exactly one game"},
		{"game contradicts the name", func(r *UploadRequest) { r.Games = []string{"badugi-fl"} }, "names game \"27td-fl\""},
		{"counts contradict the name", func(r *UploadRequest) { r.PlayerCounts = []int{2, 6} }, `that seat set is named "hu6"`},
		{"counts no token describes", func(r *UploadRequest) { r.PlayerCounts = []int{2, 3} }, "no seat token describes"},
		{"duplicate count", func(r *UploadRequest) { r.PlayerCounts = []int{2, 2} }, "declared twice"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := valid
			test.mutate(&request)
			err := request.Validate()
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), test.wantErr) {
				t.Errorf("error %q does not mention %q", err, test.wantErr)
			}
		})
	}
}

// fakeArena implements the four-step upload flow with a configurable fault.
type fakeArena struct {
	chunkBytes int64
	received   []byte
	completed  bool
	cancelled  bool

	failChunk   int  // index to fail at, or -1
	corruptAck  bool // acknowledge a byte count that does not match
	chunkBodies [][]byte
}

func (f *fakeArena) handler(t *testing.T) http.Handler {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/bot-uploads", func(w http.ResponseWriter, r *http.Request) {
		var request UploadRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}
		writeJSON(w, UploadSession{
			UploadID:    "upload-1",
			ChunkBytes:  f.chunkBytes,
			TotalChunks: PlanChunks(request.Size, f.chunkBytes),
		})
	})

	mux.HandleFunc("PUT /api/bot-uploads/upload-1/chunks/{index}", func(w http.ResponseWriter, r *http.Request) {
		var index int
		fmt.Sscanf(r.PathValue("index"), "%d", &index)

		if r.Header.Get("Content-Type") != "application/octet-stream" {
			t.Errorf("chunk %d: content type %q", index, r.Header.Get("Content-Type"))
		}
		if index == f.failChunk {
			http.Error(w, `{"error":"chunk rejected"}`, http.StatusInternalServerError)
			return
		}

		body, _ := readAll(r)
		f.received = append(f.received, body...)
		f.chunkBodies = append(f.chunkBodies, body)

		ack := ChunkAck{ReceivedBytes: int64(len(f.received)), NextChunk: index + 1}
		if f.corruptAck {
			ack.ReceivedBytes--
		}
		writeJSON(w, ack)
	})

	mux.HandleFunc("POST /api/bot-uploads/upload-1/complete", func(w http.ResponseWriter, r *http.Request) {
		f.completed = true
		writeJSON(w, map[string]string{"id": "version-1"})
	})

	mux.HandleFunc("DELETE /api/bot-uploads/upload-1", func(w http.ResponseWriter, r *http.Request) {
		f.cancelled = true
		w.WriteHeader(http.StatusNoContent)
	})

	return mux
}

func TestUploadArtifactHappyPath(t *testing.T) {
	// Three chunks plus a partial fourth, so boundary handling is exercised.
	payload := bytes.Repeat([]byte("mixedsolver"), 1000)
	path := writeTempFile(t, payload)

	fake := &fakeArena{chunkBytes: 3000, failChunk: -1}
	server := httptest.NewServer(fake.handler(t))
	defer server.Close()

	client := New(server.URL, "test-key")
	var lastSent int64
	result, err := client.UploadArtifact(t.Context(), UploadRequest{
		Name: "nutt-27td-fl-hu-h1", Games: []string{"27td-fl"}, PlayerCounts: []int{2},
		Size: int64(len(payload)),
	}, path, func(sent, total int64) { lastSent = sent })
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}

	if !bytes.Equal(fake.received, payload) {
		t.Errorf("server received %d bytes, want %d (and identical)", len(fake.received), len(payload))
	}
	if !fake.completed {
		t.Error("upload was never finalized")
	}
	if fake.cancelled {
		t.Error("a successful upload must not be cancelled")
	}
	if lastSent != int64(len(payload)) {
		t.Errorf("final progress %d, want %d", lastSent, len(payload))
	}
	if want := PlanChunks(int64(len(payload)), 3000); len(fake.chunkBodies) != want {
		t.Errorf("sent %d chunks, want %d", len(fake.chunkBodies), want)
	}
	if !strings.Contains(string(result), "version-1") {
		t.Errorf("unexpected completion payload: %s", result)
	}
}

func TestUploadCancelsSessionOnChunkFailure(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), 5000)
	path := writeTempFile(t, payload)

	fake := &fakeArena{chunkBytes: 1000, failChunk: 2}
	server := httptest.NewServer(fake.handler(t))
	defer server.Close()

	client := New(server.URL, "test-key")
	_, err := client.UploadArtifact(t.Context(), UploadRequest{
		Name: "nutt-27td-fl-hu-h1", Games: []string{"27td-fl"}, PlayerCounts: []int{2},
		Size: int64(len(payload)),
	}, path, nil)

	if err == nil {
		t.Fatal("expected the upload to fail")
	}
	if !strings.Contains(err.Error(), "chunk 2") {
		t.Errorf("error should name the failing chunk, got %v", err)
	}
	if !fake.cancelled {
		t.Error("a failed upload must DELETE the session, never retry inside it")
	}
	if fake.completed {
		t.Error("a failed upload must not be finalized")
	}
	// Chunks 0 and 1 landed; nothing after the failure may be sent.
	if len(fake.chunkBodies) != 2 {
		t.Errorf("sent %d chunks after failure, want 2", len(fake.chunkBodies))
	}
}

func TestUploadAbortsOnInconsistentAck(t *testing.T) {
	payload := bytes.Repeat([]byte("y"), 2500)
	path := writeTempFile(t, payload)

	fake := &fakeArena{chunkBytes: 1000, failChunk: -1, corruptAck: true}
	server := httptest.NewServer(fake.handler(t))
	defer server.Close()

	client := New(server.URL, "test-key")
	_, err := client.UploadArtifact(t.Context(), UploadRequest{
		Name: "nutt-27td-fl-hu-h1", Games: []string{"27td-fl"}, PlayerCounts: []int{2},
		Size: int64(len(payload)),
	}, path, nil)

	if err == nil {
		t.Fatal("expected an abort on inconsistent progress")
	}
	if !strings.Contains(err.Error(), "inconsistent progress") {
		t.Errorf("unexpected error: %v", err)
	}
	if !fake.cancelled {
		t.Error("session must be cancelled after an inconsistent acknowledgement")
	}
	if len(fake.chunkBodies) != 1 {
		t.Errorf("must stop after the first bad ack, sent %d chunks", len(fake.chunkBodies))
	}
}

func TestUploadRejectsInvalidRequestBeforeCallingServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server must not be called, got %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	client := New(server.URL, "test-key")
	_, err := client.UploadArtifact(context.Background(), UploadRequest{
		Name: "nutt-27td-fl-hu-h1", Games: []string{"27td-fl"}, PlayerCounts: []int{9}, Size: 10,
	}, "does-not-matter", nil)
	if err == nil {
		t.Fatal("expected validation to reject a 9-seat declaration")
	}
}

func writeTempFile(t *testing.T, payload []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "artifact.bin")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatalf("write temp artifact: %v", err)
	}
	return path
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func readAll(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	var buf bytes.Buffer
	_, err := buf.ReadFrom(r.Body)
	return buf.Bytes(), err
}
