// Package policy holds the broker's rules as pure, deterministic functions:
// team-spec defaults, validation and applying, the grid, accepting a message,
// the hop budget and review rounds, spawning, worktree placement and every
// string the broker shows an agent.
//
// Nothing here touches a database, the filesystem, a clock or a random source.
// Inputs arrive as values; facts about the host arrive through [Host]; time and
// seals are passed in. The service layer loads a snapshot inside a
// transaction, calls these functions, and writes what they decide. Each
// decision therefore carries everything it needs to write: new states, counter
// values and the events whose data policy can build ([Event]).
//
// Events whose data is a record snapshot (message.created, member.added,
// member.removed, team.applied, delivery.changed) are not built here: the
// store takes those snapshots after it writes. Each decision's documentation
// says which of them follow from it, and in what order.
//
// The rules are docs/protocol.md's, with PR #3's amendment to review rounds:
// a round is a reviewer's message to its writer.
package policy
