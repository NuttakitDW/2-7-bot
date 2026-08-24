// Package table rebuilds what the strategy needs to know from the arena's
// event stream.
//
// It tracks deliberately little. `act` carries no table state and `decision`
// is authoritative on legality (WIRE_PROTOCOL.md:181-188), so there is no
// reason to reconstruct pots, stacks or street commitments in order to play —
// only the things a 2-7 strategy actually reads: our own five cards, our
// position, which street we are on, and how many cards everyone drew.
package table

import (
	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/deuce"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

// MaxSeats is the engine's seat cap for 27td-fl (game/spec.rs:665).
const MaxSeats = 6

// The four streets. Each is a draw phase — where applicable — followed by a
// betting round (game/spec.rs:673-683). Predraw and Draw1 bet in small bets,
// Draw2 and Draw3 in big bets, but nothing here hardcodes that: the tier is
// visible at runtime in the decision's min_to/max_to.
const (
	Predraw = iota
	Draw1
	Draw2
	Draw3
	Streets
)

// unknownDraw marks a seat that has not drawn on a street yet. Draw counts
// are public and never redacted, so "unknown" only ever means "not yet" —
// which matters, because heads-up the big blind draws before the button and
// therefore chooses its discard blind.
const unknownDraw = -1

// Match is the per-match configuration from `hello`.
type Match struct {
	GameID        string
	SeatCount     int
	StartingStack uint64
	SmallBlind    uint64
	BigBlind      uint64
	Ante          uint64
	// RaiseCap is the maximum number of wagers a betting round allows, the
	// opening bet or blind counting as the first; 0 means uncapped.
	RaiseCap  int
	TimeoutMs uint64
}

// Hand is the state of the hand in progress.
type Hand struct {
	// No is the arena's hand counter. The spec calls it 1-based and real
	// transcripts start at 0, so it is only ever used as an opaque key.
	No int64

	// Seat is our seat for this hand. The arena rotates bots through seats
	// between hands rather than moving the button, so a seat number is also
	// a position and ours changes from hand to hand.
	Seat int

	// Button is always seat 0. Read from the event anyway rather than
	// assumed, so a future engine that rotates it does not silently
	// invert every positional decision here.
	Button int

	// Cards is our hand: seeded by deal-hole and updated on every
	// draw-result for our seat.
	Cards []cards.Card

	Street int
	Label  string

	// Wagers is how many wagers have been made on the current street, the
	// big blind counting as the first predraw — the same convention the
	// engine's raise cap uses (WIRE_PROTOCOL.md:112-117). It is the one
	// piece of betting history the strategy needs: it separates an
	// unopened pot from a raised one, which is the difference between the
	// button's opening range and its defending range.
	Wagers int

	draws [MaxSeats][Streets]int
}

// Table is the whole bot-visible world: one match, one hand at a time.
type Table struct {
	Match Match
	Hand  Hand
}

// New returns a table with no match and no hand yet.
func New() *Table { return &Table{} }

// Hello records the per-match parameters.
func (t *Table) Hello(msg wire.Message) {
	t.Match = Match{
		GameID:        msg.GameID,
		SeatCount:     msg.SeatCount,
		StartingStack: msg.StartingStack,
		SmallBlind:    msg.Stakes.SmallBlind,
		BigBlind:      msg.Stakes.BigBlind,
		Ante:          msg.Stakes.Ante,
	}
	if msg.Betting.RaiseCap != nil {
		t.Match.RaiseCap = int(*msg.Betting.RaiseCap)
	}
	if msg.TimeoutMs != nil {
		t.Match.TimeoutMs = *msg.TimeoutMs
	}
}

// HandStart begins a new hand at the seat the arena assigned us.
func (t *Table) HandStart(msg wire.Message) {
	t.Hand = Hand{No: int64(msg.HandNo), Seat: msg.Seat, Street: Predraw, Label: "predraw"}
	for seat := range t.Hand.draws {
		for street := range t.Hand.draws[seat] {
			t.Hand.draws[seat][street] = unknownDraw
		}
	}
}

// Observe folds one event into the hand state. Events this bot has no use
// for — posts, pot awards, showdowns — are ignored rather than tracked,
// because nothing in the strategy reads them.
func (t *Table) Observe(event wire.Event) {
	hand := &t.Hand
	switch event.Kind {
	case wire.EventHandStart:
		hand.Button = event.Button

	case wire.EventPost:
		// The big blind is the opening wager of the predraw round.
		if event.PostKind == "big-blind" {
			hand.Wagers++
		}

	case wire.EventStreetStart:
		// On the draw streets this arrives before the draw phase, so the
		// draw counts recorded below land on the right street.
		//
		// On street 0 it arrives *after* the blinds are posted, which is
		// the documented trap (docs/game/rules.md, "Decoder gotcha"): resetting
		// per-street betting state here would wipe the blind and make the
		// first predraw raise look like an unopened pot. Hence the guard.
		hand.Street = event.Street
		hand.Label = event.Label
		if event.Street != Predraw {
			hand.Wagers = 0
		}

	case wire.EventActed:
		switch event.Action.Kind {
		case wire.ActionBet, wire.ActionRaise:
			hand.Wagers++
		}

	case wire.EventDealHole:
		// Redacted for other seats: cards empty, count still populated.
		if event.Seat == hand.Seat && len(event.Cards) > 0 {
			hand.Cards = cards.With(hand.Cards, event.Cards)
		}

	case wire.EventDrawResult:
		if event.Seat >= 0 && event.Seat < MaxSeats &&
			hand.Street >= 0 && hand.Street < Streets {
			hand.draws[event.Seat][hand.Street] = event.Count
		}
		if event.Seat == hand.Seat {
			hand.Cards = cards.With(
				cards.Without(hand.Cards, event.Discarded), event.Drawn)
		}
	}
}

// Opened reports whether anyone has wagered voluntarily on this street.
// Predraw that means someone raised over the blind; later it means a bet.
func (h *Hand) Opened() bool {
	if h.Street == Predraw {
		return h.Wagers > 1
	}
	return h.Wagers > 0
}

// FacingRaise reports whether the wager we face has already been raised at
// least once — the point at which a marginal made hand stops being worth a
// call on a big-bet street.
func (h *Hand) FacingRaise() bool {
	if h.Street == Predraw {
		return h.Wagers > 2
	}
	return h.Wagers > 1
}

// OnButton reports whether we hold the button this hand. Heads-up the button
// posts the small blind, acts first before the draw and last on every draw
// street — and, because everyone draws in seat order from left of the button,
// draws last as well.
func (h *Hand) OnButton() bool { return h.Seat == h.Button }

// Complete reports whether we hold a full five-card hand, which is the
// precondition for evaluating one.
func (h *Hand) Complete() bool { return len(h.Cards) == deuce.HandSize }

// Category places our current hand on the strength ladder. It is meaningful
// only once Complete reports true.
func (h *Hand) Category() deuce.Category {
	if !h.Complete() {
		return deuce.Broken
	}
	return deuce.Categorize(h.Cards)
}

// DrawCount reports how many cards a seat drew on a street, and whether that
// is known yet.
func (h *Hand) DrawCount(seat, street int) (int, bool) {
	if seat < 0 || seat >= MaxSeats || street < 0 || street >= Streets {
		return 0, false
	}
	count := h.draws[seat][street]
	return count, count != unknownDraw
}

// OpponentDraw reports the most recent draw count known for an opponent, at
// or before the given street, and how many streets back that reading came
// from — 0 for the current street.
//
// The staleness is the point. Draws run in seat order from left of the
// button, so heads-up the big blind draws first and knows only the previous
// street's count, while the button draws last and sees the current one
// (docs/game/rules.md). That asymmetry is worth three extra reads a hand
// and the strategy must not pretend it away.
//
// With more than one opponent it reports the *smallest* count, since the
// opponent drawing fewest cards is the one most likely to already be made.
func (h *Hand) OpponentDraw(street int) (count, streetsAgo int, known bool) {
	for back := 0; street-back >= 0; back++ {
		best, found := 0, false
		for seat := 0; seat < MaxSeats; seat++ {
			if seat == h.Seat {
				continue
			}
			seen, ok := h.DrawCount(seat, street-back)
			if !ok {
				continue
			}
			if !found || seen < best {
				best, found = seen, true
			}
		}
		if found {
			return best, back, true
		}
	}
	return 0, 0, false
}
