package onyx

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"reflect"
	"testing"
)

func TestDecodeOpponentModelAcceptsCompressedJSON(t *testing.T) {
	plain, err := decodeOpponentModel(opponentPolicyData)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(plain)
	if err != nil {
		t.Fatal(err)
	}
	var packed bytes.Buffer
	w := gzip.NewWriter(&packed)
	if _, err := w.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	uncompressed, err := decodeOpponentModel(raw)
	if err != nil {
		t.Fatal(err)
	}
	compressed, err := decodeOpponentModel(packed.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plain, compressed) || !reflect.DeepEqual(plain, uncompressed) {
		t.Fatal("compressed model differs")
	}
	if _, err := decodeOpponentModel([]byte{0x1f, 0x8b, 0}); err == nil {
		t.Fatal("accepted broken gzip")
	}
	if _, err := decodeOpponentModel([]byte("bad JSON")); err == nil {
		t.Fatal("accepted broken JSON")
	}
	corrupt := append([]byte(nil), packed.Bytes()...)
	corrupt[len(corrupt)-8] ^= 1
	if _, err := decodeOpponentModel(corrupt); err == nil {
		t.Fatal("accepted checksum mismatch")
	}
}
