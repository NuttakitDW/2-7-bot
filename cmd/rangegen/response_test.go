package main

import (
	"github.com/nuttakit/2-7-bot/internal/arena"
	"testing"
)

func TestRiverResponseUsesFirstReplyToOpeningBet(t *testing.T) {
	h := sample()
	h.Hand.Roles = []string{"smallblind", "bigblind"}
	h.Events = h.Events[:4]
	h.Events = append(h.Events,
		arena.HandEvent{Kind: "action", Player: ptr(1), Street: ptr("draw3"), Action: ptr("check")},
		arena.HandEvent{Kind: "action", Player: ptr(0), Street: ptr("draw3"), Action: ptr("bet")},
		arena.HandEvent{Kind: "action", Player: ptr(1), Street: ptr("draw3"), Action: ptr("raise")},
		arena.HandEvent{Kind: "action", Player: ptr(0), Street: ptr("draw3"), Action: ptr("raise")},
		arena.HandEvent{Kind: "action", Player: ptr(1), Street: ptr("draw3"), Action: ptr("call")},
	)
	got, err := riverResponses(h, 1)
	if err != nil || len(got) != 1 || got[0].context != 5 || got[0].response != 2 {
		t.Fatalf("first response: %+v %v", got, err)
	}
}

func TestRiverCheckResponseRequiresHeroToCheckFirst(t *testing.T) {
	h := sample()
	h.Hand.Roles = []string{"bigblind", "smallblind"}
	h.Events = h.Events[:4]
	h.Events = append(h.Events,
		arena.HandEvent{Kind: "action", Player: ptr(0), Street: ptr("draw3"), Action: ptr("check")},
		arena.HandEvent{Kind: "action", Player: ptr(1), Street: ptr("draw3"), Action: ptr("bet")},
	)
	got, err := riverResponses(h, 1)
	if err != nil || len(got) != 1 || got[0].context != 4 || got[0].response != 4 {
		t.Fatalf("check response %+v %v", got, err)
	}
	h.Events = h.Events[:4]
	h.Events = append(h.Events, arena.HandEvent{Kind: "action", Player: ptr(1), Street: ptr("draw3"), Action: ptr("bet")})
	got, err = riverResponses(h, 1)
	if err != nil || len(got) != 0 {
		t.Fatalf("invented previous check: %+v %v", got, err)
	}
}
