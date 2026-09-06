package onyx

import (
	"github.com/nuttakit/2-7-bot/internal/deuce"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

var riverProfile = "baseline"

var riverResponseRanges = func() (out [12][5]handRange) {
	for c, rows := range riverResponseData {
		for a, data := range rows {
			out[c][a] = makeRange(data)
		}
	}
	return
}()

// responseEV compares the complete bet/call-or-fold and check/call-or-fold
// lines, relative to money already in the pot. OOP checking is not a free
// showdown. The response frequencies are empirical, not equilibrium claims.
func responseEV(v deuce.Value, pot, bet float64, button bool, r [5]handRange, raiseEquity, checkBetEquity float64) (checkEV, betEV float64, ok bool) {
	n := r[0].total + r[1].total + r[2].total
	if n < 20 || bet <= 0 {
		return 0, 0, false
	}
	raisePayoff := -bet
	if raiseEquity > bet/(pot+4*bet)+.015 {
		raisePayoff = (pot+4*bet)*raiseEquity - 2*bet
	}
	betEV = (r[0].total*pot + r[1].total*((pot+2*bet)*r[1].equity(v)-bet) + r[2].total*raisePayoff) / n
	if button {
		for i := 0; i < 3; i++ {
			checkEV += r[i].total * r[i].equity(v) * pot / n
		}
		return checkEV, betEV, true
	}
	nc := r[3].total + r[4].total
	if nc < 20 {
		return 0, 0, false
	}
	callPayoff := 0.0
	if checkBetEquity > bet/(pot+2*bet)+.015 {
		callPayoff = (pot+2*bet)*checkBetEquity - bet
	}
	checkEV = (r[3].total*r[3].equity(v)*pot + r[4].total*callPayoff) / nc
	return checkEV, betEV, true
}

func (b *Bot) responseRiver(d wire.Decision, opp int) (wire.Action, bool) {
	if riverProfile != "response" {
		return wire.Action{}, false
	}
	h := &b.Table.Hand
	// Preserve the value-raising policy and its public strong-pat read.
	if h.Category() >= deuce.Eight || b.strongPat && opp == 0 && (d.Call == nil || h.Wagers != 2) {
		return wire.Action{}, false
	}
	ours, known := h.DrawCount(h.Seat, h.Street)
	if !known {
		return wire.Action{}, false
	}
	context := min(opp, 2) * 4
	base := 8
	if ours > 0 {
		context += 2
		base++
	}
	if opp > 0 {
		base += 2
	}
	if h.OnButton() {
		context++
	}
	r := riverResponseRanges[context]
	v := deuce.Eval(h.Cards)
	// Sparse response classes back off to the existing conditional ranges.
	equity := func(sample handRange, parent int) float64 {
		if sample.total >= 8 {
			return sample.equity(v)
		}
		return finalRanges[parent].equity(v)
	}
	raiseEquity := equity(r[2], base+4)
	checkBetEquity := equity(r[4], base)
	if d.Call != nil {
		var e float64
		switch {
		case h.Wagers == 2:
			e = raiseEquity
		case h.Wagers == 1 && !h.OnButton():
			e = checkBetEquity
		default:
			return wire.Action{}, false
		}
		if e > b.odds(d)+.015 {
			return wire.Call(), true
		}
		return wire.Fold(), true
	}
	if d.Bet == nil {
		return wire.Action{}, false
	}
	bet := float64(d.Bet.MinTo)
	checkEV, betEV, ok := responseEV(v, float64(b.pot), bet, h.OnButton(), r, raiseEquity, checkBetEquity)
	if !ok {
		return wire.Action{}, false
	}
	// Demand a margin before switching on noisy empirical response rates.
	if betEV > checkEV+.10*bet {
		return wire.Raise(0), true
	}
	return wire.Check(), true
}
