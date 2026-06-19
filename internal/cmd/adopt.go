package cmd

import (
	"fmt"

	"github.com/husqylabs/stack/internal/branding"
	"github.com/husqylabs/stack/internal/forge"
	"github.com/spf13/cobra"
)

// newAdoptCmd: `<tool> adopt <pr-number>` — reconstruct a teammate's stack from a
// PR's hidden comment, with no backend. This is the consumer side of submit's
// PublishStack: the DAG round-trips entirely through the forge.
//
// Flow:
//  1. FetchStack(pr)            -> the full DAG from the hidden comment
//  2. git fetch <remote>        -> get the branch commits locally
//  3. materialize local branches for anything we don't already have
//  4. save the DAG to the local store -> sync/submit now work as usual
func newAdoptCmd(d *deps) *cobra.Command {
	var remote, owner, repo string

	cmd := &cobra.Command{
		Use:   branding.B.CmdAdopt + " <pr-number>",
		Short: "Reconstruct a teammate's stack from a PR's embedded state",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var prNum int
			if _, err := fmt.Sscanf(args[0], "%d", &prNum); err != nil || prNum <= 0 {
				return fmt.Errorf("invalid PR number %q", args[0])
			}
			if owner == "" || repo == "" {
				return fmt.Errorf("--owner and --repo are required")
			}
			// load() needs a trunk default only when no state exists; adopt will
			// overwrite local state with the fetched DAG anyway.
			if err := d.load("main"); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			ctx := cmd.Context()

			gh := forge.NewGitHub(owner, repo)
			fetched, err := gh.FetchStack(ctx, prNum)
			if err != nil {
				return err
			}

			if fo, err := d.repo.Fetch(ctx, remote); err != nil {
				return fmt.Errorf("git fetch %s: %s", remote, fo)
			}
			fmt.Fprintf(out, "Adopting stack from PR #%d (trunk %s):\n", prNum, fetched.Trunk)

			order, err := fetched.TopoOrder()
			if err != nil {
				return err
			}
			for _, b := range order {
				switch {
				case d.repo.LocalBranchExists(b.Name):
					fmt.Fprintf(out, "  have   %s\n", b.Name)
				default:
					if err := d.repo.TrackRemoteBranch(ctx, remote, b.Name); err != nil {
						return err
					}
					fmt.Fprintf(out, "  pulled %s (-> PR #%d)\n", b.Name, b.PR)
				}
			}

			if err := d.store.Save(fetched); err != nil {
				return err
			}
			fmt.Fprintf(out, "Adopted %d branches. Run `%s %s` to align them.\n",
				len(order), branding.B.Name, branding.B.CmdSync)
			return nil
		},
	}

	cmd.Flags().StringVar(&remote, "remote", "origin", "git remote to fetch from")
	cmd.Flags().StringVar(&owner, "owner", "", "forge repo owner/org")
	cmd.Flags().StringVar(&repo, "repo", "", "forge repo name")
	return cmd
}
