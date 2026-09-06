package main

import (
	"bufio"
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

// A heads-up 27td-fl session from seat 0's point of view, in the shape the
// arena actually sends: hello, the handshake, one hand played to showdown,
// then match-end. The hole cards are the nut hand 7-5-4-3-2, so every
// decision below has one obviously right answer and the assertions can be
// exact rather than merely "legal".
//
// It also carries the three things the framing contract says must not break
// a bot: a blank line, an unknown message type, and an unknown event type.
const session = `
{"t":"hello","proto":1,"game_id":"27td-fl","stakes":{"kind":"blinds","small_blind":50,"big_blind":100,"ante":0},"betting":{"kind":"fixed-limit","raise_cap":4},"seat_count":2,"starting_stack":10000,"timeout_ms":1000}
{"t":"joined","name":"candidate"}
{"t":"hand-start","hand_no":0,"seat":0}
{"t":"event","hand_no":0,"ev":{"event":"hand-start","hand_no":0,"button":0,"stacks":[10000,10000]}}
{"t":"event","hand_no":0,"ev":{"event":"post","seat":0,"kind":"small-blind","amount":50,"all_in":false}}
{"t":"event","hand_no":0,"ev":{"event":"post","seat":1,"kind":"big-blind","amount":100,"all_in":false}}
{"t":"event","hand_no":0,"ev":{"event":"street-start","street":0,"label":"predraw"}}
{"t":"event","hand_no":0,"ev":{"event":"deal-hole","seat":1,"cards":[],"count":5}}
{"t":"event","hand_no":0,"ev":{"event":"deal-hole","seat":0,"cards":["7c","5d","4h","3s","2c"],"count":5}}
{"t":"act","hand_no":0,"seat":0,"decision":{"kind":"wager","fold":true,"check":false,"call":50,"raise":{"min_to":200,"max_to":200}},"deadline_ms":1000}
{"t":"event","hand_no":0,"ev":{"event":"acted","seat":0,"action":{"kind":"raise","to":200},"street_commit":200,"all_in":false}}
{"t":"event","hand_no":0,"ev":{"event":"acted","seat":1,"action":{"kind":"call"},"street_commit":200,"all_in":false}}
{"t":"event","hand_no":0,"ev":{"event":"street-start","street":1,"label":"draw1"}}
{"t":"event","hand_no":0,"ev":{"event":"draw-result","seat":1,"discarded":[],"drawn":[],"count":2}}
{"t":"act","hand_no":0,"seat":0,"decision":{"kind":"draw","max_discards":5},"deadline_ms":1000}
{"t":"event","hand_no":0,"ev":{"event":"draw-result","seat":0,"discarded":[],"drawn":[],"count":0}}

{"t":"weather-report","outlook":{"cloudy":true}}
{"t":"event","hand_no":0,"ev":{"event":"rabbit-hunt","cards":{"burned":3}}}
{"t":"act","hand_no":0,"seat":0,"decision":{"kind":"wager","fold":false,"check":true,"bet":{"min_to":100,"max_to":100}},"deadline_ms":1000}
{"t":"event","hand_no":0,"ev":{"event":"acted","seat":0,"action":{"kind":"bet","to":100},"street_commit":100,"all_in":false}}
{"t":"event","hand_no":0,"ev":{"event":"acted","seat":1,"action":{"kind":"fold"},"street_commit":0,"all_in":false}}
{"t":"event","hand_no":0,"ev":{"event":"hand-end","nets":[200,-200]}}
{"t":"hand-end","hand_no":0,"nets":[200,-200]}
{"t":"match-end"}
{"t":"act","hand_no":1,"seat":0,"decision":{"kind":"wager","fold":true,"check":false,"call":50},"deadline_ms":1000}
`

func TestRunPlaysASession(t *testing.T) {
	var replies bytes.Buffer
	if err := run(strings.NewReader(session), &replies, io.Discard); err != nil {
		t.Fatalf("run: %v", err)
	}

	want := []string{
		`{"t":"join"}`,
		// The nuts open-raise rather than limping.
		`{"t":"action","action":{"kind":"raise","to":200}}`,
		// And stand pat, as an empty list rather than an absent field.
		`{"t":"action","action":{"kind":"discard","cards":[]}}`,
		// And bet.
		`{"t":"action","action":{"kind":"bet","to":100}}`,
	}
	got := strings.Split(strings.TrimSpace(replies.String()), "\n")
	if len(got) != len(want) {
		t.Fatalf("got %d replies, want %d:\n%s", len(got), len(want), replies.String())
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("reply %d = %s, want %s", i, got[i], want[i])
		}
	}
}

