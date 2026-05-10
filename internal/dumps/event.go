// Package dumps receives, buffers, and fans out PHP `dump()`/`dd()` events
// captured by the lerd dump bridge (auto_prepend_file). The wire format is
// newline-delimited JSON over a loopback TCP socket. See docs/features/dumps.md.
package dumps

import "encoding/json"

// ProtocolVersion is the wire-format version this package understands.
// Events with a different `v` are dropped.
const ProtocolVersion = 1

// KindDump is the only event kind PR1 emits. Reserved values like
// "query"/"job" will be added by future PRs without bumping ProtocolVersion.
const KindDump = "dump"

// Source identifies the file:line that produced a dump.
type Source struct {
	File string `json:"file"`
	Line int    `json:"line"`
}

// Context describes where a dump came from. Type is "fpm" (web request) or
// "cli" (artisan, tinker, queue worker). Empty fields are omitted on the wire.
type Context struct {
	Type    string `json:"type"`
	Site    string `json:"site,omitempty"`
	Domain  string `json:"domain,omitempty"`
	Request string `json:"request,omitempty"`
	PID     int    `json:"pid,omitempty"`
}

// Event is one dump payload. Tree is opaque JSON (the bridge sends a
// pre-walked tree from VarCloner in a future PR; PR1 ships only `text`).
type Event struct {
	V     int             `json:"v"`
	ID    string          `json:"id"`
	TS    string          `json:"ts"`
	Kind  string          `json:"kind"`
	Ctx   Context         `json:"ctx"`
	Src   Source          `json:"src"`
	Label string          `json:"label,omitempty"`
	Text  string          `json:"text,omitempty"`
	Tree  json.RawMessage `json:"tree,omitempty"`
	Trunc bool            `json:"trunc,omitempty"`
}

// Valid reports whether an event passes the minimum schema check applied by
// the listener before it is appended to the ring.
func (e Event) Valid() bool {
	return e.V == ProtocolVersion && e.ID != "" && e.Kind != ""
}
