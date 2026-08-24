package arena

import (
	"strings"
	"testing"
)

func TestParseBotNameAccepts(t *testing.T) {
	tests := []struct {
		name       string
		game       string
		seats      string
		lineage    byte
		generation int
		qualifiers []string
		counts     []int
	}{
		{"nutt-27td-fl-hu-h1", "27td-fl", "hu", 'h', 1, nil, []int{2}},
		{"nutt-27td-fl-hu-h2", "27td-fl", "hu", 'h', 2, nil, []int{2}},
		{"nutt-27td-fl-hu-b1-i050", "27td-fl", "hu", 'b', 1, []string{"i050"}, []int{2}},
		{"nutt-27td-fl-hu-b1-i200", "27td-fl", "hu", 'b', 1, []string{"i200"}, []int{2}},
		{"nutt-27td-fl-6max-b2-1bit", "27td-fl", "6max", 'b', 2, []string{"1bit"}, []int{6}},
		{"nutt-27td-fl-hu-b2-i200-4bit", "27td-fl", "hu", 'b', 2, []string{"i200", "4bit"}, []int{2}},
		{"nutt-27td-fl-all-h1", "27td-fl", "all", 'h', 1, nil, []int{2, 3, 4, 5, 6}},
		{"nutt-27td-fl-hu6-b3-1bit", "27td-fl", "hu6", 'b', 3, []string{"1bit"}, []int{2, 6}},
		{"nutt-badugi-fl-hu-x1", "badugi-fl", "hu", 'x', 1, nil, []int{2}},
		{"nutt-27td-fl-6max-b12-neutral", "27td-fl", "6max", 'b', 12, []string{"neutral"}, []int{6}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := ParseBotName(test.name)
			if err != nil {
				t.Fatalf("rejected: %v", err)
			}
			if parsed.Owner != BotNameOwner {
				t.Errorf("owner = %q, want %q", parsed.Owner, BotNameOwner)
			}
			if parsed.Game != test.game {
				t.Errorf("game = %q, want %q", parsed.Game, test.game)
			}
			if parsed.Seats != test.seats {
				t.Errorf("seats = %q, want %q", parsed.Seats, test.seats)
			}
			if parsed.Lineage != test.lineage {
				t.Errorf("lineage = %q, want %q", string(parsed.Lineage), string(test.lineage))
			}
			if parsed.Generation != test.generation {
				t.Errorf("generation = %d, want %d", parsed.Generation, test.generation)
			}
			if strings.Join(parsed.Qualifiers, ",") != strings.Join(test.qualifiers, ",") {
				t.Errorf("qualifiers = %v, want %v", parsed.Qualifiers, test.qualifiers)
			}
			if !sameCounts(parsed.Counts(), test.counts) {
				t.Errorf("counts = %v, want %v", parsed.Counts(), test.counts)
			}
			if parsed.String() != test.name {
				t.Errorf("String() = %q, does not round-trip", parsed.String())
			}
		})
	}
}

func TestParseBotNameRejects(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{"empty", "", "name is required"},
		{"retired m1", "nutt-27td-m1", "retired"},
		{"retired fl", "nutt-27td-fl", "retired"},
		{"another owner", "swit-27td-ring1", "must start with"},
		{"uppercase", "Nutt-27td-fl-hu-h1", "lowercase"},
		{"dotted version", "nutt-27td-fl-hu-1.0", "contains \".\""},
		{"slash", "rand/30/30/40", "contains \"/\""},
		{"doubled hyphen", "nutt--27td-fl-hu-h1", "stray"},
		{"trailing hyphen", "nutt-27td-fl-hu-h1-", "stray"},
		{"too long", "nutt-27td-fl-6max-b2-i200-4bit-exploit-lite", "over the 40-character limit"},
		{"no seat token", "nutt-27td-fl-h1", "no seat token"},
		{"no game", "nutt-hu-h1", "no game id"},
		{"nothing after seats", "nutt-27td-fl-hu", "needs a lineage segment"},
		{"unknown lineage", "nutt-27td-fl-hu-z1", "not one of h"},
		{"lineage without generation", "nutt-27td-fl-hu-b", "not a lineage segment"},
		{"generation zero", "nutt-27td-fl-hu-h0", "must not start with a zero"},
		{"padded generation", "nutt-27td-fl-hu-h01", "must not start with a zero"},
		{"generation not a number", "nutt-27td-fl-hu-hx", "not a number"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseBotName(test.input)
			if err == nil {
				t.Fatalf("accepted %q", test.input)
			}
			if !strings.Contains(err.Error(), test.wantErr) {
				t.Errorf("error %q does not mention %q", err, test.wantErr)
			}
		})
	}
}

// A qualifier may spell a seat token; the real one comes first and wins.
func TestParseBotNameSeatTokenIsTheFirstMatch(t *testing.T) {
	parsed, err := ParseBotName("nutt-27td-fl-hu-b1-all")
	if err != nil {
		t.Fatalf("rejected: %v", err)
	}
	if parsed.Seats != "hu" {
		t.Errorf("seats = %q, want %q", parsed.Seats, "hu")
	}
	if strings.Join(parsed.Qualifiers, ",") != "all" {
		t.Errorf("qualifiers = %v, want [all]", parsed.Qualifiers)
	}
}

func TestBotNameCountsIsACopy(t *testing.T) {
	parsed, err := ParseBotName("nutt-27td-fl-all-h1")
	if err != nil {
		t.Fatalf("rejected: %v", err)
	}
	counts := parsed.Counts()
	counts[0] = 99
	if parsed.Counts()[0] != 2 {
		t.Error("Counts() handed out the package's own slice")
	}
}

func TestSeatTokenFor(t *testing.T) {
	tests := []struct {
		counts []int
		want   string
	}{
		{[]int{2}, "hu"},
		{[]int{6}, "6max"},
		{[]int{6, 2}, "hu6"},
		{[]int{6, 5, 4, 3, 2}, "all"},
		{[]int{3}, ""},
		{[]int{2, 3}, ""},
	}
	for _, test := range tests {
		if got := seatTokenFor(test.counts); got != test.want {
			t.Errorf("seatTokenFor(%v) = %q, want %q", test.counts, got, test.want)
		}
	}
}
