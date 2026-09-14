package garnet

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"

	"github.com/nuttakit/2-7-bot/internal/beryl"
	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/handclass"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

const PolicyVersion = 1

type Strategy struct {
	Passive float64 `json:"passive"`
	Raise   float64 `json:"raise"`
	Visits  uint64  `json:"visits"`
}

type LookupKind uint8

const (
	LookupUntrained LookupKind = iota
	LookupBackoff
	LookupExact
)

type Policy struct {
	Version         int                             `json:"version"`
	Algorithm       string                          `json:"algorithm"`
	RankingHash     string                          `json:"ranking_hash"`
	PercentagesHash string                          `json:"percentages_hash"`
	Percentages     Percentages                     `json:"percentages"`
	BucketCount     int                             `json:"bucket_count"`
	Seed            uint64                          `json:"seed"`
	Iterations      uint64                          `json:"iterations"`
	Stats           TrainingStats                   `json:"stats"`
	Infosets        map[string]Strategy             `json:"infosets"`
	Backoff         [Positions][Contexts][]Strategy `json:"backoff"`
}

type TrainingStats struct {
	RegretUpdates  uint64 `json:"regret_updates"`
	AverageUpdates uint64 `json:"average_updates"`
}

func (p *Policy) Validate(expectedRankingHash string) error {
	if p == nil || p.Version != PolicyVersion {
		return fmt.Errorf("garnet policy: unsupported version")
	}
	if len(p.RankingHash) != 64 || expectedRankingHash != "" && p.RankingHash != expectedRankingHash {
		return fmt.Errorf("garnet policy: stale or invalid ranking hash")
	}
	percentagesHash, err := PercentagesHash(p.Percentages)
	if err != nil || p.PercentagesHash != percentagesHash {
		return fmt.Errorf("garnet policy: stale or invalid percentage hash")
	}
	if p.BucketCount != 16 && p.BucketCount != 32 {
		return fmt.Errorf("garnet policy: unsupported bucket count %d", p.BucketCount)
	}
	if p.Iterations == 0 || p.Algorithm == "" {
		return fmt.Errorf("garnet policy: no training iterations")
	}
	for key, strategy := range p.Infosets {
		if len(key) != 64 {
			return fmt.Errorf("garnet policy: malformed infoset key")
		}
		if err := validateStrategy(strategy); err != nil {
			return fmt.Errorf("garnet policy: infoset %s: %w", key, err)
		}
	}
	for position := Position(0); position < Positions; position++ {
		for context := Context(0); context < Contexts; context++ {
			row := p.Backoff[position][context]
			if len(row) != 0 && len(row) != p.BucketCount {
				return fmt.Errorf("garnet policy: backoff %d/%d has %d buckets", position, context, len(row))
			}
			for _, strategy := range row {
				if strategy.Visits == 0 {
					if strategy.Passive != 0 || strategy.Raise != 0 {
						return fmt.Errorf("garnet policy: unvisited backoff has probability")
					}
					continue
				}
				if err := validateStrategy(strategy); err != nil {
					return fmt.Errorf("garnet policy: backoff %d/%d: %w", position, context, err)
				}
			}
		}
	}
	return nil
}

func validateStrategy(strategy Strategy) error {
	if strategy.Visits == 0 || !finite(strategy.Passive) || !finite(strategy.Raise) || strategy.Passive < 0 || strategy.Raise < 0 || math.Abs(strategy.Passive+strategy.Raise-1) > 1e-9 {
		return fmt.Errorf("invalid strategy")
	}
	return nil
}

func EncodePolicy(p *Policy) ([]byte, error) {
	if err := p.Validate(p.RankingHash); err != nil {
		return nil, err
	}
	return json.MarshalIndent(p, "", "  ")
}

