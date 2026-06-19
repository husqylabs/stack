package cmd

import (
	"fmt"

	"github.com/husqylabs/stack/internal/branding"
	"github.com/spf13/cobra"
)

// newStartCmd: `<tool> start <name>` — create a new branch on top of the current
// one and start tracking it in the stack DAG.
func newStartCmd(d *deps) *cobra.Command {
	var trunk, parent string

	cmd := &cobra.Command{
		Use:   branding.B.CmdStart + " <name>",
		Short: "Create a branch on top of the current branch and track it in the stack",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := d.load(trunk); err != nil {
				return err
			}

			// Parent defaults to the current HEAD branch; record its tip as the
			// new branch's base (the rebase --onto "old base" for future syncs).
			if parent == "" {
				cur, err := d.repo.CurrentBranch()
				if err != nil {
					return err
				}
				parent = cur
			}
			parentTip, err := d.repo.Tip(parent)
			if err != nil {
				return err
			}

			if err := d.repo.CreateBranch(cmd.Context(), name, parent); err != nil {
				return err
			}
			if _, err := d.stack.Add(name, parent, parentTip.String()); err != nil {
				return err
			}
			if err := d.store.Save(d.stack); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Started %q on top of %q (base %s)\n",
				name, parent, parentTip.String()[:12])
			return nil
		},
	}

	cmd.Flags().StringVar(&trunk, "trunk", "main", "trunk branch the stack roots onto")
	cmd.Flags().StringVar(&parent, "parent", "", "parent branch (defaults to current branch)")
	return cmd
}
