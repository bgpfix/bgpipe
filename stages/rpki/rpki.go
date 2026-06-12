// Package rpki implements RPKI validation stages: rov and aspa.
//
// Both stages validate UPDATE messages against the shared RPKI cache,
// which is maintained by the bgpipe core (see the global --rpki flags).
package rpki

// action for invalid prefixes/paths
const (
	act_withdraw = iota // move invalid prefixes to withdrawn
	act_drop            // drop entire UPDATE message
	act_filter          // remove invalid prefixes silently (rov only)
	act_split           // split invalid prefixes to separate UPDATE (rov only)
	act_keep            // keep unchanged (tag only)
)
