package forge

import (
	"strings"
	"testing"

	"github.com/husqylabs/stack/internal/stack"
)

func TestEmbedExtractRoundTrip(t *testing.T) {
	s := stack.New("main")
	s.Add("feat-a", "main", "aaaa")
	s.Add("feat-b", "feat-a", "bbbb")

	body, err := Embed("My PR description.\n", s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "My PR description.") {
		t.Fatal("human body was clobbered")
	}
	if !strings.Contains(body, "<!--") {
		t.Fatal("state block not embedded as HTML comment")
	}

	got, found, err := Extract(body)
	if err != nil || !found {
		t.Fatalf("extract failed: found=%v err=%v", found, err)
	}
	if got.Trunk != "main" || len(got.Branches) != 2 || got.Branches["feat-b"].Parent != "feat-a" {
		t.Fatalf("round-trip lost data: %+v", got)
	}
}

func TestEmbedIsIdempotent(t *testing.T) {
	s := stack.New("main")
	s.Add("x", "main", "1111")

	once, _ := Embed("desc", s)
	twice, _ := Embed(once, s) // re-embedding must replace, not duplicate
	if strings.Count(twice, "<!--") != 1 {
		t.Fatalf("expected exactly one state block, got:\n%s", twice)
	}
}

func TestTopoOrderParentBeforeChild(t *testing.T) {
	s := stack.New("main")
	s.Add("c", "b", "cc")
	s.Add("a", "main", "aa")
	s.Add("b", "a", "bb")

	order, err := s.TopoOrder()
	if err != nil {
		t.Fatal(err)
	}
	pos := map[string]int{}
	for i, br := range order {
		pos[br.Name] = i
	}
	if !(pos["a"] < pos["b"] && pos["b"] < pos["c"]) {
		t.Fatalf("topo order violated parent-before-child: %v", names(order))
	}
}

func names(bs []*stack.Branch) []string {
	out := make([]string, len(bs))
	for i, b := range bs {
		out[i] = b.Name
	}
	return out
}
