package stack

// Pending is a write-ahead journal for an in-flight rebase (from sync or
// reparent). It is written BEFORE git is touched and cleared only after the
// operation fully succeeds. If the process dies or the user hits a conflict, the
// journal survives so the next command can reconcile our metadata against git's
// actual state instead of trusting a half-applied intent.
type Pending struct {
	Op        string `json:"op"`                   // "sync" | "reparent"
	Branch    string `json:"branch"`               // branch being rebased
	OldBase   string `json:"old_base"`             // ParentCommit before the rebase (rollback target)
	NewBase   string `json:"new_base"`             // base we rebased onto (forward target)
	NewParent string `json:"new_parent,omitempty"` // reparent only; empty means parent unchanged
}
