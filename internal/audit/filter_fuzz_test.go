package audit

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// FuzzMatchFilter exercises the QueryFilter matching path with
// arbitrary string inputs. Property: matchFilter must NEVER panic
// regardless of input, even on UTF-8 garbage, embedded NULs, or
// pathologically long strings.
//
// This is the starter pattern for the basement fuzz suite (cycle
// v1.11.0.12). To add more fuzz targets:
//
//  1. Pick a pure function that takes untrusted-ish input (parsing,
//     filtering, decoding).
//  2. Seed with realistic happy-path AND obvious adversarial inputs
//     (empty, malformed, oversized, unicode edge cases).
//  3. Assert: never panics. If the function has an error contract,
//     also assert: returns a typed error rather than a runtime
//     surprise.
//  4. Run locally with:
//     `go test -fuzz=FuzzMatchFilter -fuzztime=30s ./internal/audit/...`
//  5. Add the target name to the Makefile fuzz-* target list.
//
// CI does not run fuzz tests (they're time-boxed and best run
// interactively). Crashes from fuzzing get checked in as a `corpus/`
// regression alongside the fix.
func FuzzMatchFilter(f *testing.F) {
	// Seed corpus — happy path, empty fields, adversarial inputs.
	f.Add("matthew", "bucket:create", "bucket:abc:foo", "success")
	f.Add("", "", "", "")
	f.Add("matthew", "", "", "")
	f.Add("", "auth:login", "", "")
	f.Add("", "", "cluster:", "")
	f.Add("", "", "", "failure")
	// Pathological seeds — embedded controls, case, unicode.
	f.Add("matthew\x00admin", "BUCKET:CREATE", "bucket:abc:\x00\x01", "success")
	f.Add("user-é-name", "auth:login", "bucket:obj", "success")
	f.Add(strings.Repeat("x", 4096), "a", "b", "success")

	f.Fuzz(func(t *testing.T, actor, action, resource, result string) {
		// Build a representative Event from the fuzzed strings — we
		// want both branches of matchFilter exercised (the actor/result
		// equality path and the action/resource substring path).
		e := Event{
			Time:     time.Now().UTC(),
			Actor:    actor,
			Action:   action,
			Resource: resource,
			Result:   result,
		}

		// Filter combinations: each fuzzed input is used both as the
		// event field AND as the filter field, so we exercise both
		// match-true and match-against-different-event paths.
		filters := []QueryFilter{
			{},
			{Actor: actor},
			{Action: action},
			{Resource: resource},
			{Result: result},
			{Actor: actor, Action: action, Resource: resource, Result: result},
			{Actor: "different", Action: "different", Resource: "different", Result: "different"},
		}
		for _, qf := range filters {
			_ = matchFilter(e, qf) // contract: must not panic.
		}

		// Bonus: the Event must round-trip through JSON without panic
		// — this is the on-disk format the readDayFile path consumes.
		// Marshal failures here are unexpected (no time.Time edge case
		// is currently reachable from a normal Log() call), but the
		// fuzzer is the right place to discover any.
		if _, err := json.Marshal(e); err != nil {
			t.Fatalf("event JSON marshal: %v (event=%#v)", err, e)
		}
	})
}

// FuzzReadDayFileLine asserts that an arbitrary single line is either
// parsed as a valid Event JSON or rejected via the slog.Warn path
// inside readDayFile (i.e. never panics). We test the parsing step
// directly to keep the test hermetic (no tempdir, no fs).
func FuzzReadDayFileLine(f *testing.F) {
	// Seed with realistic event lines + adversarial JSON.
	f.Add(`{"time":"2026-05-22T14:30:00Z","actor":"matthew","action":"bucket:create","resource":"bucket:abc:foo","result":"success"}`)
	f.Add(`{}`)
	f.Add(``)
	f.Add(`{`)
	f.Add(`null`)
	f.Add(`{"time":"not-a-timestamp","actor":""}`)
	f.Add(`{"actor":" "}`)
	// Oversized detail payload — built at runtime to avoid embedding
	// NUL bytes in source (Go raw strings can't contain them).
	bigDetail := strings.Repeat("a", 4096)
	f.Add(`{"time":"2026-05-22T14:30:00Z","detail":"` + bigDetail + `"}`)

	f.Fuzz(func(t *testing.T, line string) {
		var e Event
		// Unmarshal must not panic. It MAY return an error (which is
		// the readDayFile slog.Warn skip path); that's fine.
		_ = json.Unmarshal([]byte(line), &e)
	})
}
