package main

import (
	"github.com/nuttakit/2-7-bot/internal/arena"
	"github.com/nuttakit/2-7-bot/internal/cfr"
	"testing"
)

func TestPolicySamplesRespectRolesAndObservationTime(t *testing.T) {
	h := arena.HandDetail{}
	h.Hand.Roles = []string{"bigblind", "smallblind"}
	h.Events = []arena.HandEvent{
		{Kind: "initial-cards", Player: ptr(1), Cards: ptr("2c3d4h7sKc")},
		{Kind: "action", Player: ptr(1), Street: ptr("predraw"), Action: ptr("call")},
		{Kind: "action", Player: ptr(0), Street: ptr("predraw"), Action: ptr("check")},
		{Kind: "replacement", Player: ptr(0), Street: ptr("draw1"), Cards: ptr("AcAd"), Detail: ptr("KcKd")},
		{Kind: "replacement", Player: ptr(1), Street: ptr("draw1"), Cards: ptr("5s"), Detail: ptr("Kc")},
	}
	got, err := policySamples(h, 1, cfr.BuildTree())
	if err != nil || len(got) != 2 {
		t.Fatalf("samples=%+v err=%v", got, err)
	}
	if got[0].X[0] != 0 || got[0].Y != cfr.Pass || got[1].X[6] != 2 || got[1].X[5] != -1 || got[1].X[17] != 13 || got[1].Y != 1 {
		t.Fatalf("wrong feature timing/role: %+v", got)
	}
	if got[0].Keep != nil || got[1].Keep == nil || *got[1].Keep != 15 {
		t.Fatalf("wrong pre-draw keep masks: %+v", got)
	}
	if got[0].X[36] != 0 || got[1].X[36] != 1 || got[1].X[35] != 1 {
		t.Fatalf("history includes current action or omits prior actions: %+v", got)
	}
}
