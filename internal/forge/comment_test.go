package forge

import (
	"fmt"
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

func TestRenderNav_OnlyConnectedNewestFirstWithArrow(t *testing.T) {
	// Two sibling stacks off main: a<-b, and c (separate). PRs assigned.
	s := stack.New("main")
	mk := func(name, parent string, pr int, title string) {
		b, _ := s.Add(name, parent, "h")
		b.PR = pr
		b.Title = title
		b.URL = fmt.Sprintf("https://example.test/pull/%d", pr)
	}
	mk("a", "main", 1, "first")
	mk("b", "a", 2, "second")
	mk("c", "main", 3, "sibling") // shares trunk only -> different stack

	nav, ok := RenderNav(s, "b")
	if !ok {
		t.Fatal("expected a nav block")
	}
	// Title is the only text, hyperlinked to the PR.
	if !strings.Contains(nav, "[second](https://example.test/pull/2)") {
		t.Fatalf("title should be a hyperlink to the PR:\n%s", nav)
	}
	// Sibling stack must NOT appear in b's stack.
	if strings.Contains(nav, "sibling") || strings.Contains(nav, "pull/3") {
		t.Fatalf("sibling stack leaked into nav:\n%s", nav)
	}
	// Newest-first: the child (pull/2) should appear before its parent (pull/1).
	if strings.Index(nav, "pull/2") > strings.Index(nav, "pull/1") {
		t.Fatalf("expected newest-first order:\n%s", nav)
	}
	// Current PR (b) marked with trailing arrow; the other not.
	lines := strings.Split(nav, "\n")
	var line1, line2 string
	for _, ln := range lines {
		if strings.Contains(ln, "second") {
			line2 = ln
		}
		if strings.Contains(ln, "first") {
			line1 = ln
		}
	}
	if !strings.HasSuffix(line2, "←") || !strings.Contains(line2, "**") {
		t.Fatalf("current PR not marked with trailing arrow: %q", line2)
	}
	if strings.Contains(line1, "←") {
		t.Fatalf("non-current PR wrongly marked: %q", line1)
	}

	// And from c's perspective, only its own stack shows.
	navC, ok := RenderNav(s, "c")
	if !ok || strings.Contains(navC, "pull/1") || strings.Contains(navC, "pull/2") {
		t.Fatalf("c's nav should contain only its own stack:\n%s", navC)
	}
	if !strings.Contains(navC, "pull/3") {
		t.Fatalf("c's nav should contain its own PR:\n%s", navC)
	}
}

func TestRenderNav_NoPRsReturnsFalse(t *testing.T) {
	s := stack.New("main")
	s.Add("a", "main", "h") // no PR assigned
	if _, ok := RenderNav(s, "a"); ok {
		t.Fatal("expected no nav block when nothing is submitted")
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
