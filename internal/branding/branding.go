// Package branding is the single source of truth for everything user-facing
// and protocol-facing that we expect to rebrand later: the binary name, the
// subcommand verbs, the env-var prefix, and the hidden-comment markers used to
// sync stack state through PR descriptions.
//
// RULE: nothing outside this package should hard-code the tool name, a command
// verb, an env-var key, or a comment marker. Import these instead. When the
// brand is finalized, this file is the only thing that changes.
package branding

import (
	"fmt"
	"strings"
)

// B holds the active brand. It is a var (not consts) so that, later, we can
// load overrides from config without touching call sites.
var B = Brand{
	// Identity ---------------------------------------------------------------
	Name:      "stack",                     // invoked binary name, e.g. `stack sync`
	Title:     "Stack",                     // human-friendly name in help text
	Tagline:   "Client-side stacked PRs.",  // root command short description
	EnvPrefix: "STACK",                     // env vars: STACK_TOKEN, STACK_DEBUG, ...

	// Subcommand verbs (rename freely) --------------------------------------
	CmdStart:    "start",
	CmdSync:     "sync",
	CmdSubmit:   "submit",
	CmdAdopt:    "adopt",
	CmdReparent: "reparent",

	// Local on-disk state ---------------------------------------------------
	// Stored under the repo's .git dir so it never pollutes the worktree.
	StateDir:  "stack",        // -> .git/stack/
	StateFile: "stack.json",   // -> .git/stack/stack.json

	// Hidden-comment protocol ------------------------------------------------
	// State is round-tripped through PR descriptions as an HTML comment so it is
	// invisible in rendered markdown but recoverable by any teammate's CLI.
	// Layout:
	//   <!-- stack-state:v1
	//   { ...json... }
	//   stack-state:end -->
	StateMarkerPrefix: "stack-state",
	StateMarkerEnd:    "stack-state:end",
	StateSchemaVer:    "v1",

	// Marker that identifies our stack-navigation comment so we update it in
	// place instead of posting duplicates.
	NavMarker: "stack-nav",
}

// Brand is the configurable surface. Keep it flat and boring on purpose.
type Brand struct {
	Name, Title, Tagline, EnvPrefix string

	CmdStart, CmdSync, CmdSubmit, CmdAdopt, CmdReparent string

	StateDir, StateFile string

	StateMarkerPrefix, StateMarkerEnd, StateSchemaVer string

	NavMarker string
}

// Env builds a fully-qualified env-var name, e.g. Env("TOKEN") -> "STACK_TOKEN".
func (b Brand) Env(key string) string {
	return b.EnvPrefix + "_" + strings.ToUpper(key)
}

// OpenMarker is the opening line of the hidden state block, including schema
// version, e.g. "stack-state:v1".
func (b Brand) OpenMarker() string {
	return fmt.Sprintf("%s:%s", b.StateMarkerPrefix, b.StateSchemaVer)
}

// CloseMarker is the closing sentinel line, e.g. "stack-state:end".
func (b Brand) CloseMarker() string {
	return b.StateMarkerEnd
}
