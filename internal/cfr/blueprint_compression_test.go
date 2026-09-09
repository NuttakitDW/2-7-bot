package cfr

import (
	"bytes"
	"compress/gzip"
	"testing"
)

func TestBlueprintCompressionLevelsPreservePolicy(t *testing.T) {
	bp := &Blueprint{Bet: []byte{128, 127, 0, 255}, Draw: []byte{1, 2, 252}}
	layout := &Layout{BetSlots: 4, DrawSlots: 3}
	for _, level := range []int{gzip.BestSpeed, gzip.DefaultCompression, gzip.BestCompression} {
		var out bytes.Buffer
		if err := bp.EncodeLevel(&out, level); err != nil {
			t.Fatal(err)
		}
		decoded, err := Decode(out.Bytes(), layout)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(bp.Bet, decoded.Bet) || !bytes.Equal(bp.Draw, decoded.Draw) {
			t.Fatalf("level%d changed policy", level)
		}
	}
	var out bytes.Buffer
	if err := bp.EncodeLevel(&out, 10); err == nil || out.Len() != 0 {
		t.Fatal("invalid level accepted or wrote output")
	}
}
