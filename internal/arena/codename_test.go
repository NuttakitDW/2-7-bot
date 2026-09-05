package arena

import (
	"strconv"
	"testing"
)

func TestParseCodenameAccepts(t *testing.T) {
	tests := []struct {
		name       string
		codename   string
		generation int
	}{
		{"2-7-cobalt-1", "cobalt", 1},
		{"2-7-lapis-1", "lapis", 1},
		{"2-7-lapis-12", "lapis", 12},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := ParseBotName(test.name)
			if err != nil {
				t.Fatalf("rejected: %v", err)
			}
			if parsed.Owner != test.codename {
				t.Errorf("owner = %q, want codename %q", parsed.Owner, test.codename)
			}
			if parsed.Game != CodenameGame || parsed.Seats != CodenameSeats {
				t.Errorf("game/seats = %q/%q, want %q/%q", parsed.Game, parsed.Seats, CodenameGame, CodenameSeats)
			}
			if parsed.Generation != test.generation {
				t.Errorf("generation = %d, want %d", parsed.Generation, test.generation)
			}
			if !sameCounts(parsed.Counts(), []int{2}) {
				t.Errorf("counts = %v, want [2]", parsed.Counts())
			}
			if parsed.String() != test.name {
				t.Errorf("String() = %q, does not round-trip", parsed.String())
			}
			if next := parsed.NextGen().String(); next != "2-7-"+test.codename+"-"+strconv.Itoa(test.generation+1) {
				t.Errorf("NextGen() = %q", next)
			}
		})
	}
}

func TestParseCodenameRejects(t *testing.T) {
	for _, name := range []string{
		"2-7-lapis",     // no generation
		"2-7-lapis-0",   // generation starts at one
		"2-7-lapis-01",  // leading zero
		"2-7-lapis-1-x", // no qualifiers in this grammar
		"2-7-lapis1-1",  // codename is letters only
		"2-7-la-pis-1",  // codename is one segment
		"2-7-1",         // no codename
		"2-7-fable-1",   // reserved: the agent that builds a bot never names it after itself
	} {
		if _, err := ParseBotName(name); err == nil {
			t.Errorf("%q: accepted, want an error", name)
		}
	}
}
