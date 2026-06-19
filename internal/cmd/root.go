// Package cmd wires the cobra command tree. Every user-visible string (binary
// name, verbs) comes from branding so a rebrand never touches this file.
package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/husqylabs/stack/internal/branding"
	"github.com/husqylabs/stack/internal/git"
	"github.com/husqylabs/stack/internal/stack"
	"github.com/spf13/cobra"
)

// deps is the small shared context passed to each subcommand: an open repo and a
// loaded (or fresh) stack. Building it lazily in PersistentPreRunE keeps the
// commands themselves focused on their own logic.
type deps struct {
	repo  *git.Repo
	store *stack.Store
	stack *stack.Stack
}

// NewRoot builds the full command tree.
func NewRoot() *cobra.Command {
	d := &deps{}

	root := &cobra.Command{
		Use:           branding.B.Name,
		Short:         branding.B.Tagline,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(
		newStartCmd(d),
		newSyncCmd(d),
		newSubmitCmd(d),
		newAdoptCmd(d),
		newReparentCmd(d),
	)
	return root
}

// loadRepo opens the repo at cwd and loads local stack state (creating an empty
// stack rooted on the default trunk if none exists yet). Commands call this in
// their RunE rather than a global pre-run so `--help` works without a repo.
func (d *deps) load(trunk string) error {
	if d.repo == nil {
		r, err := git.Open(".")
		if err != nil {
			return err
		}
		d.repo = r
		gitDir, err := r.GitDir()
		if err != nil {
			return err
		}
		d.store = stack.NewStore(gitDir)
	}
	s, err := d.store.Load()
	if err != nil {
		// os.ErrNotExist -> first run; start an empty stack.
		s = stack.New(trunk)
	}
	d.stack = s

	// Heal any interrupted rebase before running the command's own logic.
	return d.reconcile()
}

// reconcile makes our metadata consistent with git after an interrupted rebase
// (conflict, crash, or abort). It is a no-op in the healthy case (no journal).
//
// Ground-truth recovery:
//   - A rebase still in progress -> refuse to proceed; the user must finish it.
//   - Otherwise compare the branch tip to the journaled bases:
//   - on the NEW base -> the rebase landed; commit the intent forward.
//   - on the OLD base -> it was aborted; discard the intent (rollback).
//   - on neither      -> ambiguous; bail out rather than guess.
//
// NEW is checked before OLD on purpose: after a successful sync rebase the old
// base is an ancestor of the new base (parent moved forward), so both would
// match — and "forward" is the correct interpretation.
func (d *deps) reconcile() error {
	p, err := d.store.LoadPending()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil // healthy: nothing in flight
		}
		return err
	}

	if d.repo.RebaseInProgress() {
		return fmt.Errorf("a paused rebase from a previous %s %s of %q is still in progress; finish it with `git rebase --continue` or `git rebase --abort`, then re-run",
			branding.B.Name, p.Op, p.Branch)
	}

	onNew, err := d.repo.IsAncestor(p.NewBase, p.Branch)
	if err != nil {
		return fmt.Errorf("reconcile %q: %w", p.Branch, err)
	}
	onOld, err := d.repo.IsAncestor(p.OldBase, p.Branch)
	if err != nil {
		return fmt.Errorf("reconcile %q: %w", p.Branch, err)
	}

	b := d.stack.Branches[p.Branch]
	switch {
	case onNew:
		if b != nil {
			if p.NewParent != "" {
				b.Parent = p.NewParent
			}
			b.ParentCommit = p.NewBase
		}
		if err := d.store.Save(d.stack); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "recovered: %q landed on %s; state updated.\n", p.Branch, short(p.NewBase))
	case onOld:
		fmt.Fprintf(os.Stderr, "recovered: %s of %q was aborted; left on previous base.\n", p.Op, p.Branch)
	default:
		return fmt.Errorf("cannot auto-recover %q: its tip is on neither the recorded old nor new base; reconcile manually, then delete %s",
			p.Branch, branding.B.StateDir+"/pending.json")
	}
	return d.store.ClearPending()
}
