package arena

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// The bot name is this repo's only versioning channel.
//
// An upload declares nothing but {name, games, playerCounts, size}: the platform
// assigns the version itself — immutable and digest-addressed — and offers the
// client no label, note or tag to go with it. Match results compound that, because
// PlayerStats carries a name and never a version id, so two versions of one bot in
// the same competition are indistinguishable in a report.
//
// The generation therefore has to live in the name, which is what every account on
// the roster already does. docs/naming.md is the rule; this file enforces it.
const (
	// BotNameOwner prefixes every bot we upload.
	BotNameOwner = "nutt"

	// MaxBotNameLen is our cap, not the platform's — the server publishes no
	// limit, and the longest name on the roster is 27 characters.
	MaxBotNameLen = 40
)

// Lineage letters: what kind of strategy the artifact carries.
const (
	LineageHeuristic  = 'h'
	LineageBlueprint  = 'b'
	LineageExperiment = 'x'
)

// seatCounts maps a seat token to the exact player counts it declares.
//
// Declared counts are exact — 4 does not imply 3 — so a token stands for a set,
// not a range. "all" is the full seat range the arena offers, so it tracks
// MinSeats/MaxSeats rather than restating them.
var seatCounts = map[string][]int{
	"hu":   {MinSeats},
	"6max": {MaxSeats},
	"hu6":  {MinSeats, MaxSeats},
	"all":  seatRange(MinSeats, MaxSeats),
}

func seatRange(low, high int) []int {
	counts := make([]int, 0, high-low+1)
	for count := low; count <= high; count++ {
		counts = append(counts, count)
	}
	return counts
}

// seatTokens fixes the order tokens are listed in and searched in.
var seatTokens = []string{"hu", "6max", "hu6", "all"}

// retiredNames predate the convention. They keep their standings and their
// snapshots in past competitions; nothing new is ever uploaded under them.
var retiredNames = map[string]bool{
	"nutt-27td-m1": true,
	"nutt-27td-fl": true,
}

// BotName is a parsed bot name: nutt-<game>-<seats>-<lineage><gen>[-<qualifier>]...
type BotName struct {
	Owner      string   // always BotNameOwner
	Game       string   // a registry game id, e.g. "27td-fl" — may contain hyphens
	Seats      string   // hu | 6max | hu6 | all
	Lineage    byte     // h heuristic | b blueprint | x experiment
	Generation int      // >= 1
	Qualifiers []string // build knobs, in order: iterations, quantisation, role
}

// ParseBotName splits a name into its segments, or explains which one is wrong.
func ParseBotName(name string) (BotName, error) {
	if name == "" {
		return BotName{}, fmt.Errorf("bot name is required")
	}
	if len(name) > MaxBotNameLen {
		return BotName{}, fmt.Errorf("bot name %q is %d characters, over the %d-character limit",
			name, len(name), MaxBotNameLen)
	}
	if err := checkBotNameCharset(name); err != nil {
		return BotName{}, err
	}
	if retiredNames[name] {
		return BotName{}, fmt.Errorf("bot name %q is retired; a new build takes a new name (see docs/naming.md)", name)
	}

	segments := strings.Split(name, "-")
	if strings.HasPrefix(name, CodenamePrefix) {
		return parseCodename(name, segments)
	}
	if segments[0] != BotNameOwner {
		return BotName{}, fmt.Errorf("bot name %q must start with %q", name, BotNameOwner+"-")
	}

	// The game id contains hyphens of its own, so the seat token is what marks
	// where it ends. Scanning left to right means a qualifier that happens to
	// spell a seat token cannot win, because the real one comes first.
	seatAt := -1
	for i := 1; i < len(segments); i++ {
		if _, ok := seatCounts[segments[i]]; ok {
			seatAt = i
			break
		}
	}
	switch {
	case seatAt < 0:
		return BotName{}, fmt.Errorf("bot name %q has no seat token (%s)", name, strings.Join(seatTokens, ", "))
	case seatAt == 1:
		return BotName{}, fmt.Errorf("bot name %q has no game id between %q and the seat token", name, BotNameOwner)
	case seatAt+1 >= len(segments):
		return BotName{}, fmt.Errorf("bot name %q ends at the seat token; it needs a lineage segment such as \"h1\"", name)
	}

	lineage, generation, err := parseLineage(segments[seatAt+1])
	if err != nil {
		return BotName{}, fmt.Errorf("bot name %q: %w", name, err)
	}

	return BotName{
		Owner:      segments[0],
		Game:       strings.Join(segments[1:seatAt], "-"),
		Seats:      segments[seatAt],
		Lineage:    lineage,
		Generation: generation,
		Qualifiers: append([]string(nil), segments[seatAt+2:]...),
	}, nil
}

