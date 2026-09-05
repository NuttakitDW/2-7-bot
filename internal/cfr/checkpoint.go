package cfr

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
)

// Raw state: every table as little-endian words, so a run can be resumed
// or warm-started with a different opponent model. Nothing here is
// portable across a layout change; the header refuses one.

const stateMagic = "27st"

// SaveState writes the trainer's tables to path, atomically.
func (tr *Trainer) SaveState(path string) error {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriterSize(f, 1<<20)
	header := []byte(stateMagic)
	header = binary.LittleEndian.AppendUint64(header, uint64(tr.Layout.BetSlots))
	header = binary.LittleEndian.AppendUint64(header, uint64(tr.Layout.DrawSlots))
	header = binary.LittleEndian.AppendUint64(header, uint64(tr.Iterations()))
	if _, err := w.Write(header); err != nil {
		return err
	}
	for _, table := range [][]float64{tr.BetRegret, tr.BetStrat, tr.DrawRegret, tr.DrawStrat} {
		if err := writeFloats(w, table); err != nil {
			return err
		}
	}
	for _, table := range [][]uint32{tr.BetVisits, tr.DrawVisits} {
		if err := writeWords(w, table); err != nil {
			return err
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// LoadState reads tables written by SaveState into this trainer.
func (tr *Trainer) LoadState(path string) error {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)
	header := make([]byte, 28)
	if _, err := io.ReadFull(r, header); err != nil {
		return fmt.Errorf("state header: %w", err)
	}
	if string(header[:4]) != stateMagic {
		return fmt.Errorf("state: bad magic %q", header[:4])
	}
	bets := binary.LittleEndian.Uint64(header[4:])
	draws := binary.LittleEndian.Uint64(header[12:])
	if bets != uint64(tr.Layout.BetSlots) || draws != uint64(tr.Layout.DrawSlots) {
		return fmt.Errorf("state: %d/%d slots, layout wants %d/%d", bets, draws, tr.Layout.BetSlots, tr.Layout.DrawSlots)
	}
	tr.iterations.Store(int64(binary.LittleEndian.Uint64(header[20:])))
	for _, table := range [][]float64{tr.BetRegret, tr.BetStrat, tr.DrawRegret, tr.DrawStrat} {
		if err := readFloats(r, table); err != nil {
			return err
		}
	}
	for _, table := range [][]uint32{tr.BetVisits, tr.DrawVisits} {
		if err := readWords(r, table); err != nil {
			return err
		}
	}
	return nil
}

const chunk = 1 << 16

func writeFloats(w io.Writer, table []float64) error {
	buf := make([]byte, 0, 8*chunk)
	for start := 0; start < len(table); start += chunk {
		buf = buf[:0]
		for _, v := range table[start:min(start+chunk, len(table))] {
			buf = binary.LittleEndian.AppendUint64(buf, math.Float64bits(v))
		}
		if _, err := w.Write(buf); err != nil {
			return err
		}
	}
	return nil
}

func readFloats(r io.Reader, table []float64) error {
	buf := make([]byte, 8*chunk)
	for start := 0; start < len(table); start += chunk {
		n := min(chunk, len(table)-start)
		if _, err := io.ReadFull(r, buf[:8*n]); err != nil {
			return fmt.Errorf("state floats: %w", err)
		}
		for i := 0; i < n; i++ {
			table[start+i] = math.Float64frombits(binary.LittleEndian.Uint64(buf[8*i:]))
		}
	}
	return nil
}

func writeWords(w io.Writer, table []uint32) error {
	buf := make([]byte, 0, 4*chunk)
	for start := 0; start < len(table); start += chunk {
		buf = buf[:0]
		for _, v := range table[start:min(start+chunk, len(table))] {
			buf = binary.LittleEndian.AppendUint32(buf, v)
		}
		if _, err := w.Write(buf); err != nil {
			return err
		}
	}
	return nil
}

func readWords(r io.Reader, table []uint32) error {
	buf := make([]byte, 4*chunk)
	for start := 0; start < len(table); start += chunk {
		n := min(chunk, len(table)-start)
		if _, err := io.ReadFull(r, buf[:4*n]); err != nil {
			return fmt.Errorf("state words: %w", err)
		}
		for i := 0; i < n; i++ {
			table[start+i] = binary.LittleEndian.Uint32(buf[4*i:])
		}
	}
	return nil
}
