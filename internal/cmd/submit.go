package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/husqylabs/stack/internal/branding"
	"github.com/husqylabs/stack/internal/forge"
	"github.com/husqylabs/stack/internal/stack"
	"github.com/spf13/cobra"
)

// newSubmitCmd: `<tool> submit` — for every branch in the stack, push it and
// ensure a PR exists whose base is its parent, then publish the full stack DAG
// into each PR's hidden comment so teammates stay in sync with no backend.
//
// Two passes on purpose:
//
//	Pass 1 (push + ensure PR): walks the stack parent-first, pushing each branch
//	  and finding-or-creating its PR. We can only learn a branch's PR number here,
//	  and a child's PR base is its parent branch — so order matters and we persist
//	  after each branch to keep learned PR numbers on failure.
//
//	Pass 2 (publish state): only after every PR number is known can the embedded
//	  DAG be complete. We write the final stack into every PR so each carries the
//	  whole picture, not a snapshot from when it happened to be processed.
func newSubmitCmd(d *deps) *cobra.Command {
	var trunk, remote, owner, repo string
	var draft bool

	cmd := &cobra.Command{
		Use:   branding.B.CmdSubmit,
		Short: "Push the stack, open/refresh PRs, and publish state into each PR",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := d.load(trunk); err != nil {
				return err
			}
			if owner == "" || repo == "" {
				return fmt.Errorf("--owner and --repo are required (forge lookup needs them)")
			}
			out := cmd.OutOrStdout()
			ctx := cmd.Context()

			order, err := d.stack.TopoOrder()
			if err != nil {
				return err
			}
			gh := forge.NewGitHub(owner, repo)

			// Pass 1: push + ensure each PR exists with the right base.
			for _, b := range order {
				if b.Parent == "" {
					continue // trunk-rooted base branch: nothing to PR against
				}
				if _, err := d.repo.Push(ctx, remote, b.Name); err != nil {
					return fmt.Errorf("push %q: %w", b.Name, err)
				}
				fmt.Fprintf(out, "  pushed %s\n", b.Name)

				if err := d.ensurePR(ctx, gh, b, draft, out); err != nil {
					return err
				}
				if err := d.store.Save(d.stack); err != nil {
					return err
				}
			}

			// Pass 2: with every PR number and title known, publish the complete
			// DAG into each PR body and refresh its navigation comment.
			for _, b := range order {
				if b.PR == 0 {
					continue
				}
				if err := gh.PublishStack(ctx, b.PR, d.stack); err != nil {
					return fmt.Errorf("publish state to PR #%d: %w", b.PR, err)
				}
				if nav, ok := forge.RenderNav(d.stack, b.Name); ok {
					if err := gh.UpsertNavComment(ctx, b.PR, nav); err != nil {
						return fmt.Errorf("update nav comment on PR #%d: %w", b.PR, err)
					}
				}
				fmt.Fprintf(out, "  synced state + nav into PR #%d (%s)\n", b.PR, b.Name)
			}

			fmt.Fprintln(out, "Submitted.")
			return nil
		},
	}

	cmd.Flags().StringVar(&trunk, "trunk", "main", "trunk branch the stack roots onto")
	cmd.Flags().StringVar(&remote, "remote", "origin", "git remote to push to")
	cmd.Flags().StringVar(&owner, "owner", "", "forge repo owner/org")
	cmd.Flags().StringVar(&repo, "repo", "", "forge repo name")
	cmd.Flags().BoolVar(&draft, "draft", false, "open new PRs as drafts")
	return cmd
}

// ensurePR makes sure b has an open PR whose base is b.Parent, creating it if
// needed and reconciling the base if the branch was re-parented. The resulting PR
// number and title are written back onto b (caller persists); the title feeds the
// nav comment and rides along in the embedded state for adopt.
func (d *deps) ensurePR(ctx context.Context, gh *forge.GitHub, b *stack.Branch, draft bool, out io.Writer) error {
	// Discover the PR if we don't already track its number.
	if b.PR == 0 {
		existing, err := gh.FindPR(ctx, b.Name)
		if err != nil {
			return err
		}
		if existing != nil {
			b.PR = existing.Number
			b.Title, b.URL = existing.Title, existing.URL
			if existing.Base.Ref != b.Parent {
				updated, err := gh.SetBase(ctx, b.PR, b.Parent)
				if err != nil {
					return fmt.Errorf("repoint PR #%d base to %q: %w", b.PR, b.Parent, err)
				}
				b.Title, b.URL = updated.Title, updated.URL
				fmt.Fprintf(out, "  repointed PR #%d base -> %s\n", b.PR, b.Parent)
			}
			return nil
		}

		title := d.repo.FirstCommitSubject(b.ParentCommit, b.Name)
		created, err := gh.CreatePR(ctx, forge.NewPR{
			Title: title,
			Head:  b.Name,
			Base:  b.Parent,
			Draft: draft,
			// Body is filled by PublishStack in pass 2.
		})
		if err != nil {
			return err
		}
		b.PR = created.Number
		b.Title, b.URL = created.Title, created.URL
		fmt.Fprintf(out, "  opened PR #%d %s -> %s\n", b.PR, b.Name, b.Parent)
		return nil
	}

	// Known PR: keep its base aligned with the current parent (cheap, idempotent;
	// matters after a re-parent). PATCH to the same base is a no-op server-side and
	// returns the current title for the nav comment.
	updated, err := gh.SetBase(ctx, b.PR, b.Parent)
	if err != nil {
		return fmt.Errorf("align PR #%d base to %q: %w", b.PR, b.Parent, err)
	}
	b.Title, b.URL = updated.Title, updated.URL
	return nil
}
