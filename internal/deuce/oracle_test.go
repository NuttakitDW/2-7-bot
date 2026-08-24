package deuce

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/nuttakit/2-7-bot/internal/cards"
)

// The engine publishes its own verdict on every hand it shows down. Nothing
// mucks in this arena, so a showdown-show event carries both the five cards
// and the HandValue the engine computed for them — and because the 2-7
// encoding is frozen (eval/mod.rs:9), that value is directly comparable
// against ours. That makes any match log a correctness oracle for free, and
// it is the strongest evidence available that this package agrees with the
// engine rather than merely with itself.

// showdown is the shape of a showdown-show event, whether it arrives on the
// wire inside an `event` message or in a `--log` file inside a `{"hand":N}`
// wrapper.
type showdown struct {
	Cards []string `json:"cards"`
	Hi    *uint64  `json:"hi"`
	Event string   `json:"event"`
}

// Captured from real matches on 2026-08-22 at ENGINE_SHA
// 80c7eeb758b05fd957063330747c4f234f77a0f8:
//
//	poker-arena run --game 27td-fl --hands 300000 --seed 11 \
//	  --bot a@builtin:random --bot b@builtin:random --log …
//
// Two random bots show down anything, so this covers every hand class the
// game can produce rather than only the hands a real strategy reaches — every
// rung of the low ladder, and each of the paired, straight and flush classes
// that 2-7 pushes to the bottom. The `hi` values are the engine's own.
const capturedShowdowns = `
{"event":"showdown-show","cards":["3c","2h","4h","7s","6c"],"hi":16432623}
{"event":"showdown-show","cards":["3c","8c","5s","2c","4c"],"hi":16371183}
{"event":"showdown-show","cards":["3s","4c","7d","6h","8s"],"hi":16362462}
{"event":"showdown-show","cards":["4s","3d","2h","9d","8s"],"hi":16293359}
{"event":"showdown-show","cards":["7s","Th","2s","9s","5c"],"hi":16222927}
{"event":"showdown-show","cards":["3h","4s","Jh","2c","7s"],"hi":16166383}
{"event":"showdown-show","cards":["2c","4s","Qd","Jc","7s"],"hi":16083679}
{"event":"showdown-show","cards":["7c","4h","Kc","Td","8d"],"hi":16021933}
{"event":"showdown-show","cards":["8s","Ac","Qc","5s","9h"],"hi":15947932}
{"event":"showdown-show","cards":["5d","2h","9s","Ah","9c"],"hi":15219967}
{"event":"showdown-show","cards":["6h","6c","5s","8h","8d"],"hi":14269695}
{"event":"showdown-show","cards":["Th","Qc","Td","8h","Ts"],"hi":13064703}
{"event":"showdown-show","cards":["4d","5d","6d","3c","2c"],"hi":12320767}
{"event":"showdown-show","cards":["9h","6c","7d","8d","Ts"],"hi":12058623}
{"event":"showdown-show","cards":["7h","8h","Ah","6h","Qh"],"hi":10705323}
{"event":"showdown-show","cards":["9s","9c","8d","8c","8s"],"hi":10063871}
{"event":"showdown-show","cards":["9d","5h","9c","9s","9h"],"hi":8966143}
{"event":"showdown-show","cards":["Ts","9s","Js","Qs","8s"],"hi":7733247}
`

func TestEvalMatchesCapturedShowdowns(t *testing.T) {
	checked := 0
	for _, line := range strings.Split(strings.TrimSpace(capturedShowdowns), "\n") {
		checked += checkShowdownLine(t, []byte(line))
	}
	if checked == 0 {
		t.Fatal("no showdowns in the captured fixture")
	}
}

// TestEvalMatchesLoggedShowdowns replays an entire match log. Point
// BOT_SPAR_LOG at a file written by `poker-arena run --log` to check every
// showdown in it; without the variable there is nothing to read and the test
// is skipped. This is the same assertion as above over a far larger sample,
// kept out of the repo so a fresh spar can be replayed at any time without
// committing a large fixture.
func TestEvalMatchesLoggedShowdowns(t *testing.T) {
	path := os.Getenv("BOT_SPAR_LOG")
	if path == "" {
		t.Skip("set BOT_SPAR_LOG to a poker-arena --log file to replay it")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	checked := 0
	for scanner.Scan() {
		checked += checkShowdownLine(t, scanner.Bytes())
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if checked == 0 {
		t.Fatalf("%s contained no showdowns", path)
	}
	t.Logf("checked %d showdowns from %s", checked, path)
}

// checkShowdownLine asserts one log or wire line and reports how many
// showdowns it contained (0 or 1). Lines that are not showdowns are skipped:
// a log file interleaves every other event, plus its own header and summary.
func checkShowdownLine(t *testing.T, line []byte) int {
	t.Helper()
	var wrapper struct {
		Ev showdown `json:"ev"`
		showdown
	}
	if err := json.Unmarshal(line, &wrapper); err != nil {
		return 0
	}
	shown := wrapper.Ev
	if shown.Event == "" {
		shown = wrapper.showdown
	}
	if shown.Event != "showdown-show" || shown.Hi == nil {
		return 0
	}
	hand, err := cards.Parse(shown.Cards)
	if err != nil {
		t.Errorf("parse %v: %v", shown.Cards, err)
		return 0
	}
	if len(hand) != HandSize {
		return 0 // another game's showdown; not ours to judge
	}
	if got := uint64(Eval(hand)); got != *shown.Hi {
		t.Errorf("Eval(%v) = %d, engine says %d", shown.Cards, got, *shown.Hi)
	}
	return 1
}
