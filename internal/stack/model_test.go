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

func TestRemoveGraftsChildrenOntoParent(t *testing.T) {
	s := chain() // main <- a <- b <- c

	removed, err := s.Remove("b")
	if err != nil {
		t.Fatal(err)
	}
	if removed.Name != "b" {
		t.Fatalf("returned %q, want b", removed.Name)
	}
	if _, ok := s.Branches["b"]; ok {
		t.Fatal("b should be untracked")
	}
	// c was b's child; it must now point at b's parent, a.
	if got := s.Branches["c"].Parent; got != "a" {
		t.Fatalf("c.Parent = %q, want a (grafted onto grandparent)", got)
	}
	// The DAG stays connected and acyclic.
	if _, err := s.TopoOrder(); err != nil {
		t.Fatalf("topo order broke after remove: %v", err)
	}
}

func TestRemoveUntrackedBranchErrors(t *testing.T) {
	s := chain()
	if _, err := s.Remove("nope"); err == nil {
		t.Fatal("expected error removing an untracked branch")
	}
}

func TestIsAncestorTerminatesOnPreexistingCycle(t *testing.T) {
	// Defensive: a corrupted state with a parent cycle must not hang.
	s := New("main")
	s.Add("x", "y", "x0")
	s.Add("y", "x", "y0") // x<->y cycle
	_ = s.IsAncestor("x", "y") // must return, not loop forever
}
