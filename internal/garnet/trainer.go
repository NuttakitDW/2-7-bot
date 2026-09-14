package garnet

import (
	"fmt"
	"math/rand/v2"
	"sort"

	"github.com/nuttakit/2-7-bot/internal/beryl"
	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/cfr"
	"github.com/nuttakit/2-7-bot/internal/sixmaxsim"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

type TrainerConfig struct {
	Ranking     *Ranking
	Percentages Percentages
	BucketCount int
	Seed        uint64
	DrawModel   cfr.Model
}

type trainingSet struct {
	regret         [2]float64
	sum            [2]float64
	updates        uint64
	averageUpdates uint64
	pos            Position
	ctx            Context
	bucket         uint8
}

type Trainer struct {
	config   TrainerConfig
	gate     *Gate
	bucketer *Bucketer
	sets     map[string]*trainingSet
	rng      *rand.Rand
	rollout  uint64
	trained  uint64
}

type entryState struct {
	decided  [sixmaxsim.Seats]bool
	admitted [sixmaxsim.Seats]bool
}

func (t *Trainer) Rollouts() uint64 {
	if t == nil {
		return 0
	}
	return t.rollout
}

func NewTrainer(config TrainerConfig) (*Trainer, error) {
	if config.DrawModel == nil {
		return nil, fmt.Errorf("garnet trainer: nil draw model")
	}
	gate, err := NewGate(config.Ranking, config.Percentages)
	if err != nil {
		return nil, err
	}
	bucketer, err := NewBucketer(config.Ranking, config.BucketCount)
	if err != nil {
		return nil, err
	}
	return &Trainer{config: config, gate: gate, bucketer: bucketer, sets: make(map[string]*trainingSet),
		rng: rand.New(rand.NewPCG(config.Seed, config.Seed^0xD1B54A32D192ED03))}, nil
}

// Train runs serial external-sampling MCCFR. Serial execution makes a seed
// reproduce the complete regret and average-policy trajectory.
func (t *Trainer) Train(iterations uint64) error {
	for i := uint64(0); i < iterations; i++ {
		dealSeed := t.rng.Uint64()
		root, err := sixmaxsim.New(sixmaxsim.Config{Seed: dealSeed, HandNo: t.trained + i})
		if err != nil {
			return err
		}
		for traverser := 0; traverser < sixmaxsim.Seats; traverser++ {
			if _, err := t.walkRegret(root.Clone(), entryState{}, traverser); err != nil {
				return err
			}
		}
		var reach [sixmaxsim.Seats]float64
		for seat := range reach {
			reach[seat] = 1
		}
		if err := t.walkAverage(root.Clone(), entryState{}, reach, float64(t.trained+i+1)); err != nil {
			return err
		}
	}
	t.trained += iterations
	return nil
}

func (t *Trainer) walkRegret(game *sixmaxsim.Game, entries entryState, traverser int) (float64, error) {
	if game.Active&(1<<traverser) == 0 {
		return -float64(game.Total[traverser]), nil
	}
	if game.Done() || game.Street != sixmaxsim.Predraw {
		return t.settle(game, traverser)
	}
	actor, decision, ok := game.Legal()
	if !ok {
		return 0, fmt.Errorf("garnet trainer: live predraw has no legal actor")
	}
	view := viewFromGame(game, actor, decision, entries)
	admitted := entries.admitted[actor]
	if !entries.decided[actor] {
		entries.decided[actor] = true
		admitted = t.gate.Admit(Position(view.Position), ContextFor(view), view.Hand)
		entries.admitted[actor] = admitted
	}
	actions := policyActions(decision, admitted)
	if len(actions) == 1 {
		child := game.Clone()
		if err := child.Apply(actions[0]); err != nil {
			return 0, err
		}
		return t.walkRegret(child, entries, traverser)
	}
	bucket, ok := t.bucketer.Bucket(view.Hand)
	if !ok {
		return 0, fmt.Errorf("garnet trainer: invalid actor hand")
	}
	set := t.setFor(view, bucket)
	sigma := regretMatch(set.regret)
	if actor != traverser {
		action := sampleAction(sigma, t.rng.Float64())
		child := game.Clone()
		if err := child.Apply(actions[action]); err != nil {
			return 0, err
		}
		return t.walkRegret(child, entries, traverser)
	}
	var values [2]float64
	for action := range actions {
		child := game.Clone()
		if err := child.Apply(actions[action]); err != nil {
			return 0, err
		}
		value, err := t.walkRegret(child, entries, traverser)
		if err != nil {
			return 0, err
		}
		values[action] = value
	}
	updateRegrets(&set.regret, values, sigma)
	set.updates++
	return sigma[0]*values[0] + sigma[1]*values[1], nil
}

func (t *Trainer) walkAverage(game *sixmaxsim.Game, entries entryState, reach [sixmaxsim.Seats]float64, weight float64) error {
	if game.Done() || game.Street != sixmaxsim.Predraw {
		return nil
	}
	actor, decision, ok := game.Legal()
	if !ok {
		return fmt.Errorf("garnet trainer: live predraw has no legal actor")
	}
	view := viewFromGame(game, actor, decision, entries)
	admitted := entries.admitted[actor]
	if !entries.decided[actor] {
		entries.decided[actor] = true
		admitted = t.gate.Admit(Position(view.Position), ContextFor(view), view.Hand)
		entries.admitted[actor] = admitted
	}
	actions := policyActions(decision, admitted)
	if len(actions) == 1 {
		if err := game.Apply(actions[0]); err != nil {
			return err
		}
		return t.walkAverage(game, entries, reach, weight)
	}
	bucket, ok := t.bucketer.Bucket(view.Hand)
	if !ok {
		return fmt.Errorf("garnet trainer: invalid actor hand")
	}
	set := t.setFor(view, bucket)
	sigma := regretMatch(set.regret)
	if reach[actor] > 0 {
		accumulateAverage(&set.sum, reach[actor], sigma, weight)
		set.averageUpdates++
	}
	for action := range actions {
		child := game.Clone()
		if err := child.Apply(actions[action]); err != nil {
			return err
		}
		childReach := reach
		childReach[actor] *= sigma[action]
		if err := t.walkAverage(child, entries, childReach, weight); err != nil {
			return err
		}
	}
	return nil
}

func (t *Trainer) settle(game *sixmaxsim.Game, traverser int) (float64, error) {
	if !game.Done() {
		t.rollout++
		if err := game.Continue(t.referenceFactory(t.config.Seed ^ t.rollout*0x9E3779B97F4A7C15)); err != nil {
			return 0, err
		}
	}
	payoffs, ok := game.Payoffs()
	if !ok {
		return 0, fmt.Errorf("garnet trainer: continuation did not settle")
	}
	return float64(payoffs[traverser]), nil
}

func (t *Trainer) referenceFactory(root uint64) sixmaxsim.Factory {
	return func(seat int, seed uint64) (sixmaxsim.Player, error) {
		return beryl.NewDeterministic(t.config.DrawModel, root^seed^uint64(seat+1)*0xD1B54A32D192ED03)
	}
}

func (t *Trainer) setFor(view beryl.PredrawView, bucket uint8) *trainingSet {
	key := KeyFor(view, bucket)
	set := t.sets[key]
	if set == nil {
		set = &trainingSet{pos: Position(view.Position), ctx: ContextFor(view), bucket: bucket}
		t.sets[key] = set
	}
	return set
}

func (t *Trainer) Policy(rankingHash string) (*Policy, error) {
	percentagesHash, err := PercentagesHash(t.config.Percentages)
	if err != nil {
		return nil, err
	}
	p := &Policy{Version: PolicyVersion, Algorithm: "six-player-external-sampling-cfr+; separate-own-reach-average-pass", RankingHash: rankingHash, PercentagesHash: percentagesHash,
		Percentages: t.config.Percentages, BucketCount: t.config.BucketCount, Seed: t.config.Seed,
		Iterations: t.trained, Infosets: make(map[string]Strategy, len(t.sets))}
	type aggregate struct {
		sum     [2]float64
		updates uint64
	}
	var backoff [Positions][Contexts][]aggregate
	for position := Position(0); position < Positions; position++ {
		for context := Context(0); context < Contexts; context++ {
			backoff[position][context] = make([]aggregate, t.config.BucketCount)
		}
	}
	keys := make([]string, 0, len(t.sets))
	for key := range t.sets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		set := t.sets[key]
		p.Stats.RegretUpdates += set.updates
		p.Stats.AverageUpdates += set.averageUpdates
		if set.updates == 0 || set.sum[0]+set.sum[1] <= 0 {
			continue
		}
		p.Infosets[key] = normalizedStrategy(set.sum, set.updates)
		aggregate := &backoff[set.pos][set.ctx][set.bucket]
		aggregate.sum[0] += set.sum[0]
		aggregate.sum[1] += set.sum[1]
		aggregate.updates += set.updates
	}
	for position := Position(0); position < Positions; position++ {
		for context := Context(0); context < Contexts; context++ {
			p.Backoff[position][context] = make([]Strategy, t.config.BucketCount)
			for bucket, aggregate := range backoff[position][context] {
				if aggregate.updates > 0 {
					p.Backoff[position][context][bucket] = normalizedStrategy(aggregate.sum, aggregate.updates)
				}
			}
		}
	}
	return p, p.Validate(rankingHash)
}

