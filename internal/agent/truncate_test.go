package agent

import "testing"

// TestTruncateRunesEngine verifies the engine-local rune truncator (separate
// budget from act.go's helper) behaves identically for the common cases.
func TestTruncateRunesEngine(t *testing.T) {
	if got := truncateRunes("abcde", 3); got != "abc…" {
		t.Fatalf("truncateRunes short = %q; want %q", got, "abc…")
	}
	if got := truncateRunes("abc", 10); got != "abc" {
		t.Fatalf("truncateRunes under-budget = %q; want abc", got)
	}
	if got := truncateRunes("", 10); got != "" {
		t.Fatalf("truncateRunes empty = %q; want empty", got)
	}
}

// TestIterOutcomeString preserves the control-flow outcome labels.
func TestIterOutcomeString(t *testing.T) {
	cases := []struct {
		o    iterOutcome
		want string
	}{
		{iterOutcomeNext, "next"},
		{iterOutcomeContinue, "continue"},
		{iterOutcomeBreak, "break"},
		{iterOutcome(42), "unknown"},
	}
	for _, tc := range cases {
		if got := tc.o.String(); got != tc.want {
			t.Errorf("iterOutcome(%d).String() = %q; want %q", tc.o, got, tc.want)
		}
	}
}
