// Package main — time.go is a one-helper file: convert an
// "expires-in N seconds" relative TTL (the LLM-friendly shape we
// expose in tools.go) to an absolute RFC3339 timestamp (the wire
// shape user_shares.go expects). Lives in its own file so the
// override seam for tests stays narrow.

package main

import "time"

// nowFunc is the time source. Overridable in tests so the
// expires-at arithmetic produces deterministic strings rather
// than wall-clock-dependent ones.
var nowFunc = time.Now

// rfcTimeFromOffset returns an RFC3339 timestamp `seconds` from
// now, in UTC. The server's UserShareCreateRequest accepts an
// expiresAt *time.Time which json-decodes any RFC3339 value, so
// the string round-trips cleanly through the wire envelope.
func rfcTimeFromOffset(seconds int) string {
	return nowFunc().Add(time.Duration(seconds) * time.Second).UTC().Format(time.RFC3339)
}
