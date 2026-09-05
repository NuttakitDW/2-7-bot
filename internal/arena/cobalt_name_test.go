package arena

import "testing"

func TestCobaltNames(t *testing.T) {
	n, err := ParseBotName("2-7-cobalt-1")
	if err != nil {
		t.Fatal(err)
	}
	if n.String() != "2-7-cobalt-1" || n.NextGen().String() != "2-7-cobalt-2" || n.Game != "27td-fl" || !sameCounts(n.Counts(), []int{2}) {
		t.Fatalf("incorrect cobalt name: %+v", n)
	}
	for _, name := range []string{"2-7-cobalt-0", "2-7-cobalt-01", "2-7-cobalt-1-extra", "2-7-cobalt-abc", "2-7-astra-1"} {
		if _, err := ParseBotName(name); err == nil {
			t.Errorf("accepted %q", name)
		}
	}
	for _, tc := range []struct {
		games  []string
		counts []int
		valid  bool
	}{
		{[]string{"27td-fl"}, []int{2}, true},
		{[]string{"27td-fl"}, []int{6}, false},
		{[]string{"badugi-fl"}, []int{2}, false},
	} {
		r := UploadRequest{Name: n.String(), Games: tc.games, PlayerCounts: tc.counts, Size: 100}
		if err := r.Validate(); (err == nil) != tc.valid {
			t.Errorf("validation: %+v: %v", tc, err)
		}
	}
}
