package main

import "testing"

func TestParseIDsAcceptsExplicitSpaceAndCommaSeparatedIDs(t *testing.T) {
	got, err := parseIDs([]string{"41,85", "102", "41"})
	if err != nil {
		t.Fatal(err)
	}
	want := []int{41, 85, 102}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("IDs = %v, want %v", got, want)
		}
	}
}

func TestParseIDsRequiresPositiveID(t *testing.T) {
	if _, err := parseIDs([]string{"0"}); err == nil {
		t.Fatal("expected invalid ID error")
	}
}