// match-end means no further messages will be sent; anything after it is not
// ours to answer. The session above ends with a stray act to prove we stop.
func TestRunStopsAtMatchEnd(t *testing.T) {
	var replies bytes.Buffer
	if err := run(strings.NewReader(session), &replies, io.Discard); err != nil {
		t.Fatalf("run: %v", err)
	}
	if count := strings.Count(replies.String(), `"t":"action"`); count != 3 {
		t.Errorf("sent %d actions, want 3 — the act after match-end was answered", count)
	}
}

// A closed pipe is the other way a match ends: the arena went away, and there
// is nothing to complain about.
func TestRunExitsCleanlyOnEOF(t *testing.T) {
	var replies bytes.Buffer
	input := `{"t":"hello","proto":1,"game_id":"27td-fl","seat_count":2}`
	if err := run(strings.NewReader(input), &replies, io.Discard); err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.TrimSpace(replies.String()) != `{"t":"join"}` {
		t.Errorf("replies = %q", replies.String())
	}
}

// A line we cannot parse is the arena's problem. Abandoning the match over it
// would forfeit every remaining hand, so the bot logs and keeps reading.
func TestRunSurvivesAnUnparseableLine(t *testing.T) {
	var replies, debug bytes.Buffer
	input := "{not json at all\n" +
		`{"t":"hello","proto":1,"game_id":"27td-fl","seat_count":2}` + "\n"
	if err := run(strings.NewReader(input), &replies, &debug); err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.TrimSpace(replies.String()) != `{"t":"join"}` {
		t.Errorf("replies = %q", replies.String())
	}
	if !strings.Contains(debug.String(), "undecodable") {
		t.Errorf("nothing logged about the bad line: %q", debug.String())
	}
}

// failingWriter stands in for a pipe the arena has closed.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

// If the reply cannot be written the match is over from our side, and saying
// so beats looping on a dead pipe until the arena times us out.
func TestRunReportsAWriteFailure(t *testing.T) {
	input := `{"t":"hello","proto":1,"game_id":"27td-fl","seat_count":2}`
	err := run(strings.NewReader(input), failingWriter{}, io.Discard)
	if err == nil {
		t.Fatal("run succeeded against a closed pipe")
	}
	if !strings.Contains(err.Error(), "join") {
		t.Errorf("error = %q, want it to name the message that failed", err)
	}
}

// The arena blocks on every reply, so a buffered one is an unanswered one and
// would be scored as a timeout fault. Driving run through a live pipe is the
// only way to prove the flush actually happens per message rather than at
// exit.
func TestRepliesAreFlushedImmediately(t *testing.T) {
	requests, toBot := io.Pipe()
	fromBot, replies := io.Pipe()

	done := make(chan error, 1)
	go func() { done <- run(requests, replies, io.Discard) }()

	reader := bufio.NewReader(fromBot)
	readLine := func() string {
		t.Helper()
		type result struct {
			line string
			err  error
		}
		got := make(chan result, 1)
		go func() {
			line, err := reader.ReadString('\n')
			got <- result{line, err}
		}()
		select {
		case r := <-got:
			if r.err != nil {
				t.Fatalf("read reply: %v", r.err)
			}
			return strings.TrimSpace(r.line)
		case <-time.After(5 * time.Second):
			t.Fatal("no reply within 5s — the bot buffered instead of flushing")
			return ""
		}
	}

	send := func(line string) {
		t.Helper()
		if _, err := io.WriteString(toBot, line+"\n"); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	send(`{"t":"hello","proto":1,"game_id":"27td-fl","seat_count":2,"starting_stack":10000}`)
	if got := readLine(); got != `{"t":"join"}` {
		t.Fatalf("join = %q", got)
	}

	send(`{"t":"hand-start","hand_no":0,"seat":0}`)
	send(`{"t":"event","hand_no":0,"ev":{"event":"deal-hole","seat":0,"cards":["7c","5d","4h","3s","2c"],"count":5}}`)
	send(`{"t":"act","hand_no":0,"seat":0,"decision":{"kind":"draw","max_discards":5},"deadline_ms":1000}`)
	if got := readLine(); got != `{"t":"action","action":{"kind":"discard","cards":[]}}` {
		t.Fatalf("draw reply = %q", got)
	}

	send(`{"t":"match-end"}`)
	toBot.Close()
	if err := <-done; err != nil {
		t.Fatalf("run: %v", err)
	}
	replies.Close()
}
