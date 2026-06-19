package cmd

import (
	"fmt"

	"github.com/husqylabs/stack/internal/branding"
	"github.com/husqylabs/stack/internal/stack"
	"github.com/spf13/cobra"
)

// newReparentCmd: `<tool> reparent <branch> --onto <new-parent>` — move a branch
// to a different parent in the stack and rebase its commits onto the new base.
//
// The git math is the same single `--onto` step the cascade uses:
//
//	git rebase --onto <new-parent tip> <branch's recorded old base> <branch>
//
// The recorded ParentCommit is exactly the commits unique to the branch, so only
// those replay onto the new parent. After this, the branch's own children are
// stale (their recorded base is this branch's *old* tip); a subsequent `sync`
// cascades them onto the new tip — we don't touch descendants here.
func newReparentCmd(d *deps) *cobra.Command {
	var trunk, onto string

	cmd := &cobra.Command{
		Use:   branding.B.CmdReparent + " <branch> --onto <new-parent>",
		Short: "Move a branch onto a different parent and rebase it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if onto == "" {
				return fmt.Errorf("--onto <new-parent> is required")
			}
			if err := d.load(trunk); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			ctx := cmd.Context()

			b, ok := d.stack.Branches[name]
			if !ok {
				return fmt.Errorf("branch %q is not tracked", name)
			}
			if onto == name {
				return fmt.Errorf("cannot reparent %q onto itself", name)
			}
			// New parent must be the trunk or a tracked branch.
			_, tracked := d.stack.Branches[onto]
			if !tracked && onto != d.stack.Trunk {
				return fmt.Errorf("new parent %q is neither trunk %q nor a tracked branch", onto, d.stack.Trunk)
			}
			// Reject cycles: the new parent must not be a descendant of this branch.
			if d.stack.IsAncestor(name, onto) {
				return fmt.Errorf("cannot reparent %q onto its own descendant %q", name, onto)
			}

			newBaseHash, err := d.repo.Tip(onto)
			if err != nil {
				return err
			}
			newBase := newBaseHash.String()
			oldBase := b.ParentCommit

			if b.Parent == onto && oldBase == newBase {
				fmt.Fprintf(out, "%q is already on %q; nothing to do.\n", name, onto)
				return nil
			}

			// Write-ahead journal BEFORE touching git. If the rebase conflicts,
			// crashes, or is aborted, reconciliation (run by every command) heals
			// our metadata to whatever git actually did — we never record a
			// half-applied intent.
			if err := d.store.SavePending(&stack.Pending{
				Op: "reparent", Branch: name, OldBase: oldBase, NewBase: newBase, NewParent: onto,
			}); err != nil {
				return err
			}

			res := d.repo.RebaseOnto(ctx, newBase, oldBase, name)

			if res.Conflicted {
				// Leave the journal; change NOTHING. Whether the user runs
				// `git rebase --continue` or `--abort`, the next command reconciles.
				return fmt.Errorf("reparent of %q hit conflicts; resolve and run `git rebase --continue` (or `git rebase --abort` to cancel) — either way the next %s command will reconcile",
					name, branding.B.Name)
			}
			if res.NewTip == "" {
				// Hard failure (bad ref/args): git started no rebase, so drop the
				// journal and leave state untouched.
				_ = d.store.ClearPending()
				return fmt.Errorf("reparent of %q failed: %s", name, res.Detail)
			}

			// Clean success: commit the new topology and clear the journal.
			b.Parent = onto
			b.ParentCommit = newBase
			if err := d.store.Save(d.stack); err != nil {
				return err
			}
			if err := d.store.ClearPending(); err != nil {
				return err
			}

			fmt.Fprintf(out, "Reparented %q onto %q (%s -> %s).\n",
				name, onto, short(oldBase), short(res.NewTip))
			fmt.Fprintf(out, "Run `%s %s` to cascade any children onto the new tip.\n",
				branding.B.Name, branding.B.CmdSync)
			return nil
		},
	}

	cmd.Flags().StringVar(&trunk, "trunk", "main", "trunk branch the stack roots onto")
	cmd.Flags().StringVar(&onto, "onto", "", "the new parent branch")
	return cmd
}
