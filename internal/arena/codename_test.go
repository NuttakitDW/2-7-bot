package arena

import (
	"strconv"
	"strings"
	"testing"
)

func TestParseCodenameAccepts(t *testing.T) {
	tests := []struct {
		name       string
		codename   string
		seats      string
		generation int
		counts     []int
	}{
		{"27-cobalt-1", "cobalt", "hu", 1, []int{2}},
		{"27-lapis-12", "lapis", "hu", 12, []int{2}},
		{"27-spinel-6max-1", "spinel", "6max", 1, []int{6}},
		{"27-spinel-hu6-2", "spinel", "hu6", 2, []int{2, 6}},
		{"27-spinel-all-3", "spinel", "all", 3, []int{2, 3, 4, 5, 6}},
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
			if parsed.Game != CodenameGame || parsed.Seats != test.seats {
				t.Errorf("game/seats = %q/%q, want %q/%q", parsed.Game, parsed.Seats, CodenameGame, test.seats)
			}
			if parsed.Generation != test.generation {
				t.Errorf("generation = %d, want %d", parsed.Generation, test.generation)
			}
			if !sameCounts(parsed.Counts(), test.counts) {
				t.Errorf("counts = %v, want %v", parsed.Counts(), test.counts)
			}
			if parsed.String() != test.name {
				t.Errorf("String() = %q, does not round-trip", parsed.String())
			}
			wantNext := "27-" + test.codename + "-"
			if test.seats != CodenameSeats {
				wantNext += test.seats + "-"
			}
			wantNext += strconv.Itoa(test.generation + 1)
			if next := parsed.NextGen().String(); next != wantNext {
				t.Errorf("NextGen() = %q", next)
			}
		})
	}
}

func TestParseCodenameRejects(t *testing.T) {
	for _, name := range []string{
		"27-lapis",        // no generation
		"27-lapis-0",      // generation starts at one
		"27-lapis-01",     // leading zero
		"27-lapis-1-x",    // no qualifiers in this grammar
		"27-lapis1-1",     // codename is letters only
		"27-la-pis-1",     // codename is one segment
		"27-1",            // no codename
		"27-lapis-hu-1",   // heads-up is the short grammar's default
		"27-lapis-5max-1", // unknown seat token
		"27-fable-1",      // reserved: the agent that builds a bot never names it after itself
		"27-nutt-1",       // reserved to keep the two grammars unambiguous
	} {
		if _, err := ParseBotName(name); err == nil {
			t.Errorf("%q: accepted, want an error", name)
		}
	}
}

func TestParseCodenameRejectsLegacyPrefixWithMigration(t *testing.T) {
	_, err := ParseBotName("2-7-spinel-1")
	if err == nil {
		t.Fatal("accepted the retired 2-7 prefix")
	}
	if got := err.Error(); !strings.Contains(got, "retired") || !strings.Contains(got, "27-spinel-1") {
		t.Errorf("error %q does not provide the 27 migration", got)
	}
}
