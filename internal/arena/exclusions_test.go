package arena

import "testing"

func TestLocalExclusionReason(t *testing.T) {
	tests := []struct {
		name    string
		blocked bool
	}{
		{"paul-sauron", true},
		{"Paul-Sauron-301", true},
		{"  PAUL-SAURON experimental  ", true},
		{"paul-gandalf", false},
		{"gandalf", false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reason, blocked := LocalExclusionReason(test.name)
			if blocked != test.blocked {
				t.Fatalf("LocalExclusionReason(%q) blocked = %v, want %v", test.name, blocked, test.blocked)
			}
			if blocked && reason == "" {
				t.Error("blocked bot has no reason")
			}
			if !blocked && reason != "" {
				t.Errorf("allowed bot has reason %q", reason)
			}
		})
	}
}
