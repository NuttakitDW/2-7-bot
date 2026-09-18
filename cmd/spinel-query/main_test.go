package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func example() request { return request{Position: "BU", Cards: []string{"Js", "4h", "7d", "Qs", "3s"}} }

func TestCompilePredrawAndRaiseCap(t *testing.T) {
	v := example()
	c, err := compile(v, true)
	if err != nil {
		t.Fatal(err)
	}
	if c.state.Actor != "BU" || c.state.Pot != 150 || !contains(c.state.Legal, "call") {
		t.Fatalf("root state: %+v", c.state)
	}
	if got := c.state.Decision.(map[string]any)["raise"].(map[string]any)["min_to"]; got != int32(200) {
		t.Fatalf("root raise to %v", got)
	}
	v.History = []step{{Kind: "bet", Action: "call"}}
	c, err = compile(v, false)
	if err != nil {
		t.Fatal(err)
	}
	if c.state.Actor != "BB" || !contains(c.state.Legal, "raise") || contains(c.state.Legal, "bet") {
		t.Fatalf("BB option after limp: %+v", c.state)
	}
	v.History = append(v.History, step{Kind: "bet", Action: "raise"}, step{Kind: "bet", Action: "raise"}, step{Kind: "bet", Action: "raise"})
	c, err = compile(v, false)
	if err != nil {
		t.Fatal(err)
	}
	if c.state.Actor != "BU" || contains(c.state.Legal, "raise") || !contains(c.state.Legal, "call") {
		t.Fatalf("cap: %+v", c.state)
	}
}

func TestCompileDrawsAndRiver(t *testing.T) {
	v := example()
	v.History = []step{
		{Kind: "bet", Action: "raise"}, {Kind: "bet", Action: "call"},
		{Kind: "draw", Count: 3}, {Kind: "draw", Count: 2, Discarded: []string{"Js", "Qs"}, Drawn: []string{"Qh", "Qd"}},
		{Kind: "bet", Action: "check"}, {Kind: "bet", Action: "check"},
		{Kind: "draw", Count: 2}, {Kind: "draw", Count: 2, Discarded: []string{"Qh", "Qd"}, Drawn: []string{"9h", "Ah"}},
		{Kind: "bet", Action: "check"}, {Kind: "bet", Action: "check"},
		{Kind: "draw", Count: 2}, {Kind: "draw", Count: 1, Discarded: []string{"Ah"}, Drawn: []string{"Ts"}},
		{Kind: "bet", Action: "bet"},
	}
	c, err := compile(v, true)
	if err != nil {
		t.Fatal(err)
	}
	if c.state.Street != "Draw 3" || c.state.Actor != "BU" || c.state.Pot != 600 || !contains(c.state.Hand, "Ts") {
		t.Fatalf("river state: %+v", c.state)
	}
	decision := c.state.Decision.(map[string]any)
	if decision["call"] != int32(200) || decision["raise"].(map[string]any)["min_to"] != int32(400) {
		t.Fatalf("river decision: %+v", decision)
	}
	var found bool
	for _, line := range c.lines {
		if bytes.Contains(line, []byte(`"street_commit":200`)) {
			found = true
		}
	}
	if !found {
		t.Fatal("draw3 street commit missing or cumulative")
	}
	if !bytes.Contains(c.lines[len(c.lines)-2], []byte(`"t":"act"`)) || !bytes.Contains(c.lines[len(c.lines)-1], []byte(`"match-end"`)) {
		t.Fatal("final act/match-end framing missing")
	}
	v.History = append(v.History, step{Kind: "bet", Action: "call"})
	v.History = append(v.History, step{Kind: "bet", Action: "check"})
	if _, err := compile(v, false); err == nil {
		t.Fatal("accepted action after terminal hand")
	}
}

func TestRejectMalformedHistoryAndCards(t *testing.T) {
	cases := []request{
		{Position: "BU", Cards: []string{"Js", "Js", "7d", "Qs", "3s"}},
		{Position: "BU", Cards: []string{"Js", "4h", "7d", "Qs", "XX"}},
		{Position: "BU", Cards: []string{"Js", "4h", "7d", "Qs", "3s"}, History: []step{{Kind: "draw", Count: 0}}},
		{Position: "BU", Cards: []string{"Js", "4h", "7d", "Qs", "3s"}, History: []step{{Kind: "bet", Action: "check"}}},
		{Position: "BU", Cards: []string{"Js", "4h", "7d", "Qs", "3s"}, History: []step{{Kind: "bet", Action: "call"}, {Kind: "bet", Action: "check"}, {Kind: "draw", Count: 0}, {Kind: "draw", Count: 1, Discarded: []string{"Ah"}, Drawn: []string{"2c"}}}},
		{Position: "BU", Cards: []string{"Js", "4h", "7d", "Qs", "3s"}, History: []step{{Kind: "bet", Action: "call"}, {Kind: "bet", Action: "check"}, {Kind: "draw", Count: 0}, {Kind: "draw", Count: 1, Discarded: []string{"Js"}, Drawn: []string{"Js"}}}},
	}
	for i, v := range cases {
		if _, err := compile(v, false); err == nil {
			t.Errorf("case %d accepted", i)
		}
	}
}

