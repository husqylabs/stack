package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/husqylabs/stack/internal/branding"
	"github.com/husqylabs/stack/internal/stack"
)

// GitHub is a minimal REST client — only what stack sync/submit need. A real
// build would swap in google/go-github, but the hidden-comment protocol is the
// same: read PR body -> Extract, modify, Embed -> write PR body back.
type GitHub struct {
	Owner, Repo string
	token       string
	http        *http.Client
	base        string // override for tests; defaults to api.github.com
}

func NewGitHub(owner, repo string) *GitHub {
	return &GitHub{
		Owner: owner,
		Repo:  repo,
		token: os.Getenv(branding.B.Env("TOKEN")), // e.g. STACK_TOKEN
		http:  &http.Client{Timeout: 15 * time.Second},
		base:  "https://api.github.com",
	}
}

// PR mirrors the slice of GitHub's pull-request object we care about. Base/Head
// carry the branch a PR merges into / comes from — central to stacked PRs, where
// each PR's base is its parent branch rather than trunk.
type PR struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"html_url"`
	Body   string `json:"body"`
	State  string `json:"state"`
	Base   struct {
		Ref string `json:"ref"`
	} `json:"base"`
	Head struct {
		Ref string `json:"ref"`
	} `json:"head"`
}

// NewPR is the input to CreatePR.
type NewPR struct {
	Title string
	Head  string // branch the changes are on
	Base  string // branch to merge into (the parent in the stack)
	Body  string
	Draft bool
}

// FindPR returns the open PR whose head is `branch`, or (nil, nil) if none.
// GET /repos/{o}/{r}/pulls?head={owner}:{branch}&state=open
func (g *GitHub) FindPR(ctx context.Context, branch string) (*PR, error) {
	q := url.Values{}
	q.Set("head", g.Owner+":"+branch)
	q.Set("state", "open")
	path := fmt.Sprintf("/repos/%s/%s/pulls?%s", g.Owner, g.Repo, q.Encode())

	var prs []PR
	if err := g.doJSON(ctx, http.MethodGet, path, nil, &prs); err != nil {
		return nil, fmt.Errorf("list PRs for %q: %w", branch, err)
	}
	if len(prs) == 0 {
		return nil, nil
	}
	return &prs[0], nil
}

// CreatePR opens a new pull request.
// POST /repos/{o}/{r}/pulls
func (g *GitHub) CreatePR(ctx context.Context, in NewPR) (*PR, error) {
	payload := map[string]any{
		"title": in.Title,
		"head":  in.Head,
		"base":  in.Base,
		"body":  in.Body,
		"draft": in.Draft,
	}
	var out PR
	path := fmt.Sprintf("/repos/%s/%s/pulls", g.Owner, g.Repo)
	if err := g.doJSON(ctx, http.MethodPost, path, payload, &out); err != nil {
		return nil, fmt.Errorf("create PR for %q -> %q: %w", in.Head, in.Base, err)
	}
	return &out, nil
}

// SetBase repoints an existing PR's base branch and returns the updated PR (the
// PATCH response carries the current title, which we reuse for the nav comment).
// Needed when a branch is re-parented within the stack.
// PATCH /repos/{o}/{r}/pulls/{n} with {"base": ...}
func (g *GitHub) SetBase(ctx context.Context, number int, base string) (*PR, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d", g.Owner, g.Repo, number)
	var out PR
	if err := g.doJSON(ctx, http.MethodPatch, path, map[string]string{"base": base}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// FetchStack is the MOCKUP the spec asked for: extract the JSON stack state from
// a PR's hidden comment via the REST API. Because state lives in the PR itself,
// ANY teammate can reconstruct the stack with just a token — no backend.
func (g *GitHub) FetchStack(ctx context.Context, number int) (*stack.Stack, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d", g.Owner, g.Repo, number)
	var p PR
	if err := g.doJSON(ctx, http.MethodGet, path, nil, &p); err != nil {
		return nil, fmt.Errorf("get PR #%d: %w", number, err)
	}
	s, found, err := Extract(p.Body)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("PR #%d has no embedded %s state", number, branding.B.Title)
	}
	return s, nil
}

// PublishStack writes the stack into the PR body (read-modify-write) so the next
// teammate to sync sees the latest DAG, preserving the human-written description.
func (g *GitHub) PublishStack(ctx context.Context, number int, s *stack.Stack) error {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d", g.Owner, g.Repo, number)

	var current PR
	if err := g.doJSON(ctx, http.MethodGet, path, nil, &current); err != nil {
		return fmt.Errorf("get PR #%d: %w", number, err)
	}
	newBody, err := Embed(current.Body, s)
	if err != nil {
		return err
	}
	return g.doJSON(ctx, http.MethodPatch, path, map[string]string{"body": newBody}, nil)
}

// issueComment is the slice of GitHub's comment object we need to find and
// update our own nav comment. (PRs are issues, so PR comments use the issues API.)
type issueComment struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
}

// UpsertNavComment writes the stack-navigation comment into a PR, updating our
// existing comment in place if present (matched by the hidden nav marker) and
// creating it otherwise. Idempotent: safe to call on every submit.
func (g *GitHub) UpsertNavComment(ctx context.Context, number int, body string) error {
	listPath := fmt.Sprintf("/repos/%s/%s/issues/%d/comments?per_page=100", g.Owner, g.Repo, number)
	var comments []issueComment
	if err := g.doJSON(ctx, http.MethodGet, listPath, nil, &comments); err != nil {
		return fmt.Errorf("list comments on #%d: %w", number, err)
	}

	for _, c := range comments {
		if strings.Contains(c.Body, branding.B.NavMarker) {
			path := fmt.Sprintf("/repos/%s/%s/issues/comments/%d", g.Owner, g.Repo, c.ID)
			return g.doJSON(ctx, http.MethodPatch, path, map[string]string{"body": body}, nil)
		}
	}

	createPath := fmt.Sprintf("/repos/%s/%s/issues/%d/comments", g.Owner, g.Repo, number)
	return g.doJSON(ctx, http.MethodPost, createPath, map[string]string{"body": body}, nil)
}

// doJSON performs one REST call: marshals `in` (if non-nil), sets auth headers,
// checks for a 2xx, and decodes the response into `out` (if non-nil). This is the
// single chokepoint for HTTP concerns so the methods above stay declarative.
func (g *GitHub) doJSON(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, g.base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if g.token != "" {
		req.Header.Set("Authorization", "Bearer "+g.token)
	}

	resp, err := g.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, msg)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}
