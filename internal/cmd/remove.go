package cmd

import (
	"fmt"
	"strings"

	"github.com/husqylabs/stack/internal/branding"
	"github.com/husqylabs/stack/internal/forge"
	"github.com/spf13/cobra"
)

// newRemoveCmd: `<tool> remove <branch>` — untrack a branch from the stack. Any
// children are grafted onto the removed branch's parent so the DAG stays
// connected (run sync afterwards to rebase them onto the new parent). Optionally
// closes the PR and deletes the git branch. Nav comments on the remaining PRs in
// the affected stack are refreshed so they drop the removed PR.
func newRemoveCmd(d *deps) *cobra.Command {
	var trunk, owner, repo, remote string
	var closePR, deleteBranch bool

	cmd := &cobra.Command{
		Use:   branding.B.CmdRemove + " <branch>",
		Short: "Untrack a branch (grafting children onto its parent); optionally close its PR",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := d.load(trunk); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			ctx := cmd.Context()

			if _, ok := d.stack.Branches[name]; !ok {
				return fmt.Errorf("branch %q is not tracked", name)
			}
			if closePR && (owner == "" || repo == "") {
				return fmt.Errorf("--owner and --repo are required with --close-pr")
			}

			// Remember the connected PRs before removal so we can refresh their nav.
			component := d.stack.Component(name)
			children := d.stack.Children(name)

			removed, err := d.stack.Remove(name)
			if err != nil {
				return err
			}
			if err := d.store.Save(d.stack); err != nil {
				return err
			}
			if len(children) > 0 {
				names := make([]string, len(children))
				for i, c := range children {
					names[i] = c.Name
				}
				parent := removed.Parent
				if parent == "" {
					parent = d.stack.Trunk
				}
				fmt.Fprintf(out, "Reparented %d child(ren) onto %q: %s (run `%s %s` to rebase)\n",
					len(children), parent, strings.Join(names, ", "), branding.B.Name, branding.B.CmdSync)
			}
			fmt.Fprintf(out, "Untracked %q.\n", name)

			var gh *forge.GitHub
			if owner != "" && repo != "" {
				gh = forge.NewGitHub(owner, repo)
			}

			if closePR {
				if removed.PR == 0 {
					fmt.Fprintf(out, "  (no PR to close)\n")
				} else if err := gh.ClosePR(ctx, removed.PR); err != nil {
					return fmt.Errorf("close PR #%d: %w", removed.PR, err)
				} else {
					fmt.Fprintf(out, "  closed PR #%d\n", removed.PR)
				}
			}

			if deleteBranch {
				// Don't delete the branch we're standing on.
				if cur, _ := d.repo.CurrentBranch(); cur == name {
					dest := removed.Parent
					if dest == "" {
						dest = d.stack.Trunk
					}
					if o, err := d.repo.Checkout(ctx, dest); err != nil {
						return fmt.Errorf("switch off %q before deleting: %s", name, o)
					}
				}
				if o, err := d.repo.DeleteRemoteBranch(ctx, remote, name); err != nil {
					fmt.Fprintf(out, "  warning: could not delete %s/%s: %s\n", remote, name, strings.TrimSpace(o))
				}
				if o, err := d.repo.DeleteLocalBranch(ctx, name); err != nil {
					fmt.Fprintf(out, "  warning: could not delete local %s: %s\n", name, strings.TrimSpace(o))
				} else {
					fmt.Fprintf(out, "  deleted branch %q\n", name)
				}
			}

			// Refresh nav on the remaining PRs that shared the removed branch's stack.
			if gh != nil {
				for rn := range component {
					if rn == name {
						continue
					}
					rb := d.stack.Branches[rn]
					if rb == nil || rb.PR == 0 {
						continue
					}
					if nav, ok := forge.RenderNav(d.stack, rb.Name); ok {
						if err := gh.UpsertNavComment(ctx, rb.PR, nav); err != nil {
							return fmt.Errorf("refresh nav on PR #%d: %w", rb.PR, err)
						}
						fmt.Fprintf(out, "  refreshed nav on PR #%d\n", rb.PR)
					}
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&trunk, "trunk", "main", "trunk branch the stack roots onto")
	cmd.Flags().BoolVar(&closePR, "close-pr", false, "also close the branch's PR")
	cmd.Flags().BoolVar(&deleteBranch, "delete-branch", false, "also delete the local and remote git branch")
	cmd.Flags().StringVar(&remote, "remote", "origin", "git remote (for --delete-branch)")
	cmd.Flags().StringVar(&owner, "owner", "", "forge repo owner/org (for --close-pr / nav refresh)")
	cmd.Flags().StringVar(&repo, "repo", "", "forge repo name (for --close-pr / nav refresh)")
	return cmd
}
