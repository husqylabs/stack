package stack

import "testing"

// build main <- a <- b <- c
func chain() *Stack {
	s := New("main")
	s.Add("a", "main", "a0")
	s.Add("b", "a", "b0")
	s.Add("c", "b", "c0")
	return s
}

func TestIsAncestor(t *testing.T) {
	s := chain()
	cases := []struct {
		ancestor, of string
		want         bool
	}{
		{"a", "c", true},  // a is above c
		{"b", "c", true},  // direct parent
		{"c", "a", false}, // c is below a, not an ancestor
		{"a", "a", false},   // not its own ancestor via chain
		{"main", "c", true}, // trunk sits at the top of the chain (a.Parent == "main")
		{"main", "a", true}, // ...directly above a too
	}
	for _, tc := range cases {
		if got := s.IsAncestor(tc.ancestor, tc.of); got != tc.want {
			t.Errorf("IsAncestor(%q,%q)=%v want %v", tc.ancestor, tc.of, got, tc.want)
		}
	}
}

// A reparent is a cycle iff the new parent is the branch itself or a descendant,
// which IsAncestor(branch, newParent) detects.
func TestReparentCycleGuard(t *testing.T) {
	s := chain()
	// Moving a onto c would make c (a descendant of a) the parent of a -> cycle.
	if !s.IsAncestor("a", "c") {
		t.Fatal("expected reparent a -> c to be rejected as a cycle")
	}
	// Moving c onto a is fine (a is not a descendant of c).
	if s.IsAncestor("c", "a") {
		t.Fatal("reparent c -> a should be allowed")
	}
}

func TestIsAncestorTerminatesOnPreexistingCycle(t *testing.T) {
	// Defensive: a corrupted state with a parent cycle must not hang.
	s := New("main")
	s.Add("x", "y", "x0")
	s.Add("y", "x", "y0") // x<->y cycle
	_ = s.IsAncestor("x", "y") // must return, not loop forever
}
