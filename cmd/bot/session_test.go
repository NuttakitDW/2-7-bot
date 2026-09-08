package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/cfr"
	"github.com/nuttakit/2-7-bot/internal/wire"
)

// Drive both seats through legal public events. A learned strategy can limp
// or check a strong hand, so the transcript must follow its actual reply.
func TestRunPlaysASession(t *testing.T) {
	for hero := 0; hero < 2; hero++ {
		t.Run(fmt.Sprint(hero), func(t *testing.T) { playProtocolSession(t, hero) })
	}
}

func playProtocolSession(t *testing.T, hero int) {
	t.Helper()
	requests, toBot := io.Pipe()
	fromBot, replies := io.Pipe()
	t.Cleanup(func() {
		_ = requests.Close()
		_ = toBot.Close()
		_ = fromBot.Close()
		_ = replies.Close()
	})
	done := make(chan error, 1)
	go func() {
		done <- run(requests, replies, io.Discard)
		_ = requests.Close()
		_ = replies.Close()
	}()
	lines := make(chan []byte, 1)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(fromBot)
		for scanner.Scan() {
			lines <- append([]byte(nil), scanner.Bytes()...)
		}
	}()
	send := func(v any) {
		t.Helper()
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = toBot.Write(append(data, '\n')); err != nil {
			t.Fatal(err)
		}
	}
	read := func() wire.BotMsg {
		t.Helper()
		select {
		case raw, ok := <-lines:
			if !ok {
				t.Fatal("bot closed before replying")
			}
			var m wire.BotMsg
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Fatal(err)
			}
			return m
		case <-time.After(5 * time.Second):
			t.Fatal("bot did not flush its reply")
			return wire.BotMsg{}
		}
	}
	event := func(e map[string]any) { send(map[string]any{"t": "event", "hand_no": 0, "ev": e}) }
	send(map[string]any{"t": "hello", "proto": 1, "game_id": "27td-fl", "seat_count": 2, "starting_stack": 10000,
		"stakes":  map[string]any{"kind": "blinds", "small_blind": 50, "big_blind": 100, "ante": 0},
		"betting": map[string]any{"kind": "fixed-limit", "raise_cap": 4}, "timeout_ms": 1000})
	if read().Type != wire.MsgJoin {
		t.Fatal("missing join")
	}
	send(map[string]any{"t": "hand-start", "hand_no": 0, "seat": hero})
	event(map[string]any{"event": "hand-start", "hand_no": 0, "button": 0, "stacks": []int{10000, 10000}})
	for p, amount := range []int{50, 100} {
		event(map[string]any{"event": "post", "seat": p, "kind": []string{"small-blind", "big-blind"}[p], "amount": amount})
	}
	event(map[string]any{"event": "street-start", "street": 0, "label": "predraw"})
	hand := cards.MustParse("7c", "5d", "4h", "3s", "2c")
	known := cards.NewSet(hand)
	for _, p := range []int{1, 0} {
		visible := []cards.Card{}
		if p == hero {
			visible = hand
		}
		event(map[string]any{"event": "deal-hole", "seat": p, "cards": visible, "count": 5})
	}
	tree := cfr.BuildTree()
	node, street := tree.Root, 0
	var base [2]int32
	decisions := 0
	for steps := 0; ; steps++ {
		if steps > 100 {
			t.Fatal("session did not terminate")
		}
		n := &tree.Nodes[node]
		if n.Kind == cfr.KindFold || n.Kind == cfr.KindShowdown {
			break
		}
		if int(n.Street) != street {
			street = int(n.Street)
			base = n.Commit
			event(map[string]any{"event": "street-start", "street": street, "label": fmt.Sprintf("draw%d", street)})
		}
		p := int(n.Actor)
		d := wire.Decision{Kind: wire.DecisionDraw, MaxDiscards: 5}
		if n.Kind == cfr.KindBet {
			d = wire.Decision{Kind: wire.DecisionWager, Fold: n.Facing, Check: !n.Facing}
			if n.Facing {
				amount := uint64(n.Commit[1-p] - n.Commit[p])
				d.Call = &amount
			}
			if n.Acts[len(n.Acts)-1] == cfr.Aggr {
				size := int32(cfr.SmallBet)
				if street >= cfr.Draw2 {
					size = cfr.BigBet
				}
				to := uint64(max(n.Commit[0], n.Commit[1]) - base[p] + size)
				r := &wire.Range{MinTo: to, MaxTo: to}
				if n.Facing {
					d.Raise = r
				} else {
					d.Bet = r
				}
			}
		}
		action := wire.Discard(nil)
		if n.Kind == cfr.KindBet {
			action = wire.Check()
			if n.Facing {
				action = wire.Call()
			}
		}
		if p == hero {
			send(map[string]any{"t": "act", "hand_no": 0, "seat": p, "decision": d, "deadline_ms": 1000})
			m := read()
			if m.Type != wire.MsgAction || m.Action == nil {
				t.Fatal("missing action")
			}
			action = *m.Action
			decisions++
			before, _ := json.Marshal(action)
			legal, _ := json.Marshal(wire.Legalize(d, action, hand))
			if string(before) != string(legal) {
				t.Fatalf("illegal reply %s; expected legal form %s", before, legal)
			}
		}
		if n.Kind == cfr.KindDraw {
			discarded, drawn := action.Cards, []cards.Card{}
			if p == hero {
				deck := known.Complement().Append(nil)
				drawn = append(drawn, deck[:len(discarded)]...)
				known |= cards.NewSet(drawn)
				hand = cards.With(cards.Without(hand, discarded), drawn)
			}
			event(map[string]any{"event": "draw-result", "seat": p, "discarded": discarded, "drawn": drawn, "count": len(discarded)})
			node = n.Next[0]
			continue
		}
		act, commit := cfr.Pass, n.Commit[p]-base[p]
		switch action.Kind {
		case wire.ActionFold:
			act = cfr.Fold
		case wire.ActionBet, wire.ActionRaise:
			act = cfr.Aggr
			commit = int32(action.To)
		case wire.ActionCall:
			commit = n.Commit[1-p] - base[p]
		}
		event(map[string]any{"event": "acted", "seat": p, "action": action, "street_commit": commit, "all_in": false})
		found := false
		for i, a := range n.Acts {
			if int(a) == act {
				node = n.Next[i]
				found = true
				break
			}
		}
		if !found {
			t.Fatal("reply not in legal public tree")
		}
	}
	if decisions == 0 {
		t.Fatal("session exercised no decisions")
	}
	send(map[string]any{"t": "match-end"})
	_ = toBot.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bot did not exit at match-end")
	}
	if _, ok := <-lines; ok {
		t.Fatal("unexpected extra reply")
	}
}