func TestAnswerValidation(t *testing.T) {
	c, _ := compile(example(), true)
	if err := validateAnswer(c.state, json.RawMessage(`{"kind":"raise","to":200}`)); err != nil {
		t.Fatal(err)
	}
	for _, answer := range []string{`null`, `{"kind":"check"}`, `{"kind":"raise","to":300}`} {
		if err := validateAnswer(c.state, json.RawMessage(answer)); err == nil {
			t.Errorf("accepted %s", answer)
		}
	}
	draw := state{Phase: "draw", Hand: []string{"Js", "4h", "7d", "Qs", "3s"}}
	for _, answer := range []string{`{"kind":"discard"}`, `{"kind":"discard","cards":null}`, `{"kind":"discard","cards":["Ah"]}`} {
		if err := validateAnswer(draw, json.RawMessage(answer)); err == nil {
			t.Errorf("accepted draw %s", answer)
		}
	}
	if err := validateAnswer(draw, json.RawMessage(`{"kind":"discard","cards":[]}`)); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPAndRealBinary(t *testing.T) {
	path, err := filepath.Abs("../../bin/bot-spinel")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Skip("Spinel binary unavailable: " + err.Error())
	}
	s := &server{bot: path}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/inspect", s.inspect)
	mux.HandleFunc("/api/query", s.query)
	h := s.secure(mux)
	payload, _ := json.Marshal(example())
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8765/api/inspect", bytes.NewReader(payload))
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("inspect %d: %s", w.Code, w.Body.String())
	}
	spots := []request{example(), example(), example()}
	spots[1].History = []step{{Kind: "bet", Action: "raise"}, {Kind: "bet", Action: "call"}, {Kind: "draw", Count: 3}}
	spots[2].History = []step{{Kind: "bet", Action: "raise"}, {Kind: "bet", Action: "call"}, {Kind: "draw", Count: 3}, {Kind: "draw", Count: 2, Discarded: []string{"Js", "Qs"}, Drawn: []string{"Qh", "Qd"}}, {Kind: "bet", Action: "check"}, {Kind: "bet", Action: "check"}, {Kind: "draw", Count: 2}, {Kind: "draw", Count: 2, Discarded: []string{"Qh", "Qd"}, Drawn: []string{"9h", "Ah"}}, {Kind: "bet", Action: "check"}, {Kind: "bet", Action: "check"}, {Kind: "draw", Count: 2}, {Kind: "draw", Count: 1, Discarded: []string{"Ah"}, Drawn: []string{"Ts"}}, {Kind: "bet", Action: "bet"}}
	for i, spot := range spots {
		body, _ := json.Marshal(spot)
		req = httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8765/api/query", bytes.NewReader(body))
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set("Content-Type", "application/json")
		w = httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("spot %d query %d: %s", i, w.Code, w.Body.String())
		}
		var result struct {
			Action struct {
				Kind string `json:"kind"`
			} `json:"action"`
			Stdin     []json.RawMessage `json:"stdin"`
			Stdout    []json.RawMessage `json:"stdout"`
			Stderr    string            `json:"stderr"`
			Fallbacks int               `json:"blueprint_fallbacks"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Action.Kind == "" || len(result.Stdin) == 0 || len(result.Stdout) < 2 || !strings.Contains(result.Stderr, "blueprint fallbacks") || result.Fallbacks < 0 {
			t.Fatalf("spot %d incomplete response: %+v", i, result)
		}
	}
	req = httptest.NewRequest(http.MethodPost, "http://evil.example/api/query", bytes.NewReader(payload))
	req.RemoteAddr = "127.0.0.1:12345"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("foreign host status %d", w.Code)
	}
	req = httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8765/api/query", bytes.NewReader(payload))
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Origin", "https://evil.example")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("foreign origin status %d", w.Code)
	}
}
