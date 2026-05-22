// Package webdav: clock indirection. Centralised so tests that need
// to assert SA expiry behaviour can override time without sprinkling
// `time.Now()` shadows across every method.

package webdav

import "time"

// nowFunc returns the current time in UTC. Variable so tests can stub
// it; production reads the wall clock.
var nowFunc = func() time.Time { return time.Now().UTC() }
