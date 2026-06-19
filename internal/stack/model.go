// Package stack defines the branch DAG model and how it is persisted and
// serialized. It knows nothing about git plumbing or any forge (GitHub/GitLab);
// those live in sibling packages. This package is the "what", not the "how".
package stack

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Branch is one node in the stack DAG.
//
// ParentCommit is the crux of the whole tool: it records the commit hash of the
// parent *at the time this branch was last synced onto it*. That recorded hash
// is the "old base" handed to `git rebase --onto <new-parent-tip> <old-base>`.
// Without it we cannot tell which commits are uniquely ours versus inherited
// from the parent, and the cascade would replay the wrong range.
type Branch struct {
	Name         string `json:"name"`
	Parent       string `json:"parent"`         // "" for a trunk-rooted base branch
	ParentCommit string `json:"parent_commit"`  // recorded parent tip; the rebase --onto "old base"
	PR           int    `json:"pr,omitempty"`   // forge PR/MR number, 0 if unsubmitted
	Title        string `json:"title,omitempty"` // PR title, captured at submit; used in the nav comment
}

// Stack is the DAG: a flat map of branches keyed by name, plus the trunk they
// ultimately root onto (e.g. "main"). A flat map keeps serialization trivial;
// parent/child structure is derived on demand.
type Stack struct {
	Trunk    string             `json:"trunk"`
	Branches map[string]*Branch `json:"branches"`
}

// New returns an empty stack rooted on the given trunk.
func New(trunk string) *Stack {
	return &Stack{Trunk: trunk, Branches: map[string]*Branch{}}
}

// Add inserts a branch. parent may be the trunk name or another tracked branch.
func (s *Stack) Add(name, parent, parentCommit string) (*Branch, error) {
	if name == "" {
		return nil, errors.New("branch name required")
	}
	if _, exists := s.Branches[name]; exists {
		return nil, fmt.Errorf("branch %q already tracked", name)
	}
	b := &Branch{Name: name, Parent: parent, ParentCommit: parentCommit}
	s.Branches[name] = b
	return b, nil
}

// Children returns the branches whose Parent is the given name.
func (s *Stack) Children(name string) []*Branch {
	var out []*Branch
	for _, b := range s.Branches {
		if b.Parent == name {
			out = append(out, b)
		}
	}
	return out
}

// Component returns the set of branch names connected to `name` through tracked
// parent/child edges (the connected stack `name` belongs to). The trunk is not a
// tracked branch, so two branches that both root on the trunk are NOT connected
// unless one descends from the other — i.e. sibling stacks stay separate.
func (s *Stack) Component(name string) map[string]bool {
	seen := map[string]bool{}
	if _, ok := s.Branches[name]; !ok {
		return seen
	}
	queue := []string{name}
	for len(queue) > 0 {
		cur := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if seen[cur] {
			continue
		}
		seen[cur] = true
		b := s.Branches[cur]
		if b == nil {
			continue
		}
		// Up: the parent, but only if it is itself a tracked branch (not trunk).
		if _, ok := s.Branches[b.Parent]; ok && !seen[b.Parent] {
			queue = append(queue, b.Parent)
		}
		// Down: every tracked child.
		for _, ch := range s.Children(cur) {
			if !seen[ch.Name] {
				queue = append(queue, ch.Name)
			}
		}
	}
	return seen
}

// IsAncestor reports whether `ancestor` lies on the parent chain above `of`
// within the tracked DAG. Used to reject a reparent that would form a cycle.
func (s *Stack) IsAncestor(ancestor, of string) bool {
	cur := of
	for i := 0; i < len(s.Branches)+1; i++ { // bounded: guards against a pre-existing cycle
		b := s.Branches[cur]
		if b == nil || b.Parent == "" {
			return false
		}
		if b.Parent == ancestor {
			return true
		}
		cur = b.Parent
	}
	return false
}

// TopoOrder returns branches in dependency order: every branch appears after its
// parent. This is the order the cascade must process so a child is always rebased
// onto an already-updated parent. Returns an error if a cycle is detected (the
// DAG invariant is violated).
func (s *Stack) TopoOrder() ([]*Branch, error) {
	const (
		white = 0 // unvisited
		gray  = 1 // on the current DFS stack -> back-edge means cycle
		black = 2 // fully processed
	)
	color := make(map[string]int, len(s.Branches))
	var order []*Branch

	var visit func(name string) error
	visit = func(name string) error {
		switch color[name] {
		case gray:
			return fmt.Errorf("cycle detected in stack at %q", name)
		case black:
			return nil
		}
		color[name] = gray
		b := s.Branches[name]
		if b != nil && b.Parent != "" && b.Parent != s.Trunk {
			if _, tracked := s.Branches[b.Parent]; tracked {
				if err := visit(b.Parent); err != nil {
					return err
				}
			}
		}
		color[name] = black
		if b != nil {
			order = append(order, b)
		}
		return nil
	}

	for name := range s.Branches {
		if err := visit(name); err != nil {
			return nil, err
		}
	}
	return order, nil
}

// MarshalJSON / UnmarshalJSON use the default struct encoding; declared here only
// to keep the serialization format colocated with the type it describes.
func (s *Stack) Encode() ([]byte, error) { return json.MarshalIndent(s, "", "  ") }

func Decode(data []byte) (*Stack, error) {
	var s Stack
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	if s.Branches == nil {
		s.Branches = map[string]*Branch{}
	}
	return &s, nil
}
