package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/nuttakit/2-7-bot/internal/cards"
	"github.com/nuttakit/2-7-bot/internal/cfr"
)

//go:embed index.html
var assets embed.FS

type step struct {
	Kind      string   `json:"kind"`
	Action    string   `json:"action,omitempty"`
	Count     int      `json:"count,omitempty"`
	Discarded []string `json:"discarded,omitempty"`
	Drawn     []string `json:"drawn,omitempty"`
}

type request struct {
	Position string   `json:"position"`
	Cards    []string `json:"cards"`
	History  []step   `json:"history"`
}

type state struct {
	Position string   `json:"position"`
	Actor    string   `json:"actor"`
	Street   string   `json:"street"`
	Phase    string   `json:"phase"`
	Hand     []string `json:"hand"`
	Pot      int32    `json:"pot"`
	Commit   [2]int32 `json:"commit"`
	Legal    []string `json:"legal"`
	Decision any      `json:"decision"`
	Terminal bool     `json:"terminal"`
	Warnings []string `json:"warnings,omitempty"`
}

type compiled struct {
	state state
	lines []json.RawMessage
	hero  int
}

func main() {
	listen := flag.String("listen", "127.0.0.1:8765", "loopback listen address")
	bot := flag.String("bot", "bin/bot-spinel", "Spinel executable")
	flag.Parse()
	if !loopbackListen(*listen) {
		fmt.Fprintln(os.Stderr, "listen address must be loopback")
		os.Exit(2)
	}
	path, err := filepath.Abs(*bot)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	app := &server{bot: path}
	mux := http.NewServeMux()
	mux.HandleFunc("/", app.index)
	mux.HandleFunc("/api/inspect", app.inspect)
	mux.HandleFunc("/api/query", app.query)
	fmt.Printf("Spinel Query: http://%s\n", *listen)
	if err := http.ListenAndServe(*listen, app.secure(mux)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func loopbackListen(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	return err == nil && (strings.EqualFold(host, "localhost") || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback())
}

type server struct{ bot string }

func (s *server) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		ip := net.ParseIP(host)
		if !(strings.EqualFold(host, "localhost") || ip != nil && ip.IsLoopback()) {
			http.Error(w, "localhost only", http.StatusForbidden)
			return
		}
		remote, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil || net.ParseIP(remote) == nil || !net.ParseIP(remote).IsLoopback() {
			http.Error(w, "loopback only", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host != r.Host {
				http.Error(w, "invalid origin", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	b, _ := assets.ReadFile("index.html")
	_, _ = w.Write(b)
}

func readRequest(w http.ResponseWriter, r *http.Request) (request, bool) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return request{}, false
	}
	var v request
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return request{}, false
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		respondError(w, http.StatusBadRequest, "one JSON object required")
		return request{}, false
	}
	return v, true
}

func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func respondError(w http.ResponseWriter, status int, msg string) {
	respond(w, status, map[string]string{"error": msg})
}

func (s *server) inspect(w http.ResponseWriter, r *http.Request) {
	v, ok := readRequest(w, r)
	if !ok {
		return
	}
	c, err := compile(v, false)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respond(w, http.StatusOK, c.state)
}

func (s *server) query(w http.ResponseWriter, r *http.Request) {
	v, ok := readRequest(w, r)
	if !ok {
		return
	}
	c, err := compile(v, true)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if c.state.Terminal || c.state.Actor != c.state.Position {
		respondError(w, http.StatusBadRequest, "query requires your turn in a live hand")
		return
	}
	start := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	var input bytes.Buffer
	for _, line := range c.lines {
		input.Write(line)
		input.WriteByte('\n')
	}
	cmd := exec.CommandContext(ctx, s.bot)
	cmd.Env = append(os.Environ(), "BOT_DEBUG=1")
	cmd.Stdin = &input
	var stdout, stderr cappedBuffer
	stdout.limit = 1 << 20
	stderr.limit = 1 << 20
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if ctx.Err() != nil {
		respondError(w, http.StatusGatewayTimeout, "Spinel timed out")
		return
	}
	if stdout.over || stderr.over {
		respondError(w, http.StatusBadGateway, "Spinel output exceeded limit")
		return
	}
	if err != nil {
		respondError(w, http.StatusBadGateway, fmt.Sprintf("Spinel failed: %v: %s", err, stderr.String()))
		return
	}
	var output []json.RawMessage
	var answer json.RawMessage
	actionCount := 0
	for _, line := range bytes.Split(bytes.TrimSpace(stdout.Bytes()), []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var msg struct {
			Type   string          `json:"t"`
			Action json.RawMessage `json:"action"`
		}
		if err := json.Unmarshal(line, &msg); err != nil {
			respondError(w, http.StatusBadGateway, "Spinel returned malformed JSON")
			return
		}
		output = append(output, json.RawMessage(append([]byte(nil), line...)))
		if msg.Type == "action" {
			answer = msg.Action
			actionCount++
		}
	}
	expectedActions := 0
	for _, line := range c.lines {
		var msg struct {
			Type string `json:"t"`
		}
		_ = json.Unmarshal(line, &msg)
		if msg.Type == "act" {
			expectedActions++
		}
	}
	if len(answer) == 0 || actionCount != expectedActions {
		respondError(w, http.StatusBadGateway, fmt.Sprintf("Spinel returned %d actions for %d decisions", actionCount, expectedActions))
		return
	}
	if err := validateAnswer(c.state, answer); err != nil {
		respondError(w, http.StatusBadGateway, "Spinel returned an illegal action: "+err.Error())
		return
	}
	// The bot reports blueprint fallback counts through its debug channel.
	warnings := append([]string(nil), c.state.Warnings...)
	fallbacks := parseFallbacks(stderr.String())
	if fallbacks > 0 {
		warnings = append(warnings, fmt.Sprintf("Spinel reported %d blueprint fallback(s) across this query transcript.", fallbacks))
	}
	if fallbacks < 0 {
		warnings = append(warnings, "Spinel did not report a fallback count; check diagnostics.")
	}
	respond(w, http.StatusOK, map[string]any{"state": c.state, "action": answer, "stdin": c.lines, "stdout": output, "stderr": stderr.String(), "elapsed_ms": time.Since(start).Milliseconds(), "blueprint_fallbacks": fallbacks, "warnings": warnings})
}

type cappedBuffer struct {
	bytes.Buffer
	limit int
	over  bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.limit {
		b.over = true
		return 0, errors.New("output limit")
	}
	return b.Buffer.Write(p)
}

func add(lines *[]json.RawMessage, v any) { b, _ := json.Marshal(v); *lines = append(*lines, b) }
func ev(lines *[]json.RawMessage, v any) {
	add(lines, map[string]any{"t": "event", "hand_no": 0, "ev": v})
}
func act(lines *[]json.RawMessage, seat int, d any) {
	add(lines, map[string]any{"t": "act", "hand_no": 0, "seat": seat, "decision": d, "deadline_ms": 5000})
}

func compile(v request, includeAct bool) (compiled, error) {
	var out compiled
	if v.Position != "BU" && v.Position != "BB" {
		return out, fmt.Errorf("position must be BU or BB")
	}
	if len(v.History) > 80 {
		return out, fmt.Errorf("history exceeds 80 steps")
	}
	hero := cfr.Btn
	if v.Position == "BB" {
		hero = cfr.BB
	}
	out.hero = hero
	hand, err := parseCards(v.Cards, 5)
	if err != nil {
		return out, fmt.Errorf("initial hand: %w", err)
	}
	if len(hand) != 5 {
		return out, fmt.Errorf("initial hand needs five cards")
	}
	labels := []string{"Predraw", "Draw 1", "Draw 2", "Draw 3"}
	add(&out.lines, map[string]any{"t": "hello", "proto": 1, "game_id": "27td-fl", "stakes": map[string]any{"kind": "blinds", "small_blind": 50, "big_blind": 100, "ante": 0}, "betting": map[string]any{"kind": "fixed-limit", "raise_cap": 4}, "seat_count": 2, "starting_stack": 10000, "timeout_ms": 5000})
	add(&out.lines, map[string]any{"t": "hand-start", "hand_no": 0, "seat": hero})
	ev(&out.lines, map[string]any{"event": "hand-start", "hand_no": 0, "button": 0, "stacks": []int{10000, 10000}})
	ev(&out.lines, map[string]any{"event": "post", "seat": 0, "kind": "small-blind", "amount": 50})
	ev(&out.lines, map[string]any{"event": "post", "seat": 1, "kind": "big-blind", "amount": 100})
	ev(&out.lines, map[string]any{"event": "street-start", "street": 0, "label": "predraw"})
	for p := 0; p < 2; p++ {
		shown := []string{}
		if p == hero {
			shown = hand
		}
		ev(&out.lines, map[string]any{"event": "deal-hole", "seat": p, "cards": shown, "count": 5})
	}
	tree := cfr.BuildTree()
	id := tree.Root
	street := 0
	base := [2]int32{}
	seen := map[string]bool{}
	for _, card := range hand {
		seen[card] = true
	}
	warnings := []string{}
	for i, s := range v.History {
		n := tree.Nodes[id]
		if n.Kind == cfr.KindFold || n.Kind == cfr.KindShowdown {
			return out, fmt.Errorf("step %d follows a terminal hand", i+1)
		}
		if int(n.Street) != street {
			street = int(n.Street)
			base = n.Commit
			ev(&out.lines, map[string]any{"event": "street-start", "street": street, "label": fmt.Sprintf("draw%d", street)})
		}
		p := int(n.Actor)
		if n.Kind == cfr.KindDraw {
			if s.Kind != "draw" {
				return out, fmt.Errorf("step %d: expected draw by %s", i+1, seatName(p))
			}
			if s.Action != "" {
				return out, fmt.Errorf("step %d: draw cannot carry a wager action", i+1)
			}
			if s.Count < 0 || s.Count > 5 {
				return out, fmt.Errorf("step %d: draw count must be 0–5", i+1)
			}
			if p == hero {
				discard, err := parseCards(s.Discarded, 5)
				if err != nil {
					return out, fmt.Errorf("step %d: %w", i+1, err)
				}
				drawn, err := parseCards(s.Drawn, 5)
				if err != nil {
					return out, fmt.Errorf("step %d: %w", i+1, err)
				}
				if len(discard) != s.Count || len(drawn) != s.Count {
					return out, fmt.Errorf("step %d: your draw count must match discarded and replacement cards", i+1)
				}
				for _, card := range discard {
					if !contains(hand, card) {
						return out, fmt.Errorf("step %d: %s is not in your hand", i+1, card)
					}
				}
				remaining := without(hand, discard)
				for _, card := range drawn {
					if contains(remaining, card) {
						return out, fmt.Errorf("step %d: replacement %s duplicates a held card", i+1, card)
					}
					if seen[card] {
						return out, fmt.Errorf("step %d: replacement %s was already seen in this hand", i+1, card)
					}
					seen[card] = true
				}
				act(&out.lines, p, map[string]any{"kind": "draw", "max_discards": 5})
				ev(&out.lines, map[string]any{"event": "draw-result", "seat": p, "discarded": discard, "drawn": drawn, "count": s.Count})
				hand = append(remaining, drawn...)
			} else {
				if len(s.Discarded) > 0 || len(s.Drawn) > 0 {
					return out, fmt.Errorf("step %d: opponent cards are private", i+1)
				}
				ev(&out.lines, map[string]any{"event": "draw-result", "seat": p, "discarded": []string{}, "drawn": []string{}, "count": s.Count})
			}
			id = n.Next[0]
			continue
		}
		if s.Kind != "bet" {
			return out, fmt.Errorf("step %d: expected wager action by %s", i+1, seatName(p))
		}
		if s.Count != 0 || len(s.Discarded) > 0 || len(s.Drawn) > 0 {
			return out, fmt.Errorf("step %d: wager cannot carry draw cards or count", i+1)
		}
		legal, d := betOptions(n, base)
		if !contains(legal, s.Action) {
			return out, fmt.Errorf("step %d: illegal %s action %q; choose %s", i+1, seatName(p), s.Action, strings.Join(legal, ", "))
		}
		if p == hero {
			act(&out.lines, p, d)
		}
		var choice uint8
		var action map[string]any
		switch s.Action {
		case "fold":
			choice = cfr.Fold
			action = map[string]any{"kind": "fold"}
		case "check":
			choice = cfr.Pass
			action = map[string]any{"kind": "check"}
		case "call":
			choice = cfr.Pass
			action = map[string]any{"kind": "call"}
		case "bet", "raise":
			choice = cfr.Aggr
			amount := d[s.Action].(map[string]any)["min_to"]
			action = map[string]any{"kind": s.Action, "to": amount}
		}
		var next int32 = -1
		for j, a := range n.Acts {
			if a == choice {
				next = n.Next[j]
				break
			}
		}
		if next < 0 {
			return out, fmt.Errorf("step %d: action not in tree", i+1)
		}
		commit := n.Commit[p] - base[p]
		if choice == cfr.Pass && n.Facing {
			commit = n.Commit[1-p] - base[p]
		}
		if choice == cfr.Aggr {
			commit = int32(action["to"].(int32))
		}
		ev(&out.lines, map[string]any{"event": "acted", "seat": p, "action": action, "street_commit": commit, "all_in": false})
		id = next
	}
	n := tree.Nodes[id]
	if int(n.Street) != street && n.Kind != cfr.KindFold && n.Kind != cfr.KindShowdown {
		street = int(n.Street)
		base = n.Commit
		ev(&out.lines, map[string]any{"event": "street-start", "street": street, "label": fmt.Sprintf("draw%d", street)})
	}
	out.state = state{Position: v.Position, Actor: seatName(int(n.Actor)), Street: labels[n.Street], Phase: "wager", Hand: hand, Pot: n.Commit[0] + n.Commit[1], Commit: n.Commit, Legal: []string{}, Warnings: warnings}
	if n.Kind == cfr.KindFold || n.Kind == cfr.KindShowdown {
		out.state.Terminal = true
		out.state.Phase = "terminal"
		out.state.Actor = "—"
		return out, nil
	}
	if n.Kind == cfr.KindDraw {
		out.state.Phase = "draw"
		out.state.Legal = []string{"draw 0–5"}
		out.state.Decision = map[string]any{"kind": "draw", "max_discards": 5}
	}
	if n.Kind == cfr.KindBet {
		out.state.Legal, out.state.Decision = betOptions(n, base)
	}
	if includeAct && int(n.Actor) == hero {
		act(&out.lines, hero, out.state.Decision)
		add(&out.lines, map[string]any{"t": "match-end"})
	}
	return out, nil
}

func seatName(p int) string {
	if p == cfr.Btn {
		return "BU"
	}
	return "BB"
}
func contains[T comparable](s []T, v T) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
func without(s, remove []string) []string {
	out := make([]string, 0, len(s))
	for _, v := range s {
		if !contains(remove, v) {
			out = append(out, v)
		}
	}
	return out
}
func parseCards(in []string, max int) ([]string, error) {
	if len(in) > max {
		return nil, fmt.Errorf("too many cards")
	}
	out := make([]string, 0, len(in))
	for _, v := range in {
		c, e := cards.ParseCard(strings.TrimSpace(v))
		if e != nil {
			return nil, e
		}
		normalized := c.String()
		if contains(out, normalized) {
			return nil, fmt.Errorf("duplicate card %s", normalized)
		}
		out = append(out, normalized)
	}
	return out, nil
}

func betOptions(n cfr.Node, base [2]int32) ([]string, map[string]any) {
	p := int(n.Actor)
	d := map[string]any{"kind": "wager", "fold": n.Facing, "check": !n.Facing}
	legal := []string{}
	if n.Facing {
		legal = append(legal, "fold", "call")
		d["call"] = n.Commit[1-p] - n.Commit[p]
	} else {
		legal = append(legal, "check")
	}
	if contains(n.Acts, uint8(cfr.Aggr)) {
		size := int32(cfr.SmallBet)
		if n.Street >= cfr.Draw2 {
			size = cfr.BigBet
		}
		to := max(n.Commit[0], n.Commit[1]) - base[p] + size
		kind := "bet"
		if n.Wagers > 0 {
			kind = "raise"
		}
		legal = append(legal, kind)
		d[kind] = map[string]any{"min_to": to, "max_to": to}
	}
	return legal, d
}

var fallbackPattern = regexp.MustCompile(`match end, (\d+) blueprint fallbacks`)

func parseFallbacks(s string) int {
	m := fallbackPattern.FindStringSubmatch(s)
	if len(m) != 2 {
		return -1
	}
	var n int
	_, _ = fmt.Sscan(m[1], &n)
	return n
}

func validateAnswer(s state, raw json.RawMessage) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	if fields == nil {
		return fmt.Errorf("action must be an object")
	}
	var a struct {
		Kind  string   `json:"kind"`
		To    int32    `json:"to"`
		Cards []string `json:"cards"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return err
	}
	if s.Phase == "draw" {
		if a.Kind != "discard" {
			return fmt.Errorf("expected discard")
		}
		cardsRaw, ok := fields["cards"]
		if !ok || len(bytes.TrimSpace(cardsRaw)) == 0 || bytes.TrimSpace(cardsRaw)[0] != '[' {
			return fmt.Errorf("discard cards must be an array")
		}
		v, err := parseCards(a.Cards, 5)
		if err != nil {
			return err
		}
		for _, card := range v {
			if !contains(s.Hand, card) {
				return fmt.Errorf("%s is not held", card)
			}
		}
		return nil
	}
	if !contains(s.Legal, a.Kind) {
		return fmt.Errorf("%q is not legal", a.Kind)
	}
	if a.Kind == "bet" || a.Kind == "raise" {
		d := s.Decision.(map[string]any)
		r := d[a.Kind].(map[string]any)
		if a.To != r["min_to"].(int32) {
			return fmt.Errorf("wrong wager amount")
		}
	}
	return nil
}
