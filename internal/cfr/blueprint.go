package cfr

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
)

// Blueprint is a trained average strategy, one byte per action: the
// probabilities of a set scaled to sum to 255, and all zero for a set the
// trainer never reached — which is how the player knows to defer.
type Blueprint struct {
	Bet  []uint8
	Draw []uint8
}

// Extract normalises the trainer's strategy sums, leaving sets visited
// fewer than minVisits times empty so the player defers on them.
func (tr *Trainer) Extract(minVisits uint32) *Blueprint {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	bp := &Blueprint{Bet: make([]uint8, tr.Layout.BetSlots), Draw: make([]uint8, tr.Layout.DrawSlots)}
	for i := range tr.Tree.Nodes {
		node := &tr.Tree.Nodes[i]
		if node.Kind != KindBet {
			continue
		}
		n := int64(len(node.Acts))
		sets := int64(BetContexts(int(node.Street))) * int64(Buckets(int(node.Street)))
		for set := int64(0); set < sets; set++ {
			slot := node.Offset + set*n
			if tr.BetVisits[slot] >= minVisits {
				quantise(tr.BetStrat[slot:slot+n], bp.Bet[slot:slot+n])
			}
		}
	}
	for slot := int64(0); slot < tr.Layout.DrawSlots; slot += MaxCand {
		if tr.DrawVisits[slot] >= minVisits {
			quantise(tr.DrawStrat[slot:slot+MaxCand], bp.Draw[slot:slot+MaxCand])
		}
	}
	return bp
}

// quantise scales one set's weights to bytes summing to 255, with the
// remainder of the rounding given to the most likely action so a purified
// reading and a sampled one agree on the mode.
func quantise(weights []float64, out []uint8) {
	total, best := 0.0, 0
	for i, w := range weights {
		total += w
		if w > weights[best] {
			best = i
		}
	}
	if total <= 0 {
		return
	}
	sum := 0
	for i, w := range weights {
		out[i] = uint8(w / total * 255)
		sum += int(out[i])
	}
	out[best] += uint8(255 - sum)
}

const blueprintMagic = "27bp"

// Encode writes the blueprint gzip-compressed. Unreached sets are zero
// runs, which is most of the table, so this is small.
func (bp *Blueprint) Encode(w io.Writer) error {
	zw, err := gzip.NewWriterLevel(w, gzip.BestCompression)
	if err != nil {
		return err
	}
	header := make([]byte, 0, 20)
	header = append(header, blueprintMagic...)
	header = binary.LittleEndian.AppendUint64(header, uint64(len(bp.Bet)))
	header = binary.LittleEndian.AppendUint64(header, uint64(len(bp.Draw)))
	for _, chunk := range [][]byte{header, bp.Bet, bp.Draw} {
		if _, err := zw.Write(chunk); err != nil {
			return err
		}
	}
	return zw.Close()
}

// Decode reads what Encode wrote and checks it fits the layout.
func Decode(data []byte, l *Layout) (*Blueprint, error) {
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("blueprint: %w", err)
	}
	header := make([]byte, 20)
	if _, err := io.ReadFull(zr, header); err != nil {
		return nil, fmt.Errorf("blueprint header: %w", err)
	}
	if string(header[:4]) != blueprintMagic {
		return nil, fmt.Errorf("blueprint: bad magic %q", header[:4])
	}
	bets := binary.LittleEndian.Uint64(header[4:])
	draws := binary.LittleEndian.Uint64(header[12:])
	if bets != uint64(l.BetSlots) || draws != uint64(l.DrawSlots) {
		return nil, fmt.Errorf("blueprint: %d/%d slots, layout wants %d/%d", bets, draws, l.BetSlots, l.DrawSlots)
	}
	bp := &Blueprint{Bet: make([]uint8, bets), Draw: make([]uint8, draws)}
	if _, err := io.ReadFull(zr, bp.Bet); err != nil {
		return nil, fmt.Errorf("blueprint bets: %w", err)
	}
	if _, err := io.ReadFull(zr, bp.Draw); err != nil {
		return nil, fmt.Errorf("blueprint draws: %w", err)
	}
	return bp, nil
}

// Player plays a Blueprint as a Model.
//
// Purify drops actions under a probability floor before sampling. The
// average strategy of an unconverged run carries residual weight on
// actions it has all but abandoned. Thresholding changes the strategy and
// must be evaluated against opponents; it does not guarantee improvement.
type Player struct {
	Tree   *Tree
	Abs    *Abstraction
	Layout *Layout
	BP     *Blueprint
	Purify float64
}

// Bet samples a betting action; ok is false for an untrained set.
func (pl *Player) Bet(v *View) (int, bool) {
	node := &pl.Tree.Nodes[v.Node]
	class := Class(v.Hand[:])
	slot := pl.Layout.BetSlot(node, BetContext(v.Seat, v.Street, &v.Drawn), pl.Abs.Bucket(v.Street, class))
	probs := pl.BP.Bet[slot : slot+int64(len(node.Acts))]
	i, ok := pl.choose(probs, v.Rand)
	if !ok {
		return Pass, false
	}
	return int(node.Acts[i]), true
}

// Draw samples a keep mask; ok is false for an untrained set.
func (pl *Player) Draw(v *View) (uint8, bool) {
	info := &pl.Abs.Classes[Class(v.Hand[:])]
	slot := pl.Layout.DrawSlot(v.Street, v.Seat, AggrState(v.Seat, v.LastAggr),
		DrawContext(v.Seat, v.Street, &v.Drawn), int(info.DrawClass))
	probs := pl.BP.Draw[slot : slot+int64(info.NumCand)]
	i, ok := pl.choose(probs, v.Rand)
	if !ok {
		return 0, false
	}
	return info.Keep[i], true
}

func (pl *Player) choose(probs []uint8, u float64) (int, bool) {
	floor := uint8(pl.Purify * 255)
	total := 0
	for _, p := range probs {
		if p >= floor {
			total += int(p)
		}
	}
	if total == 0 {
		return 0, false
	}
	target := u * float64(total)
	for i, p := range probs {
		if p < floor {
			continue
		}
		target -= float64(p)
		if target < 0 {
			return i, true
		}
	}
	return len(probs) - 1, true
}

// Probabilities is the blueprint's mixed answer at a betting view, for
// inspection.
func (pl *Player) Probabilities(v *View) []float64 {
	node := &pl.Tree.Nodes[v.Node]
	slot := pl.Layout.BetSlot(node, BetContext(v.Seat, v.Street, &v.Drawn), pl.Abs.Bucket(v.Street, Class(v.Hand[:])))
	probs := pl.BP.Bet[slot : slot+int64(len(node.Acts))]
	out := make([]float64, len(probs))
	for i, p := range probs {
		out[i] = float64(p) / 255
	}
	return out
}

var _ Model = (*Player)(nil)
