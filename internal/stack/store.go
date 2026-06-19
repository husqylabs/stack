package stack

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/husqylabs/stack/internal/branding"
)

// Store persists a Stack under the repo's .git directory so it never shows up in
// the worktree or in `git status`. The hidden PR comment is the *shared* source
// of truth across teammates; this local file is just a fast, offline cache.
type Store struct {
	gitDir string // absolute path to the .git directory
}

// NewStore points at <gitDir>/<branding.StateDir>/<branding.StateFile>.
func NewStore(gitDir string) *Store { return &Store{gitDir: gitDir} }

func (st *Store) path() string {
	return filepath.Join(st.gitDir, branding.B.StateDir, branding.B.StateFile)
}

// Load reads the local stack. Returns (nil, os.ErrNotExist) if nothing is saved
// yet so callers can distinguish "no stack" from a real I/O error.
func (st *Store) Load() (*Stack, error) {
	data, err := os.ReadFile(st.path())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, os.ErrNotExist
		}
		return nil, err
	}
	return Decode(data)
}

// Save atomically writes the stack (write-temp-then-rename).
func (st *Store) Save(s *Stack) error {
	dir := filepath.Join(st.gitDir, branding.B.StateDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := s.Encode()
	if err != nil {
		return err
	}
	tmp := st.path() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, st.path())
}

func (st *Store) pendingPath() string {
	return filepath.Join(st.gitDir, branding.B.StateDir, "pending.json")
}

// LoadPending returns the in-flight rebase journal, or (nil, os.ErrNotExist) if
// none is recorded (the common, healthy case).
func (st *Store) LoadPending() (*Pending, error) {
	data, err := os.ReadFile(st.pendingPath())
	if err != nil {
		return nil, err // includes os.ErrNotExist
	}
	var p Pending
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// SavePending atomically writes the journal before a rebase begins.
func (st *Store) SavePending(p *Pending) error {
	dir := filepath.Join(st.gitDir, branding.B.StateDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	tmp := st.pendingPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, st.pendingPath())
}

// ClearPending removes the journal after a clean finish; absent journal is fine.
func (st *Store) ClearPending() error {
	err := os.Remove(st.pendingPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
