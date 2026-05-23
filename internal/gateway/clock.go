// Package gateway: clock indirection so tests that need to assert
// SA expiry behaviour can override time without sprinkling
// `time.Now()` shadows across the file. Mirrors the same pattern in
// internal/gateway/webdav (and the legacy internal/webdav).

package gateway

import "time"

// nowFunc returns the current wall clock in UTC. Tests assign to it
// to drive deterministic expiry checks; production reads the real
// clock.
var nowFunc = func() time.Time { return time.Now().UTC() }