func DecodePolicy(raw []byte) (*Policy, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var policy Policy
	if err := decoder.Decode(&policy); err != nil {
		return nil, fmt.Errorf("garnet policy: decode: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("garnet policy: trailing data")
	}
	if err := policy.Validate(policy.RankingHash); err != nil {
		return nil, err
	}
	return &policy, nil
}

func RankingHash(ranking *Ranking) (string, error) {
	raw, err := EncodeRanking(ranking)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func PercentagesHash(percentages Percentages) (string, error) {
	for position := Position(0); position < Positions; position++ {
		for context := Context(0); context < Contexts; context++ {
			pct := percentages[position][context]
			if !finite(pct) || pct < 0 || pct > 100 {
				return "", fmt.Errorf("garnet percentages: invalid %.4g at %d/%d", pct, position, context)
			}
		}
	}
	raw, err := json.Marshal(percentages)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

type Bucketer struct {
	buckets int
	start   [handclass.Num]int
}

func NewBucketer(ranking *Ranking, buckets int) (*Bucketer, error) {
	if err := ranking.Validate(); err != nil {
		return nil, err
	}
	if buckets != 16 && buckets != 32 {
		return nil, fmt.Errorf("garnet bucketer: unsupported count %d", buckets)
	}
	ordered := append([]ClassEV(nil), ranking.Classes...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].BestEV == ordered[j].BestEV {
			return ordered[i].ID < ordered[j].ID
		}
		return ordered[i].BestEV < ordered[j].BestEV
	})
	b := &Bucketer{buckets: buckets}
	cumulative := 0
	for _, class := range ordered {
		b.start[class.ID] = cumulative
		cumulative += class.Weight
	}
	return b, nil
}

func (b *Bucketer) Bucket(hand []cards.Card) (uint8, bool) {
	if b == nil || len(hand) != 5 || cards.NewSet(hand).Len() != 5 {
		return 0, false
	}
	id := handclass.Of(hand)
	ordinal, ok := physicalOrdinal(hand)
	if !ok {
		return 0, false
	}
	bucket := (b.start[id] + ordinal) * b.buckets / cards.Deals
	if bucket >= b.buckets {
		bucket = b.buckets - 1
	}
	return uint8(bucket), true
}

// KeyFor hashes only button-relative public predraw state plus the acting
// player's EV bucket. Beryl deliberately omits every opponent private card.
func KeyFor(view beryl.PredrawView, bucket uint8) string {
	buf := make([]byte, 0, 128)
	buf = append(buf, byte(view.Position), bucket, view.Active, byte(view.Wagers))
	for _, commitment := range view.Commitments {
		buf = binary.LittleEndian.AppendUint64(buf, commitment)
	}
	buf = append(buf, byte(len(view.Actions)))
	for _, action := range view.Actions {
		buf = append(buf, byte(action.Position), actionCode(action.Action))
		buf = binary.LittleEndian.AppendUint64(buf, action.Commit)
	}
	buf = append(buf, boolByte(view.Decision.Check), boolByte(view.Decision.Call != nil), boolByte(view.Decision.Raise != nil))
	if view.Decision.Call != nil {
		buf = binary.LittleEndian.AppendUint64(buf, *view.Decision.Call)
	}
	if view.Decision.Raise != nil {
		buf = binary.LittleEndian.AppendUint64(buf, view.Decision.Raise.MinTo)
		buf = binary.LittleEndian.AppendUint64(buf, view.Decision.Raise.MaxTo)
	}
	digest := sha256.Sum256(buf)
	return hex.EncodeToString(digest[:])
}

func ContextFor(view beryl.PredrawView) Context {
	switch {
	case view.Raises == 0 && view.Calls == 0:
		return Unopened
	case view.Raises == 0:
		return Limped
	case view.Raises == 1:
		return SingleOpen
	default:
		return MultipleOpen
	}
}

func (p *Policy) Lookup(key string, position Position, context Context, bucket uint8) (Strategy, LookupKind) {
	if p != nil {
		if strategy, ok := p.Infosets[key]; ok && strategy.Visits > 0 {
			return strategy, LookupExact
		}
		if position < Positions && context < Contexts && int(bucket) < p.BucketCount {
			row := p.Backoff[position][context]
			if int(bucket) < len(row) && row[bucket].Visits > 0 {
				return row[bucket], LookupBackoff
			}
		}
	}
	return Strategy{Passive: 1}, LookupUntrained
}

func actionCode(action string) byte {
	switch action {
	case wire.ActionFold:
		return 1
	case wire.ActionCheck:
		return 2
	case wire.ActionCall:
		return 3
	case wire.ActionBet:
		return 4
	case wire.ActionRaise:
		return 5
	default:
		return 0
	}
}

func boolByte(value bool) byte {
	if value {
		return 1
	}
	return 0
}
