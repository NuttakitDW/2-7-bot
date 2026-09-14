package zircon

import (
	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/deuce"
	"github.com/nuttakit/2-7-bot/internal/policy"
	"github.com/nuttakit/2-7-bot/internal/sixmaxclone"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

type Bot struct {
	State         *State
	profile       Profile
	clone         *sixmaxclone.Model
	overrideStats OverrideStats
}

func New() *Bot                         { return NewGeneration2() }
func (b *Bot) Hello(m wire.Message)     { b.State.Hello(m) }
func (b *Bot) HandStart(m wire.Message) { b.State.HandStart(m) }
func (b *Bot) Observe(e wire.Event)     { b.State.Observe(e) }

func (b *Bot) Decide(d wire.Decision) wire.Action {
	baseline := wire.Legalize(d, b.propose(d), b.State.Hand.Cards)
	if action, ok := b.clonePredraw(d, baseline); ok {
		return wire.Legalize(d, action, b.State.Hand.Cards)
	}
	return baseline
}

func (b *Bot) propose(d wire.Decision) wire.Action {
	if d.Kind == wire.DecisionDraw {
		return wire.Discard(b.draw())
	}
	if d.Kind != wire.DecisionWager || !b.State.Hand.Complete() {
		return wire.Check()
	}
	if b.State.Hand.Street == Predraw {
		return b.predraw(d)
	}
	return b.postdraw(d)
}

type startQuality struct {
	category deuce.Category
	draw     int
	strong1  bool
	clean1   bool
	strong2  bool
	late2    bool
}

func classifyStart(hand []cards.Card) startQuality {
	keep := policy.DrawingKeep(hand)
	draw := len(hand) - len(keep)
	q := startQuality{category: deuce.Categorize(hand), draw: draw}
	q.strong1 = draw == 1 && len(keep) == 4 && keep[3] <= cards.Eight
	q.clean1 = draw == 1 && len(keep) == 4 && keep[3] <= cards.Eight
	q.strong2 = draw == 2 && len(keep) == 3 && keep[0] == cards.Two
	q.late2 = draw == 2 && len(keep) == 3 && (keep[0] == cards.Two || keep[2] <= cards.Eight)
	return q
}

func (b *Bot) predraw(d wire.Decision) wire.Action {
	h := &b.State.Hand
	q, pos := classifyStart(h.Cards), b.State.Position(h.Hero)
	late := pos == Cutoff || pos == Button
	unopenedPlayable := q.category >= deuce.Nine || q.clean1 || q.strong2 ||
		((late || pos == SmallBlind) && q.late2)
	facing := d.Call != nil

	if !facing {
		if unopenedPlayable {
			return wire.Raise(0)
		}
		return wire.Check()
	}

	// Cold entry becomes sharply stronger as the number of raises grows.
	if h.StreetAggressions >= 2 {
		if q.category >= deuce.Seven {
			return wire.Raise(0)
		}
		if q.category >= deuce.Eight || q.strong1 {
			return wire.Call()
		}
		if h.HeroPredrawRaises > 0 && q.strong2 && b.affordable(d.Call, 6) {
			return wire.Call()
		}
		return wire.Fold()
	}
	if h.StreetAggressions == 1 {
		if q.category >= deuce.Eight {
			return wire.Raise(0)
		}
		if q.category >= deuce.Nine || q.strong1 {
			return wire.Call()
		}
		if pos == BigBlind && (q.clean1 || q.strong2) && b.affordable(d.Call, 4) {
			return wire.Call()
		}
		return wire.Fold()
	}
	// Limped pots retain position discipline; the big blind checks its free
	// option through the no-facing branch above.
	if h.StreetCalls > 0 {
		if q.category >= deuce.Nine || q.strong1 {
			return wire.Raise(0)
		}
		if (late || pos == BigBlind) && (q.clean1 || q.strong2) {
			return wire.Call()
		}
		return wire.Fold()
	}
	if unopenedPlayable {
		return wire.Raise(0)
	}
	return wire.Fold()
}

func (b *Bot) draw() []cards.Card {
	h := &b.State.Hand
	if !h.Complete() {
		return nil
	}
	category := h.Category()
	if category >= deuce.Eight {
		return nil
	}
	discards := drawingDiscards(h.Cards)
	if category == deuce.Nine || category == deuce.Ten {
		if len(discards) != 1 {
			return nil
		}
	}
	if len(discards) != 1 {
		return discards
	}
	keep := policy.DrawingKeep(h.Cards)
	if !cleanFourCardEight(keep) && (category == deuce.Nine || category == deuce.Ten) {
		return nil
	}
	if !cleanFourCardEight(keep) {
		return discards
	}

	freshPat, _ := b.State.LivePatPressure()
	if category == deuce.Nine {
		if freshPat == 0 {
			return nil
		}
		// Exact replacement enumeration prevents a blanket final-draw break.
		// The improvement rate is a draw-quality signal, not showdown equity.
		outcomes := EnumerateOneCard(h.Cards, discards[0])
		if outcomes.BetterRate() < 0.15 {
			return nil
		}
		return discards
	}
	if category == deuce.Ten {
		// Smooth tens take their useful one-card redraw before the river. On
		// the final draw they do so only against credible pat pressure.
		if h.Street < Draw3 || freshPat > 0 {
			return discards
		}
		return nil
	}
	return discards
}

func (b *Bot) postdraw(d wire.Decision) wire.Action {
	h := &b.State.Hand
	if h.Street == Draw3 {
		return b.river(d)
	}
	category := h.Category()
	facing := d.Call != nil
	freshPat, stalePat := b.State.LivePatPressure()
	pressure := freshPat > 0 || (stalePat > 0 && h.StreetAggressions > 0)
	live := b.State.LiveOpponents()

	if category == deuce.Seven {
		if !pressure || exactNuts(h.Cards) || live <= 2 {
			return wire.Raise(0)
		}
		return wire.Call()
	}
	if category == deuce.Eight {
		if !facing && (b.State.AllLiveDrawingCurrent() || live <= 2) {
			return wire.Bet(0)
		}
		if facing && !pressure && live == 1 && smoothEight(h.Cards) && h.StreetAggressions <= 1 {
			return wire.Raise(0)
		}
		return wire.Call()
	}
	if !facing && len(drawingDiscards(h.Cards)) == 1 && b.State.AllLiveDrewAtLeastCurrent(2) {
		return wire.Bet(0)
	}
	if category == deuce.Nine {
		if !facing && live <= 2 && b.State.AllLiveDrawingCurrent() {
			return wire.Bet(0)
		}
		if facing && !pressure && live <= 3 && b.affordable(d.Call, uint64(5+live)) {
			return wire.Call()
		}
		if facing && pressure && h.Street < Draw3 && h.StreetAggressions <= 1 &&
			cleanFourCardEight(policy.DrawingKeep(h.Cards)) && b.affordable(d.Call, 7) {
			return wire.Call()
		}
		if !facing {
			return wire.Check()
		}
		return wire.Fold()
	}
	if !facing {
		return wire.Check()
	}
	draws := len(drawingDiscards(h.Cards))
	if draws == 1 && h.StreetAggressions <= 2 {
		denominator := uint64(5 + max(0, live-1))
		if pressure {
			denominator += 2
		}
		if h.StreetAggressions == 2 {
			denominator += 1
		}
		keep := policy.DrawingKeep(h.Cards)
		canDraw := (freshPat <= 1 && cleanFourCardEight(keep)) || (freshPat == 0 && cleanFourCardNine(keep))
		if canDraw && b.affordable(d.Call, denominator) {
			return wire.Call()
		}
	}
	if draws == 2 && h.Street == Draw1 && h.StreetAggressions <= 1 && b.affordable(d.Call, 5) {
		return wire.Call()
	}
	return wire.Fold()
}

func (b *Bot) river(d wire.Decision) wire.Action {
	h := &b.State.Hand
	category := h.Category()
	facing := d.Call != nil
	freshPat, stalePat := b.State.LivePatPressure()
	live, behind := b.State.LiveOpponents(), b.State.PlayersBehind()
	strongAction := h.StreetAggressions >= 2
	if h.LastAggressor >= 0 && h.LastAggressor != h.Hero && !h.Seats[h.LastAggressor].Folded {
		read, ok := b.State.LatestDraw(h.LastAggressor)
		strongAction = strongAction || (ok && read.Count == 0)
	}

	if exactNuts(h.Cards) {
		if facing {
			return wire.Raise(0)
		}
		return wire.Bet(0)
	}
	if !facing {
		switch category {
		case deuce.Seven:
			return wire.Bet(0)
		case deuce.Eight:
			if freshPat == 0 || smoothEight(h.Cards) {
				return wire.Bet(0)
			}
		case deuce.Nine:
			if live == 1 && freshPat == 0 && stalePat == 0 && b.State.AllLiveDrawingCurrent() {
				return wire.Bet(0)
			}
		}
		return wire.Check()
	}

	switch category {
	case deuce.Seven:
		if behind == 0 && h.StreetAggressions <= 1 && live <= 2 && deuce.Eval(h.Cards) >= strongSevenFloor {
			return wire.Raise(0)
		}
		return wire.Call()
	case deuce.Eight:
		if (freshPat >= 2 || stalePat >= 2 || (freshPat >= 1 && stalePat >= 1)) && h.StreetAggressions >= 2 {
			return wire.Fold()
		}
		if live >= 3 && (freshPat >= 2 || strongAction) && !smoothEight(h.Cards) {
			return wire.Fold()
		}
		if live >= 2 && h.StreetAggressions >= 2 && (freshPat >= 1 || stalePat >= 1) && !smoothEight(h.Cards) {
			return wire.Fold()
		}
		if h.StreetAggressions >= 3 && !smoothEight(h.Cards) {
			return wire.Fold()
		}
		return wire.Call()
	case deuce.Nine:
		if behind != 0 || freshPat != 0 || strongAction || !b.State.AllLiveDrawingCurrent() {
			return wire.Fold()
		}
		switch {
		case live == 1 && b.affordable(d.Call, 5):
			return wire.Call()
		case live == 2 && riverSmoothNine(h.Cards) && b.affordable(d.Call, 9):
			return wire.Call()
		case live == 3 && riverSmoothNine(h.Cards) && b.affordable(d.Call, 12):
			return wire.Call()
		}
	case deuce.Ten:
		if live == 1 && behind == 0 && freshPat == 0 && !strongAction && !b.State.LiveHadPriorPat() &&
			b.State.AllLiveDrewAtLeastCurrent(2) && b.affordable(d.Call, 8) {
			return wire.Call()
		}
	}
	return wire.Fold()
}

func (b *Bot) affordable(call *uint64, denominator uint64) bool {
	if call == nil || denominator == 0 || *call == 0 {
		return call != nil
	}
	pot := b.eligiblePot(*call)
	return *call*denominator <= pot+*call
}

// eligiblePot caps every contribution at the amount hero can contest. The
// pending call can itself be the action that creates a side pot.
func (b *Bot) eligiblePot(call uint64) uint64 {
	h := &b.State.Hand
	cap := h.Seats[h.Hero].Contribution + call
	var pot uint64
	for i := range h.Seats {
		contribution := h.Seats[i].Contribution
		if contribution > cap {
			contribution = cap
		}
		pot += contribution
	}
	return pot
}
