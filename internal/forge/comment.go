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

// blockRE matches the whole hidden comment and captures the JSON payload (group 1).
// Markers come from branding, so a rebrand changes the protocol in one place.
func blockRE() *regexp.Regexp {
	open := regexp.QuoteMeta(branding.B.OpenMarker())
	end := regexp.QuoteMeta(branding.B.CloseMarker())
	// <!-- <open> \n (json) \n <end> -->
	return regexp.MustCompile(`(?s)<!--\s*` + open + `\s*(.*?)\s*` + end + `\s*-->`)
}