// Counts reports the exact player counts the seat token declares. The slice is
// fresh, so a caller cannot reach into the package's table.
func (n BotName) Counts() []int {
	counts := seatCounts[n.Seats]
	out := make([]int, len(counts))
	copy(out, counts)
	return out
}

// NextGen is the name the next raceable build in this lineage should take: the
// generation bumped, the old build's knobs dropped. It exists to make "that
// name is taken" an answer rather than a complaint.
func (n BotName) NextGen() BotName {
	next := n
	next.Generation++
	next.Qualifiers = nil
	return next
}

// String rebuilds the name, so a parse round-trips.
func (n BotName) String() string {
	if n.Owner != BotNameOwner {
		return fmt.Sprintf("%s%s-%d", CodenamePrefix, n.Owner, n.Generation)
	}
	segments := []string{n.Owner, n.Game, n.Seats, fmt.Sprintf("%c%d", n.Lineage, n.Generation)}
	return strings.Join(append(segments, n.Qualifiers...), "-")
}

// The codename grammar: 2-7-<codename>-<gen>, heads-up 27td-fl only.
//
// A codename is one lowercase word naming a strategy family; the generation
// counts raceable builds within it. Owner carries the codename, since the
// roster shows no account prefix for these names. Reserved codenames are the
// agents that build bots — a bot is never named after its author.
const (
	CodenamePrefix = "2-7-"
	CodenameGame   = "27td-fl"
	CodenameSeats  = "hu"
)

var reservedCodenames = map[string]bool{"fable": true}

func parseCodename(name string, segments []string) (BotName, error) {
	if len(segments) != 4 {
		return BotName{}, fmt.Errorf("bot name %q must be %s<codename>-<generation>", name, CodenamePrefix)
	}
	codename := segments[2]
	for i := 0; i < len(codename); i++ {
		if codename[i] < 'a' || codename[i] > 'z' {
			return BotName{}, fmt.Errorf("codename %q must be lowercase letters only", codename)
		}
	}
	if reservedCodenames[codename] {
		return BotName{}, fmt.Errorf("codename %q is reserved", codename)
	}
	_, generation, err := parseLineage("h" + segments[3])
	if err != nil {
		return BotName{}, fmt.Errorf("bot name %q: %w", name, err)
	}
	return BotName{
		Owner:      codename,
		Game:       CodenameGame,
		Seats:      CodenameSeats,
		Lineage:    LineageBlueprint,
		Generation: generation,
	}, nil
}

func checkBotNameCharset(name string) error {
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case c == '-':
			if i == 0 || i == len(name)-1 || name[i-1] == '-' {
				return fmt.Errorf("bot name %q has a stray %q", name, "-")
			}
		default:
			return fmt.Errorf("bot name %q contains %q; use lowercase letters, digits and %q only",
				name, string(c), "-")
		}
	}
	return nil
}

func parseLineage(segment string) (byte, int, error) {
	if len(segment) < 2 {
		return 0, 0, fmt.Errorf("%q is not a lineage segment such as \"b2\"", segment)
	}
	lineage := segment[0]
	switch lineage {
	case LineageHeuristic, LineageBlueprint, LineageExperiment:
	default:
		return 0, 0, fmt.Errorf("lineage %q is not one of h (heuristic), b (blueprint) or x (experiment)",
			string(lineage))
	}
	digits := segment[1:]
	if digits[0] == '0' {
		return 0, 0, fmt.Errorf("generation %q must not start with a zero", digits)
	}
	generation, err := strconv.Atoi(digits)
	if err != nil {
		return 0, 0, fmt.Errorf("generation %q is not a number", digits)
	}
	return lineage, generation, nil
}

// seatTokenFor reports the token that declares exactly these counts, if any.
func seatTokenFor(counts []int) string {
	for _, token := range seatTokens {
		if sameCounts(counts, seatCounts[token]) {
			return token
		}
	}
	return ""
}

// sameCounts compares two count sets ignoring order. It copies before sorting,
// because sorting a caller's slice in place would be a silent mutation.
func sameCounts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	x := append([]int(nil), a...)
	y := append([]int(nil), b...)
	sort.Ints(x)
	sort.Ints(y)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

// firstDuplicate reports a repeated count, if any. splitCSV does not dedupe, so
// "--counts 2,2" reaches Validate and deserves its own message rather than a
// confusing seat-token mismatch.
func firstDuplicate(counts []int) (int, bool) {
	seen := make(map[int]bool, len(counts))
	for _, count := range counts {
		if seen[count] {
			return count, true
		}
		seen[count] = true
	}
	return 0, false
}

func joinCounts(counts []int) string {
	parts := make([]string, len(counts))
	for i, count := range counts {
		parts[i] = strconv.Itoa(count)
	}
	return strings.Join(parts, ",")
}
