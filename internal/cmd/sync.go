package cmd

import (
	"fmt"

	"github.com/husqylabs/stack/internal/branding"
	"github.com/husqylabs/stack/internal/git"
	"github.com/husqylabs/stack/internal/stack"
	"github.com/spf13/cobra"
)

// newSyncCmd: `<tool> sync` — cascade-rebase the whole stack so every child sits
// on its parent's current tip. The hard part lives in git.Repo.Cascade.
func newSyncCmd(d *deps) *cobra.Command {
	var trunk string

	cmd := &cobra.Command{
		Use:   branding.B.CmdSync,
		Short: "Cascade-rebase every branch onto its updated parent",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := d.load(trunk); err != nil {
				return err
			}
			out := cmd.OutOrStdout()

			hooks := git.CascadeHooks{
				// Write-ahead journal before each rebase so any interruption
				// (conflict, crash, abort) is reconciled on the next command.
				BeforeRebase: func(branch, oldBase, newBase string) error {
					return d.store.SavePending(&stack.Pending{
						Op: "sync", Branch: branch, OldBase: oldBase, NewBase: newBase,
					})
				},
				// Persist advanced state and clear the journal after each clean
				// rebase, so resume picks up exactly at the conflicted branch.
				AfterRebase: func(b *stack.Branch, res git.RebaseResult) error {
					if err := d.store.Save(d.stack); err != nil {
						return err
					}
					fmt.Fprintf(out, "  rebased %-20s %s -> %s\n",
						b.Name, short(res.OldBase), short(res.NewTip))
					return d.store.ClearPending()
				},
			}

			results, err := d.repo.Cascade(cmd.Context(), d.stack, hooks)
			for _, r := range results {
				if r.Skipped {
					fmt.Fprintf(out, "  up-to-date %s\n", r.Branch)
				}
			}
			if err != nil {
				return err // includes the "resolve conflicts then re-run" guidance
			}

			if err := d.store.Save(d.stack); err != nil {
				return err
			}
			fmt.Fprintln(out, "Stack synced.")
			return nil
		},
	}

	cmd.Flags().StringVar(&trunk, "trunk", "main", "trunk branch the stack roots onto")
	return cmd
}

func short(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}
