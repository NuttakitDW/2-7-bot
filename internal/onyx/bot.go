// Package onyx plays heads-up triple draw with pot-aware betting and mixed
// river actions. It uses only our cards and public events at runtime.
package onyx

import (
	"fmt"
	"math/rand/v2"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/cfr"
	"github.com/nuttakit/2-7-bot/internal/deuce"
	"github.com/nuttakit/2-7-bot/internal/handclass"
	"github.com/nuttakit/2-7-bot/internal/policy"
	"github.com/nuttakit/2-7-bot/internal/table"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

type Bot struct {
	bayes           *riverSolver
	Table           *table.Table
	pot             uint64
	committed       [table.MaxSeats]uint64
	snow            bool
	muck            cards.Set
	strongPat       bool
	opponentActions uint64
	opponentRaises  uint64
	random          func() float64
}

// NewModeled enables public-history tracking and the embedded acting-player
// model for a hybrid policy, independently of the standalone river profile.
func NewModeled() (*Bot, error) {
	switch modelSelection {
	case "mode", "confident", "range-call", "draw-model", "all-model", "all-bets", "nash-river", "range-response", "all-response", "plan-draw":
	default:
		return nil, fmt.Errorf("unknown model selection %q", modelSelection)
	}
	b, err := New()
	if err != nil {
		return nil, err
	}
	if b.bayes == nil {
		b.bayes, err = newRiverSolver()
	}
	return b, err
}

func New() (*Bot, error) {
	switch drawProfile {
	case "baseline", "adaptive":
	default:
		return nil, fmt.Errorf("onyx: unknown draw profile %q", drawProfile)
	}
	switch riverProfile {
	case "baseline", "response", "bayes":
	default:
		return nil, fmt.Errorf("onyx: unknown river profile %q", riverProfile)
	}
	switch snowProfile {
	case "baseline", "balanced", "none":
	default:
		return nil, fmt.Errorf("onyx: unknown snow profile %q", snowProfile)
	}
	switch predrawProfile {
	case "baseline", "raise", "limp", "wide-raise", "wide-limp", "smooth-limp", "value-limp":
	case "priced":
		found := false
		for id, action := range pricedOpening {
			if action > 2 || (action != 0 && handclass.Weight(handclass.ID(id)) == 0) {
				return nil, fmt.Errorf("onyx: invalid priced opening at class %d", id)
			}
			found = found || action != 0
		}
		if !found {
			return nil, fmt.Errorf("onyx: priced opening table is missing")
		}
	default:
		return nil, fmt.Errorf("onyx: unknown pre-draw profile %q", predrawProfile)
	}
	switch predrawDefense {
	case "original", "wide", "pressure":
	default:
		return nil, fmt.Errorf("onyx: unknown pre-draw defense %q", predrawDefense)
	}
	b := &Bot{Table: table.New()}
	if riverProfile == "bayes" {
		var err error
		b.bayes, err = newRiverSolver()
		if err != nil {
			return nil, err
		}
	}
	return b, nil
}

// NewSeeded preserves the default policy while giving offline simulations a
// reproducible private mixing stream. Runtime bots continue to use rand.Float64.
func NewSeeded(seed uint64) (*Bot, error) {
	b, err := New()
	if err != nil {
		return nil, err
	}
	rng := rand.New(rand.NewPCG(seed, seed^0x9E3779B97F4A7C15))
	b.random = rng.Float64
	return b, nil
}
func (b *Bot) Hello(m wire.Message) {
	b.Table.Hello(m)
	b.opponentActions, b.opponentRaises = 0, 0
	if b.bayes != nil {
		b.bayes.fixed = cfr.FixedCard{}
	}
}
func (b *Bot) HandStart(m wire.Message) {
	if b.bayes != nil {
		b.bayes.reset()
	}
	b.Table.HandStart(m)
	b.pot = 0
	b.snow = false
	b.muck = 0
	b.strongPat = false
	b.committed = [table.MaxSeats]uint64{}
}

func (b *Bot) Observe(e wire.Event) {
	if b.bayes != nil {
		b.bayes.observe(e, b.Table.Hand.Seat)
	}
	switch e.Kind {
	case wire.EventDrawResult:
		if e.Seat == b.Table.Hand.Seat {
			b.muck |= cards.NewSet(e.Discarded)
		} else if e.Count > 0 {
			b.strongPat = false
		}
	case wire.EventStreetStart:
		if e.Street != table.Predraw {
			b.committed = [table.MaxSeats]uint64{}
		}
	case wire.EventPost:
		b.pot += e.Amount
		if e.Seat >= 0 && e.Seat < table.MaxSeats && e.PostKind != "ante" {
			b.committed[e.Seat] += e.Amount
		}
	case wire.EventActed:
		if e.Seat >= 0 && e.Seat < table.MaxSeats && e.Seat != b.Table.Hand.Seat {
			b.opponentActions++
			if e.Action.Kind == wire.ActionRaise || e.Action.Kind == wire.ActionBet {
				b.opponentRaises++
			}
		}
		if e.Seat != b.Table.Hand.Seat && b.Table.Hand.Street > table.Predraw && e.Action.Kind == wire.ActionRaise {
			b.strongPat = true
		}
		if e.Seat >= 0 && e.Seat < table.MaxSeats && e.StreetCommit >= b.committed[e.Seat] {
			b.pot += e.StreetCommit - b.committed[e.Seat]
			b.committed[e.Seat] = e.StreetCommit
		}
	}
	b.Table.Observe(e)
}

// RiverSolve answers a last-street wager from the exact solve against the
// fitted opponent model, when the tracker can place the hand.
func (b *Bot) RiverSolve(d wire.Decision) (wire.Action, bool) {
	if b.bayes == nil {
		return wire.Action{}, false
	}
	a, ok := b.bayes.decide(b.Table.Hand.Seat, b.Table.Hand.Cards, d)
	if !ok {
		return wire.Action{}, false
	}
	return wire.Legalize(d, a, b.Table.Hand.Cards), true
}

func (b *Bot) Decide(d wire.Decision) wire.Action {
	if b.bayes != nil {
		if a, ok := b.bayes.decide(b.Table.Hand.Seat, b.Table.Hand.Cards, d); ok {
			return wire.Legalize(d, a, b.Table.Hand.Cards)
		}
	}
	// The sampled ranges describe an opponent that checks and calls too.
	// They do not apply to a near-always-raiser; use the broad value policy
	// after enough public actions establish that behavior.
	if b.opponentActions >= 64 && b.opponentRaises*4 > b.opponentActions*3 {
		return policy.Decide(b.Table, d)
	}
	mix := rand.Float64()
	if b.random != nil {
		mix = b.random()
	}
	return wire.Legalize(d, b.propose(d, mix), b.Table.Hand.Cards)
}

func (b *Bot) propose(d wire.Decision, mix float64) wire.Action {
	h := &b.Table.Hand
	if !h.Complete() {
		return wire.Check()
	}
	if d.Kind == wire.DecisionDraw {
		opp, ago, known := h.OpponentDraw(h.Street)
		if b.snow {
			if !known || opp > 0 {
				return wire.Discard(nil)
			}
			b.snow = false
		}
		if h.Street == table.Draw3 && known && opp > 0 && len(draw(h)) >= 2 {
			frequency := finalSnowFrequency(ago == 0)
			if mix < frequency {
				b.snow = true
				return wire.Discard(nil)
			}
		}
		// Low duplicates remove outs from the opponent's range while
		// leaving us little drawing value. Mix a pat bluff into those
		// hands on the second draw, and abandon it if the opponent pats.
		if h.Street == table.Draw2 && known && opp >= 2 && h.Category() == deuce.Broken && len(draw(h)) >= 2 {
			blockers := 0
			for _, c := range h.Cards {
				if c.Rank <= cards.Seven {
					blockers++
				}
			}
			if blockers >= 3 && mix < secondSnowFrequency() {
				b.snow = true
				return wire.Discard(nil)
			}
		}
		if h.Street == table.Draw3 {
			return wire.Discard(b.finalDraw())
		}
		return wire.Discard(draw(h))
	}
	if d.Kind != wire.DecisionWager {
		return wire.Check()
	}
	if h.Street == table.Predraw {
		return predraw(h, mix)
	}
	opp, _, known := h.OpponentDraw(h.Street)
	if !known {
		opp = 1
	}
	if b.snow {
		if d.Call == nil && opp > 0 {
			return wire.Raise(0)
		}
		return wire.Fold()
	}
	category := h.Category()
	if h.Street == table.Draw3 {
		return b.river(d, opp, mix)
	}
	next := *h
	next.Street++
	n := len(draw(&next))
	if d.Call == nil {
		if b.strongPat && opp == 0 && category < deuce.Seven {
			return wire.Check()
		}
		if category >= deuce.Nine || (category == deuce.Ten && opp > 0) {
			return wire.Raise(0)
		}
		if opp > 0 && ((n <= 1 && (n < opp || h.OnButton())) || (n <= 2 && opp >= 3)) {
			return wire.Raise(0)
		}
		if h.OnButton() && opp > 0 && n <= 2 && mix < 0.70 {
			return wire.Raise(0)
		}
		return wire.Check()
	}
	if (category == deuce.Seven && (!h.FacingRaise() && !b.strongPat || nuts(h.Cards))) || (category == deuce.Eight && !b.strongPat && !h.FacingRaise() && (opp > 0 || smooth(h.Cards))) {
		return wire.Raise(0)
	}
	if category >= deuce.Eight || (category == deuce.Nine && !h.FacingRaise()) || (category == deuce.Ten && !h.FacingRaise()) {
		return wire.Call()
	}
	if n <= 1 || (h.Street == table.Draw1 && n <= 2) {
		return wire.Call()
	}
	// A two-card draw can continue in a large pot on the second draw,
	// but needs to cover both its immediate cost and its poor realization.
	if h.Street == table.Draw2 && n == 2 && b.odds(d) < 0.10 {
		return wire.Call()
	}
	if h.Street == table.Draw2 && n == 2 && opp > 0 && !h.FacingRaise() && b.odds(d) < 0.24 {
		return wire.Call()
	}
	return wire.Fold()
}

func predraw(h *table.Hand, mix float64) wire.Action {
	c := policy.Classify(h.Cards)
	if !h.Opened() {
		if predrawProfile == "priced" && h.OnButton() {
			if c.Open == policy.Raise {
				return wire.Raise(0)
			}
			switch pricedOpening[handclass.Of(h.Cards)] {
			case 1:
				return wire.Call()
			case 2:
				return wire.Raise(0)
			default:
				return wire.Fold()
			}
		}
		if predrawProfile == "smooth-limp" || predrawProfile == "value-limp" {
			if c.Shape >= policy.TwoCardDraw {
				return wire.Raise(0)
			}
			if h.OnButton() && (c.Open == policy.Raise || additionalOpen(h.Cards)) {
				return wire.Call()
			}
			return wire.Fold() // legalized to check for a free BB option
		}
		if c.Open == policy.Raise {
			return wire.Raise(0)
		}
		if h.OnButton() && additionalOpen(h.Cards) {
			if predrawProfile == "limp" || predrawProfile == "wide-limp" {
				return wire.Call()
			}
			return wire.Raise(0)
		}
		return wire.Fold()
	}
	if c.Defend == policy.Fold {
		return wire.Fold()
	}
	if !h.OnButton() && h.Wagers == 2 {
		keep := policy.DrawingKeep(h.Cards)
		if c.Shape < policy.OneCardDraw && (len(keep) < 2 || (predrawDefense == "original" && len(keep) == 2 && (keep[0] >= cards.Six || keep[1] > cards.Eight))) {
			return wire.Fold()
		}
		if predrawDefense == "pressure" && c.Shape == policy.TwoCardDraw {
			return wire.Raise(0)
		}
	}
	if c.Defend == policy.Raise {
		if c.Shape >= policy.OneCardDraw || (h.Wagers < 3 && mix < 0.35) {
			return wire.Raise(0)
		}
	}
	return wire.Call()
}

func draw(h *table.Hand) []cards.Card {
	c := h.Category()
	if drawProfile == "adaptive" && c == deuce.Nine && h.Street < table.Draw3 {
		opp, _, known := h.OpponentDraw(h.Street)
		keep := policy.DrawingKeep(h.Cards)
		if known && opp <= 1 && len(keep) == 4 {
			return policy.Discards(h.Cards, keep)
		}
	}
	if c >= deuce.Nine {
		return nil
	}
	opp, _, known := h.OpponentDraw(h.Street)
	if c == deuce.Ten && (!known || opp > 0) {
		// Keep the ten when breaking it would require more than one card;
		// on the final draw it already beats most uncompleted draws.
		if h.Street == table.Draw3 || len(policy.DrawingKeep(h.Cards)) < 4 {
			return nil
		}
	}
	return policy.Draw(h)
}

func (b *Bot) odds(d wire.Decision) float64 {
	if d.Call == nil {
		return 0
	}
	return float64(*d.Call) / float64(max(uint64(1), b.pot+*d.Call))
}

func (b *Bot) river(d wire.Decision, opp int, mix float64) wire.Action {
	if action, ok := b.responseRiver(d, opp); ok {
		return action
	}
	h := &b.Table.Hand
	v := deuce.Eval(h.Cards)
	c := deuce.CategoryOf(v)
	if d.Call == nil {
		if b.strongPat && opp == 0 && c < deuce.Seven {
			return wire.Check()
		}
		if c >= deuce.Eight || (c == deuce.Nine && (opp > 0 || h.OnButton())) || (c == deuce.Ten && (opp >= 2 || (opp > 0 && h.OnButton()))) {
			return wire.Raise(0)
		}
		if opp > 0 && c == deuce.Broken {
			bet := float64(max(uint64(1), b.Table.Match.BigBlind*2))
			frequency := 1.4 * bet / (float64(b.pot) + bet)
			if h.OnButton() {
				frequency *= 1.6
			}
			if mix < min(0.45, frequency) {
				return wire.Raise(0)
			}
		}
		return wire.Check()
	}
	if (c == deuce.Seven && (!h.FacingRaise() && !b.strongPat || nuts(h.Cards))) || (c == deuce.Eight && !b.strongPat && !h.FacingRaise() && (opp > 0 || smooth(h.Cards))) {
		return wire.Raise(0)
	}
	// Conditional on a bet, even a weak no-pair hand can be profitable at
	// the offered price. Keep a conservative margin for sparse samples.
	context := 8
	if opp > 0 {
		context += 2
	}
	ours, known := h.DrawCount(h.Seat, h.Street)
	if !known || ours > 0 {
		context++
	}
	if h.FacingRaise() || b.strongPat && opp == 0 {
		context += 4
	}
	r := finalRanges[context]
	if c < deuce.Eight && r.total >= 15 {
		if r.equity(v) > b.odds(d)+0.015 {
			return wire.Call()
		}
		return wire.Fold()
	}
	if c >= deuce.Nine {
		if c == deuce.Nine && h.FacingRaise() {
			return wire.Fold()
		}
		if c == deuce.Nine && opp == 0 && b.odds(d) > 0.12 {
			return wire.Fold()
		}
		return wire.Call()
	}
	if c == deuce.Ten && opp > 0 && !h.FacingRaise() {
		return wire.Call()
	}
	if opp > 0 && !h.FacingRaise() && v.Class() == deuce.HighCard {
		if v.HighCard() <= cards.Jack && b.odds(d) < 0.22 {
			return wire.Call()
		}
		if v.HighCard() <= cards.Queen && b.odds(d) < 0.15 {
			return wire.Call()
		}
	}
	return wire.Fold()
}

func smooth(hand []cards.Card) bool {
	ranks := cards.DistinctRanks(hand)
	return len(ranks) == 5 && ranks[3] <= cards.Six
}

func nuts(hand []cards.Card) bool {
	return deuce.Eval(hand) == nutValue
}

var nutValue = deuce.Eval(cards.MustParse("2c", "3d", "4h", "5s", "7c"))
