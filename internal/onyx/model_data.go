package onyx

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"

	"github.com/nuttakit/2-7-bot/internal/cfr"
)

// Compressed generated models keep hosted artifacts small. Decoding happens
// once at startup, with the same validation used for uncompressed JSON.
func decodeOpponentModel(raw []byte) (*cfr.Empirical, error) {
	raw, err := modelJSON(raw)
	if err != nil {
		return nil, err
	}
	return cfr.DecodeEmpirical(raw)
}

func modelJSON(raw []byte) ([]byte, error) {
	if len(raw) >= 2 && raw[0] == 0x1f && raw[1] == 0x8b {
		r, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		defer r.Close()
		const limit = 64 << 20
		raw, err = io.ReadAll(io.LimitReader(r, limit+1))
		if err != nil {
			return nil, err
		}
		if len(raw) > limit {
			return nil, fmt.Errorf("opponent model exceeds %d bytes", limit)
		}
	}
	return raw, nil
}
