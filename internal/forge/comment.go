// Package forge handles stateless cross-teammate sync: the stack DAG is embedded
// in each PR's description as a hidden HTML comment, so anyone's CLI can recover
// the full stack from the forge with no external backend.
//
// This file is forge-agnostic (just (de)serialization of the comment block);
// github.go holds the GitHub REST specifics.
package forge

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/husqylabs/stack/internal/branding"
	"github.com/husqylabs/stack/internal/stack"
)

// Embed returns `body` with the hidden state block appended (or replaced if one
// already exists). The block is an HTML comment, invisible in rendered markdown:
//
//	<!-- stack-state:v1
//	{ ...json... }
//	stack-state:end -->
func Embed(body string, s *stack.Stack) (string, error) {
	jsonBytes, err := s.Encode()
	if err != nil {
		return "", err
	}
	block := fmt.Sprintf("<!-- %s\n%s\n%s -->",
		branding.B.OpenMarker(), string(jsonBytes), branding.B.CloseMarker())

	stripped := strings.TrimRight(stripBlock(body), "\n")
	if stripped == "" {
		return block, nil
	}
	return stripped + "\n\n" + block, nil
}

// Extract pulls the stack out of a PR/MR body. Returns (nil, false) if no block
// is present (e.g. the PR predates the tool).
func Extract(body string) (*stack.Stack, bool, error) {
	m := blockRE().FindStringSubmatch(body)
	if m == nil {
		return nil, false, nil
	}
	s, err := stack.Decode([]byte(strings.TrimSpace(m[1])))
	if err != nil {
		return nil, true, fmt.Errorf("parse embedded stack state: %w", err)
	}
	return s, true, nil
}

// stripBlock removes an existing state block so Embed can replace it idempotently.
func stripBlock(body string) string {
	return blockRE().ReplaceAllString(body, "")
}

// RenderNav builds the stack-navigation comment for the PR on branch `current`.
// It lists only the PRs in `current`'s connected stack (branches reachable
// through tracked parent/child edges — sibling stacks that merely share the trunk
// are excluded), newest-first (leaves before roots), shows each PR's title, and
// marks the current PR with an arrow. The trunk never appears. A hidden marker is
// embedded so the comment can be found and updated in place.
//
// Returns ("", false) if there is nothing to link (no connected branch has a PR).
func RenderNav(s *stack.Stack, current string) (string, bool) {
	order, err := s.TopoOrder() // parents-first
	if err != nil {
		return "", false
	}
	connected := s.Component(current)

	var b strings.Builder
	fmt.Fprintf(&b, "<!-- %s -->\n", branding.B.NavMarker)
	b.WriteString("**\U0001F4DA This stack** · newest first\n\n")

	any := false
	// Walk in reverse so the newest (deepest) branches sit on top.
	for i := len(order) - 1; i >= 0; i-- {
		br := order[i]
		if br.PR == 0 || !connected[br.Name] {
			continue // unsubmitted, or part of a different (sibling) stack
		}
		any = true
		title := br.Title
		if title == "" {
			title = br.Name
		}
		// The PR title is the only text, linked to the PR. Fall back to a plain
		// "#<n>" reference if we somehow have no URL yet (still auto-links in-repo).
		var entry string
		if br.URL != "" {
			entry = fmt.Sprintf("[%s](%s)", title, br.URL)
		} else {
			entry = fmt.Sprintf("[%s](#%d)", title, br.PR)
		}
		if br.Name == current {
			fmt.Fprintf(&b, "- **%s** ←\n", entry) // current PR: left arrow at end
		} else {
			fmt.Fprintf(&b, "- %s\n", entry)
		}
	}
	if !any {
		return "", false
	}

	fmt.Fprintf(&b, "\n<sub>Auto-updated by `%s`</sub>", branding.B.Name)
	return b.String(), true
}

// blockRE matches the whole hidden comment and captures the JSON payload (group 1).
// Markers come from branding, so a rebrand changes the protocol in one place.
func blockRE() *regexp.Regexp {
	open := regexp.QuoteMeta(branding.B.OpenMarker())
	end := regexp.QuoteMeta(branding.B.CloseMarker())
	// <!-- <open> \n (json) \n <end> -->
	return regexp.MustCompile(`(?s)<!--\s*` + open + `\s*(.*?)\s*` + end + `\s*-->`)
}