func normalizedStrategy(sum [2]float64, visits uint64) Strategy {
	total := sum[0] + sum[1]
	if total <= 0 {
		return Strategy{Passive: .5, Raise: .5, Visits: visits}
	}
	return Strategy{Passive: sum[0] / total, Raise: sum[1] / total, Visits: visits}
}

func regretMatch(regrets [2]float64) [2]float64 {
	a, b := max(0, regrets[0]), max(0, regrets[1])
	if a+b == 0 {
		return [2]float64{.5, .5}
	}
	return [2]float64{a / (a + b), b / (a + b)}
}

func updateRegrets(regrets *[2]float64, values, sigma [2]float64) {
	utility := sigma[0]*values[0] + sigma[1]*values[1]
	for action := range regrets {
		regrets[action] = max(0, regrets[action]+values[action]-utility)
	}
}

func accumulateAverage(sum *[2]float64, actorReach float64, sigma [2]float64, weight float64) {
	for action := range sum {
		sum[action] += weight * actorReach * sigma[action]
	}
}

func sampleAction(sigma [2]float64, draw float64) int {
	if draw < sigma[0] {
		return 0
	}
	return 1
}

func policyActions(decision wire.Decision, admitted bool) []wire.Action {
	passive := wire.Check()
	if decision.Call != nil {
		passive = wire.Call()
	}
	if !admitted {
		if decision.Fold {
			return []wire.Action{wire.Fold()}
		}
		return []wire.Action{passive}
	}
	if decision.Raise != nil {
		return []wire.Action{passive, wire.Raise(decision.Raise.MinTo)}
	}
	if decision.Bet != nil {
		return []wire.Action{passive, wire.Bet(decision.Bet.MinTo)}
	}
	return []wire.Action{passive}
}

// viewFromGame is the single simulator-to-runtime normalization boundary.
// All seats are positions relative to button zero, matching Beryl's wire view.
func viewFromGame(game *sixmaxsim.Game, actor int, decision wire.Decision, entries entryState) beryl.PredrawView {
	view := beryl.PredrawView{Hand: append([]cards.Card(nil), game.Hands[actor]...), Position: beryl.Position(actor),
		Decision: decision, Active: game.Active, Commitments: game.Committed, Wagers: game.Wagers,
		Raises: game.Wagers - 1, EntryDecided: entries.decided[actor], Admitted: entries.admitted[actor]}
	for _, event := range game.Events {
		if event.Kind != wire.EventActed || event.Street != sixmaxsim.Predraw {
			continue
		}
		if event.Action.Kind == wire.ActionCall {
			view.Calls++
		}
		view.Actions = append(view.Actions, beryl.PredrawAction{Position: beryl.Position(event.Seat), Action: event.Action.Kind, Commit: event.StreetCommit})
	}
	return view
}
