// Package sixmaxsim is a cloneable, exact-stakes simulator for six-handed
// fixed-limit 2-7 triple draw. It is intentionally limited to the arena's
// default 50/100 blinds, 100/200 bets, 10,000 stacks, and four-wager cap.
package sixmaxsim

import (
	"fmt"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/deuce"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

const (
	Seats         = 6
	Predraw       = 0
	Draw1         = 1
	Draw2         = 2
	Draw3         = 3
	SmallBlind    = uint64(50)
	BigBlind      = uint64(100)
	SmallBet      = uint64(100)
	BigBet        = uint64(200)
	StartingStack = uint64(10_000)
	RaiseCap      = 4
	reshuffleSalt = uint64(0x5245534855464C31)
)

type phase uint8

const (
	betting phase = iota
	drawing
)

// ForcedHand conditions the deal on one seat holding five specified cards.
type ForcedHand struct {
	Seat  int
	Cards []cards.Card
}

// Config defines one independently reproducible hand. Button is always seat
// zero, matching the training abstraction and duplicate-deal engine hands.
type Config struct {
	Seed   uint64
	HandNo uint64
	Forced *ForcedHand
}

// Player is the bot-facing protocol surface. Implementations reconstruct
// their state exclusively from the redacted event stream.
type Player interface {
	Hello(wire.Message)
	HandStart(wire.Message)
	Observe(wire.Event)
	Decide(wire.Decision) wire.Action
}

// Factory creates a fresh policy instance for one continuation branch.
type Factory func(seat int, seed uint64) (Player, error)

// Game holds both public training state and private chance state. Hands are
// exposed only so a trainer can identify its sampled chance outcome; policies
// receive cards through redacted events instead.
type Game struct {
	Actor     int
	Street    int
	Active    uint8
	Committed [Seats]uint64
	Total     [Seats]uint64
	Wagers    int
	Events    []wire.Event
	Hands     [Seats][]cards.Card

	Seed    uint64
	HandNo  uint64
	stacks  [Seats]uint64
	acted   [Seats]bool
	current uint64
	phase   phase
	deck    []cards.Card
	muck    []cards.Card
	rng     rng64
	done    bool
	payoffs [Seats]int64
}

// New samples one shared physical deck and opens the predraw betting round.
func New(config Config) (*Game, error) {
	forced, err := validateForced(config.Forced)
	if err != nil {
		return nil, err
	}
	dealRNG := newRNG(config.Seed, config.HandNo)
	deck := fullDeck()
	if forced != nil {
		for _, card := range forced.Cards {
			deck = removeCard(deck, card)
		}
	}
	dealRNG.shuffle(deck)
	// The engine's shuffled deck deals by popping from the end. Store the
	// resulting deal order directly so draw and clone operations stay simple.
	reverse(deck)

	g := &Game{
		Actor:  3,
		Street: Predraw,
		Active: (1 << Seats) - 1,
		Wagers: 1,
		Seed:   config.Seed,
		HandNo: config.HandNo,
		deck:   deck,
		rng:    newRNG(config.Seed^reshuffleSalt, config.HandNo),
	}
	for seat := 0; seat < Seats; seat++ {
		g.stacks[seat] = StartingStack
	}
	g.emit(wire.Event{Kind: wire.EventHandStart, HandNo: config.HandNo, Button: 0, Stacks: repeat(StartingStack)})
	g.pay(1, SmallBlind)
	g.emit(wire.Event{Kind: wire.EventPost, Seat: 1, PostKind: "small-blind", Amount: SmallBlind})
	g.pay(2, BigBlind)
	g.emit(wire.Event{Kind: wire.EventPost, Seat: 2, PostKind: "big-blind", Amount: BigBlind})
	g.current = BigBlind
	g.emit(wire.Event{Kind: wire.EventStreetStart, Street: Predraw, Label: streetLabel(Predraw)})
	for offset := 1; offset <= Seats; offset++ {
		seat := offset % Seats
		var hand []cards.Card
		if forced != nil && seat == forced.Seat {
			hand = append([]cards.Card(nil), forced.Cards...)
		} else {
			hand = g.draw(5)
		}
		g.Hands[seat] = hand
		g.emit(wire.Event{Kind: wire.EventDealHole, Seat: seat, Cards: append([]cards.Card(nil), hand...), Count: 5})
	}
	return g, nil
}

func validateForced(forced *ForcedHand) (*ForcedHand, error) {
	if forced == nil {
		return nil, nil
	}
	if forced.Seat < 0 || forced.Seat >= Seats {
		return nil, fmt.Errorf("sixmaxsim: forced seat %d outside 0..5", forced.Seat)
	}
	if len(forced.Cards) != 5 {
		return nil, fmt.Errorf("sixmaxsim: forced hand has %d cards, want 5", len(forced.Cards))
	}
	seen := map[cards.Card]bool{}
	for _, card := range forced.Cards {
		if !validCard(card) {
			return nil, fmt.Errorf("sixmaxsim: invalid forced card %q", card.String())
		}
		if seen[card] {
			return nil, fmt.Errorf("sixmaxsim: duplicate forced card %s", card)
		}
		seen[card] = true
	}
	return &ForcedHand{Seat: forced.Seat, Cards: append([]cards.Card(nil), forced.Cards...)}, nil
}

// Clone deeply copies all mutable chance, hand, muck, and history state.
func (g *Game) Clone() *Game {
	clone := *g
	clone.deck = append([]cards.Card(nil), g.deck...)
	clone.muck = append([]cards.Card(nil), g.muck...)
	for seat := 0; seat < Seats; seat++ {
		clone.Hands[seat] = append([]cards.Card(nil), g.Hands[seat]...)
	}
	clone.Events = cloneEvents(g.Events)
	return &clone
}

// Done reports whether the hand has settled.
func (g *Game) Done() bool { return g.done }

// Payoffs returns per-seat net chips once the hand has settled.
func (g *Game) Payoffs() ([Seats]int64, bool) { return g.payoffs, g.done }

// Legal returns the acting seat and its authoritative wire decision.
func (g *Game) Legal() (int, wire.Decision, bool) {
	if g.done {
		return -1, wire.Decision{}, false
	}
	if g.phase == drawing {
		return g.Actor, wire.Decision{Kind: wire.DecisionDraw, MaxDiscards: 5}, true
	}
	seat := g.Actor
	owed := g.current - g.Committed[seat]
	d := wire.Decision{Kind: wire.DecisionWager, Fold: owed > 0, Check: owed == 0}
	if owed > 0 {
		amount := owed
		d.Call = &amount
	}
	if g.Wagers < RaiseCap {
		tier := betSize(g.Street)
		if g.current == 0 {
			d.Bet = &wire.Range{MinTo: tier, MaxTo: tier}
		} else {
			to := g.current + tier
			d.Raise = &wire.Range{MinTo: to, MaxTo: to}
		}
	}
	return seat, d, true
}

// Apply validates and applies the current actor's action atomically.
func (g *Game) Apply(action wire.Action) error {
	seat, legal, ok := g.Legal()
	if !ok {
		return fmt.Errorf("sixmaxsim: hand is over")
	}
	if err := conforms(action, legal, g.Hands[seat]); err != nil {
		return fmt.Errorf("sixmaxsim: seat %d: %w", seat, err)
	}
	if g.phase == drawing {
		g.applyDraw(seat, action.Cards)
		return nil
	}

	switch action.Kind {
	case wire.ActionFold:
		g.Active &^= 1 << seat
		g.muck = append(g.muck, g.Hands[seat]...)
		g.Hands[seat] = nil
	case wire.ActionCheck:
	case wire.ActionCall:
		g.pay(seat, g.current-g.Committed[seat])
	case wire.ActionBet, wire.ActionRaise:
		g.pay(seat, action.To-g.Committed[seat])
		g.current = action.To
		g.Wagers++
		for other := 0; other < Seats; other++ {
			if other != seat && g.live(other) {
				g.acted[other] = false
			}
		}
	}
	g.acted[seat] = true
	g.emit(wire.Event{Kind: wire.EventActed, Seat: seat, Action: cloneAction(action), StreetCommit: g.Committed[seat]})
	if g.liveCount() == 1 || g.roundComplete() {
		g.advance()
	} else {
		g.Actor = g.nextBetActor(seat)
	}
	return nil
}

func conforms(action wire.Action, legal wire.Decision, hand []cards.Card) error {
	switch action.Kind {
	case wire.ActionFold:
		if !legal.Fold {
			return fmt.Errorf("fold is not legal")
		}
	case wire.ActionCheck:
		if !legal.Check {
			return fmt.Errorf("check is not legal")
		}
	case wire.ActionCall:
		if legal.Call == nil {
			return fmt.Errorf("call is not legal")
		}
	case wire.ActionBet:
		if legal.Bet == nil || action.To != legal.Bet.MinTo {
			return fmt.Errorf("bet to %d is not legal", action.To)
		}
	case wire.ActionRaise:
		if legal.Raise == nil || action.To != legal.Raise.MinTo {
			return fmt.Errorf("raise to %d is not legal", action.To)
		}
	case wire.ActionDiscard:
		if legal.Kind != wire.DecisionDraw || len(action.Cards) > legal.MaxDiscards {
			return fmt.Errorf("discard count is not legal")
		}
		available := append([]cards.Card(nil), hand...)
		for _, card := range action.Cards {
			at := indexCard(available, card)
			if at < 0 {
				return fmt.Errorf("discard %s is duplicated or not held", card)
			}
			available = append(available[:at], available[at+1:]...)
		}
	default:
		return fmt.Errorf("unknown action %q", action.Kind)
	}
	return nil
}

func (g *Game) applyDraw(seat int, discarded []cards.Card) {
	for _, card := range discarded {
		at := indexCard(g.Hands[seat], card)
		g.Hands[seat] = append(g.Hands[seat][:at], g.Hands[seat][at+1:]...)
	}
	drawn := g.replacements(discarded)
	g.Hands[seat] = append(g.Hands[seat], drawn...)
	g.emit(wire.Event{
		Kind: wire.EventDrawResult, Seat: seat, Count: len(discarded),
		Discarded: append([]cards.Card(nil), discarded...), Drawn: append([]cards.Card(nil), drawn...),
	})
	if next, ok := g.nextDrawActor(seat); ok {
		g.Actor = next
		return
	}
	g.phase = betting
	g.Actor = g.firstLiveLeftOfButton()
}

func (g *Game) replacements(discarded []cards.Card) []cards.Card {
	drawn := make([]cards.Card, 0, len(discarded))
	recycledOwn := false
	for len(drawn) < len(discarded) {
		if len(g.deck) > 0 {
			drawn = append(drawn, g.draw(1)[0])
			continue
		}
		if len(g.muck) == 0 {
			if recycledOwn {
				break
			}
			g.muck = append(g.muck, discarded...)
			recycledOwn = true
		}
		g.rng.shuffle(g.muck)
		g.deck = append(g.deck[:0], g.muck...)
		g.muck = nil
	}
	if !recycledOwn {
		g.muck = append(g.muck, discarded...)
	}
	return drawn
}

func (g *Game) advance() {
	g.refundUncalled()
	if g.liveCount() == 1 {
		g.finishFoldout()
		return
	}
	if g.Street == Draw3 {
		g.finishShowdown()
		return
	}
	g.Street++
	g.Committed = [Seats]uint64{}
	g.acted = [Seats]bool{}
	g.current = 0
	g.Wagers = 0
	g.emit(wire.Event{Kind: wire.EventStreetStart, Street: g.Street, Label: streetLabel(g.Street)})
	g.phase = drawing
	g.Actor = g.firstLiveLeftOfButton()
}

func (g *Game) refundUncalled() {
	top := -1
	for seat := 0; seat < Seats; seat++ {
		if g.live(seat) && (top < 0 || g.Committed[seat] > g.Committed[top]) {
			top = seat
		}
	}
	if top < 0 {
		return
	}
	var matched uint64
	for seat := 0; seat < Seats; seat++ {
		if seat != top && g.Committed[seat] > matched {
			matched = g.Committed[seat]
		}
	}
	if g.Committed[top] <= matched {
		return
	}
	refund := g.Committed[top] - matched
	g.Committed[top] -= refund
	g.Total[top] -= refund
	g.stacks[top] += refund
}

func (g *Game) finishFoldout() {
	winner := g.firstLiveLeftOfButton()
	pot := sum(g.Total)
	g.finish([]int{winner}, pot)
}

func (g *Game) finishShowdown() {
	best := deuce.Value(0)
	winners := make([]int, 0, Seats)
	for offset := 1; offset <= Seats; offset++ {
		seat := offset % Seats
		if !g.live(seat) {
			continue
		}
		value := deuce.Eval(g.Hands[seat])
		v := uint64(value)
		g.emit(wire.Event{Kind: wire.EventShowdownShow, Seat: seat, Cards: append([]cards.Card(nil), g.Hands[seat]...), Hi: &v})
		switch {
		case value > best:
			best = value
			winners = winners[:0]
			winners = append(winners, seat)
		case value == best:
			winners = append(winners, seat)
		}
	}
	g.finish(winners, sum(g.Total))
}

func (g *Game) finish(winners []int, pot uint64) {
	amounts := make(map[int]uint64, len(winners))
	share, odd := pot/uint64(len(winners)), pot%uint64(len(winners))
	for _, seat := range winners {
		amounts[seat] = share
	}
	for offset := 1; odd > 0 && offset <= Seats; offset++ {
		seat := offset % Seats
		if _, wins := amounts[seat]; wins {
			amounts[seat]++
			odd--
		}
	}
	awards := make([][]int64, 0, len(winners))
	// Engine pot awards are sorted by seat after odd-chip bonuses are chosen.
	for seat := 0; seat < Seats; seat++ {
		if amount, wins := amounts[seat]; wins {
			awards = append(awards, []int64{int64(seat), int64(amount)})
			g.payoffs[seat] += int64(amount)
		}
	}
	for seat := 0; seat < Seats; seat++ {
		g.payoffs[seat] -= int64(g.Total[seat])
	}
	g.emit(wire.Event{Kind: wire.EventPotAwarded, Pot: 0, Side: "whole", Winners: awards})
	nets := make([]int64, Seats)
	copy(nets, g.payoffs[:])
	g.emit(wire.Event{Kind: wire.EventHandEnd, Nets: nets})
	g.done = true
	g.Actor = -1
}

func (g *Game) pay(seat int, amount uint64) {
	if amount > g.stacks[seat] {
		panic("sixmaxsim: default-stakes contract exceeded stack")
	}
	g.stacks[seat] -= amount
	g.Committed[seat] += amount
	g.Total[seat] += amount
}

func (g *Game) roundComplete() bool {
	for seat := 0; seat < Seats; seat++ {
		if g.live(seat) && (!g.acted[seat] || g.Committed[seat] != g.current) {
			return false
		}
	}
	return true
}

func (g *Game) nextBetActor(from int) int {
	for offset := 1; offset <= Seats; offset++ {
		seat := (from + offset) % Seats
		if g.live(seat) && (!g.acted[seat] || g.Committed[seat] != g.current) {
			return seat
		}
	}
	panic("sixmaxsim: no next actor")
}

func (g *Game) nextDrawActor(from int) (int, bool) {
	if from == 0 {
		return 0, false
	}
	for seat := from + 1; seat < Seats; seat++ {
		if g.live(seat) {
			return seat, true
		}
	}
	// Draw order is 1,2,3,4,5,0. Seat zero is last, never followed by one.
	if g.live(0) {
		return 0, true
	}
	return 0, false
}

func (g *Game) firstLiveLeftOfButton() int {
	for offset := 1; offset <= Seats; offset++ {
		seat := offset % Seats
		if g.live(seat) {
			return seat
		}
	}
	panic("sixmaxsim: no live seat")
}

func (g *Game) live(seat int) bool { return g.Active&(1<<seat) != 0 }

func (g *Game) liveCount() int {
	count := 0
	for seat := 0; seat < Seats; seat++ {
		if g.live(seat) {
			count++
		}
	}
	return count
}

func (g *Game) emit(event wire.Event) { g.Events = append(g.Events, event) }

func (g *Game) draw(count int) []cards.Card {
	if count > len(g.deck) {
		panic("sixmaxsim: deck exhausted")
	}
	drawn := append([]cards.Card(nil), g.deck[:count]...)
	g.deck = g.deck[count:]
	return drawn
}

func betSize(street int) uint64 {
	if street <= Draw1 {
		return SmallBet
	}
	return BigBet
}

func streetLabel(street int) string {
	return [...]string{"predraw", "draw1", "draw2", "draw3"}[street]
}

func repeat(value uint64) []uint64 {
	values := make([]uint64, Seats)
	for i := range values {
		values[i] = value
	}
	return values
}

func sum(values [Seats]uint64) uint64 {
	var total uint64
	for _, value := range values {
		total += value
	}
	return total
}

func fullDeck() []cards.Card {
	deck := make([]cards.Card, 0, 52)
	for rank := cards.Two; rank <= cards.Ace; rank++ {
		for _, suit := range []cards.Suit{cards.Clubs, cards.Diamonds, cards.Hearts, cards.Spades} {
			deck = append(deck, cards.Card{Rank: rank, Suit: suit})
		}
	}
	return deck
}

func validCard(card cards.Card) bool {
	if card.Rank < cards.Two || card.Rank > cards.Ace {
		return false
	}
	switch card.Suit {
	case cards.Clubs, cards.Diamonds, cards.Hearts, cards.Spades:
		return true
	default:
		return false
	}
}

func removeCard(deck []cards.Card, card cards.Card) []cards.Card {
	at := indexCard(deck, card)
	return append(deck[:at], deck[at+1:]...)
}

func reverse(deck []cards.Card) {
	for left, right := 0, len(deck)-1; left < right; left, right = left+1, right-1 {
		deck[left], deck[right] = deck[right], deck[left]
	}
}

func indexCard(hand []cards.Card, card cards.Card) int {
	for i, held := range hand {
		if held == card {
			return i
		}
	}
	return -1
}

func cloneAction(action wire.Action) wire.Action {
	action.Cards = append([]cards.Card(nil), action.Cards...)
	return action
}

func cloneEvents(events []wire.Event) []wire.Event {
	cloned := make([]wire.Event, len(events))
	for i, event := range events {
		cloned[i] = cloneEvent(event)
	}
	return cloned
}

func cloneEvent(event wire.Event) wire.Event {
	event.Stacks = append([]uint64(nil), event.Stacks...)
	event.Cards = append([]cards.Card(nil), event.Cards...)
	event.Action = cloneAction(event.Action)
	event.Discarded = append([]cards.Card(nil), event.Discarded...)
	event.Drawn = append([]cards.Card(nil), event.Drawn...)
	event.Nets = append([]int64(nil), event.Nets...)
	if event.Hi != nil {
		hi := *event.Hi
		event.Hi = &hi
	}
	if event.Lo != nil {
		lo := *event.Lo
		event.Lo = &lo
	}
	if event.Winners != nil {
		winners := make([][]int64, len(event.Winners))
		for i := range event.Winners {
			winners[i] = append([]int64(nil), event.Winners[i]...)
		}
		event.Winners = winners
	}
	return event
}

// EventFor returns one event as visible to observer. Private hole and draw
// cards are retained only for their owner; public counts remain.
func EventFor(event wire.Event, observer int) wire.Event {
	redacted := cloneEvent(event)
	if event.Seat == observer {
		return redacted
	}
	switch event.Kind {
	case wire.EventDealHole:
		redacted.Cards = []cards.Card{}
	case wire.EventDrawResult:
		redacted.Discarded = []cards.Card{}
		redacted.Drawn = []cards.Card{}
	}
	return redacted
}

// EventsFor returns a deeply copied, observer-redacted history.
func (g *Game) EventsFor(observer int) []wire.Event {
	events := make([]wire.Event, len(g.Events))
	for i, event := range g.Events {
		events[i] = EventFor(event, observer)
	}
	return events
}

// Continue creates fresh players, replays the branch prefix with private
// redaction, then drives the cloned leaf through settlement.
func (g *Game) Continue(factory Factory) error {
	if factory == nil {
		return fmt.Errorf("sixmaxsim: nil player factory")
	}
	var players [Seats]Player
	for seat := 0; seat < Seats; seat++ {
		player, err := factory(seat, g.Seed)
		if err != nil {
			return fmt.Errorf("sixmaxsim: create seat %d: %w", seat, err)
		}
		if player == nil {
			return fmt.Errorf("sixmaxsim: create seat %d: nil player", seat)
		}
		players[seat] = player
		player.Hello(hello())
		player.HandStart(wire.Message{Type: wire.MsgHandStart, HandNo: g.HandNo, Seat: seat})
		for _, event := range g.Events {
			player.Observe(EventFor(event, seat))
		}
	}
	for !g.done {
		seat, decision, _ := g.Legal()
		action := players[seat].Decide(decision)
		before := len(g.Events)
		if err := g.Apply(action); err != nil {
			return err
		}
		for _, event := range g.Events[before:] {
			for observer, player := range players {
				player.Observe(EventFor(event, observer))
			}
		}
	}
	return nil
}

func hello() wire.Message {
	cap := uint8(RaiseCap)
	return wire.Message{
		Type: wire.MsgHello, Proto: 1, GameID: "27td-fl",
		Stakes:  wire.Stakes{Kind: "blinds", SmallBlind: SmallBlind, BigBlind: BigBlind},
		Betting: wire.Betting{Kind: "fixed-limit", RaiseCap: &cap}, SeatCount: Seats,
		StartingStack: StartingStack,
	}
}

// rng64 mirrors the engine's xoshiro256** stream and is copied by value in
// Clone, so sibling actions consume independent but identical chance streams.
type rng64 struct{ state [4]uint64 }

func newRNG(seed, stream uint64) rng64 {
	a := seed
	b := stream ^ 0xD1B54A32D192ED03
	state := [4]uint64{splitmix(&a), splitmix(&a), splitmix(&b), splitmix(&b)}
	state[2] ^= splitmix(&a)
	state[3] ^= splitmix(&b)
	if state == [4]uint64{} {
		state = [4]uint64{0x9E3779B97F4A7C15, 0x9E3779B97F4A7C15, 0x9E3779B97F4A7C15, 0x9E3779B97F4A7C15}
	}
	return rng64{state: state}
}

func splitmix(state *uint64) uint64 {
	*state += 0x9E3779B97F4A7C15
	z := *state
	z = (z ^ z>>30) * 0xBF58476D1CE4E5B9
	z = (z ^ z>>27) * 0x94D049BB133111EB
	return z ^ z>>31
}

func (r *rng64) next() uint64 {
	result := bitsRotateLeft64(r.state[1]*5, 7) * 9
	t := r.state[1] << 17
	r.state[2] ^= r.state[0]
	r.state[3] ^= r.state[1]
	r.state[1] ^= r.state[2]
	r.state[0] ^= r.state[3]
	r.state[2] ^= t
	r.state[3] = bitsRotateLeft64(r.state[3], 45)
	return result
}

func (r *rng64) below(n uint64) uint64 {
	zone := ^uint64(0) - (^uint64(0) % n)
	for {
		value := r.next()
		if value < zone {
			return value % n
		}
	}
}

func (r *rng64) shuffle(items []cards.Card) {
	for i := len(items) - 1; i > 0; i-- {
		j := int(r.below(uint64(i + 1)))
		items[i], items[j] = items[j], items[i]
	}
}

func bitsRotateLeft64(value uint64, shift int) uint64 {
	return value<<shift | value>>(64-shift)
}
