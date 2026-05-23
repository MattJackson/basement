// Package gateway: registry tests covering Register / Get / All /
// Enabled / StartAll / StopAll. Doctrine per the cycle: duplicate
// Name() errors loud, All() is sorted alphabetically, Enabled()
// filters by org caps and by Implemented().

package gateway

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

// fakeGateway is a configurable Gateway stub for registry-level tests.
// All accessors are direct field reads; Start/Stop record their call
// counts.
type fakeGateway struct {
	name        string
	display     string
	desc        string
	caps        Capabilities
	status      Status
	implemented bool
	listenAddr  string
	handler     http.Handler

	startCalls int
	stopCalls  int
	startErr   error
	stopErr    error
}

func (f *fakeGateway) Name() string                  { return f.name }
func (f *fakeGateway) DisplayName() string           { return f.display }
func (f *fakeGateway) Description() string           { return f.desc }
func (f *fakeGateway) Capabilities() Capabilities    { return f.caps }
func (f *fakeGateway) Status() Status                { return f.status }
func (f *fakeGateway) Implemented() bool             { return f.implemented }
func (f *fakeGateway) Start(_ context.Context) error { f.startCalls++; return f.startErr }
func (f *fakeGateway) Stop(_ context.Context) error  { f.stopCalls++; return f.stopErr }
func (f *fakeGateway) HTTPHandler() http.Handler     { return f.handler }
func (f *fakeGateway) ListenAddress() string         { return f.listenAddr }

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := New()
	g := &fakeGateway{name: "webdav"}
	if err := r.Register(g); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got, ok := r.Get("webdav")
	if !ok {
		t.Fatal("Get(webdav): want found")
	}
	if got != Gateway(g) {
		t.Errorf("Get returned a different value than was registered")
	}

	if _, ok := r.Get("nonexistent"); ok {
		t.Errorf("Get(nonexistent): want !ok")
	}
}

func TestRegistry_DuplicateName_Errors(t *testing.T) {
	r := New()
	if err := r.Register(&fakeGateway{name: "webdav"}); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	err := r.Register(&fakeGateway{name: "webdav"})
	if err == nil {
		t.Fatal("second Register with same name: want error")
	}
	if !errors.Is(err, ErrDuplicateGateway) {
		t.Errorf("want errors.Is(err, ErrDuplicateGateway), got %v", err)
	}
}

func TestRegistry_NilOrEmptyName_Errors(t *testing.T) {
	r := New()
	if err := r.Register(nil); err == nil {
		t.Errorf("Register(nil): want error")
	}
	if err := r.Register(&fakeGateway{name: ""}); err == nil {
		t.Errorf("Register(empty-name): want error")
	}
}

func TestRegistry_All_Sorted(t *testing.T) {
	r := New()
	// Register out of order so a non-sorting impl would fail.
	for _, name := range []string{"webdav", "smb", "ftp", "s3", "nfs"} {
		if err := r.Register(&fakeGateway{name: name}); err != nil {
			t.Fatalf("Register %s: %v", name, err)
		}
	}
	all := r.All()
	want := []string{"ftp", "nfs", "s3", "smb", "webdav"}
	if len(all) != len(want) {
		t.Fatalf("All: got %d gateways, want %d", len(all), len(want))
	}
	for i, g := range all {
		if g.Name() != want[i] {
			t.Errorf("All[%d]: got %s want %s", i, g.Name(), want[i])
		}
	}
}

// stubCaps implements OrgCaps via a fixed map.
type stubCaps map[string]bool

func (s stubCaps) IsEnabled(name string) bool { return s[name] }

func TestRegistry_Enabled_FiltersByCapsAndImplemented(t *testing.T) {
	r := New()
	if err := r.Register(&fakeGateway{name: "webdav", implemented: true}); err != nil {
		t.Fatalf("Register webdav: %v", err)
	}
	// SMB is a stub: even if caps says "enabled", Enabled() must skip it.
	if err := r.Register(&fakeGateway{name: "smb", implemented: false}); err != nil {
		t.Fatalf("Register smb: %v", err)
	}
	// FTP: implemented but disabled in caps.
	if err := r.Register(&fakeGateway{name: "ftp", implemented: true}); err != nil {
		t.Fatalf("Register ftp: %v", err)
	}

	caps := stubCaps{
		"webdav": true,
		"smb":    true, // ignored — stub
		"ftp":    false,
	}
	enabled := r.Enabled(caps)
	if len(enabled) != 1 {
		t.Fatalf("Enabled: got %d gateways, want 1: %v", len(enabled), enabled)
	}
	if enabled[0].Name() != "webdav" {
		t.Errorf("Enabled[0]: got %s want webdav", enabled[0].Name())
	}
}

func TestRegistry_StartAll_CallsEveryGateway(t *testing.T) {
	r := New()
	a := &fakeGateway{name: "a", implemented: true}
	b := &fakeGateway{name: "b", implemented: true}
	stub := &fakeGateway{name: "stub", implemented: false}
	_ = r.Register(a)
	_ = r.Register(b)
	_ = r.Register(stub)

	if err := r.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	if a.startCalls != 1 || b.startCalls != 1 || stub.startCalls != 1 {
		t.Errorf("StartAll: want each gateway started exactly once; got a=%d b=%d stub=%d",
			a.startCalls, b.startCalls, stub.startCalls)
	}
}

func TestRegistry_StartAll_ContinuesPastError(t *testing.T) {
	r := New()
	a := &fakeGateway{name: "a", implemented: true, startErr: errors.New("a boom")}
	b := &fakeGateway{name: "b", implemented: true}
	_ = r.Register(a)
	_ = r.Register(b)

	err := r.StartAll(context.Background())
	if err == nil {
		t.Fatal("StartAll: want error from a's failure")
	}
	// b must still have started — StartAll continues past the failure.
	if b.startCalls != 1 {
		t.Errorf("StartAll: want b started despite a's error; got b.startCalls=%d", b.startCalls)
	}
}

func TestRegistry_StopAll_CollectsErrors(t *testing.T) {
	r := New()
	a := &fakeGateway{name: "a", stopErr: errors.New("a stop boom")}
	b := &fakeGateway{name: "b", stopErr: errors.New("b stop boom")}
	c := &fakeGateway{name: "c"}
	_ = r.Register(a)
	_ = r.Register(b)
	_ = r.Register(c)

	err := r.StopAll(context.Background())
	if err == nil {
		t.Fatal("StopAll: want a combined error")
	}
	// Both stop errors should be in the joined error.
	if !errorContains(err, "a stop boom") || !errorContains(err, "b stop boom") {
		t.Errorf("StopAll: want both stop errors surfaced; got %v", err)
	}
	if c.stopCalls != 1 {
		t.Errorf("StopAll: want c stopped despite earlier errors")
	}
}

func errorContains(err error, substr string) bool {
	if err == nil {
		return false
	}
	return contains(err.Error(), substr)
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestOrgCapsFunc(t *testing.T) {
	caps := OrgCapsFunc(func(name string) bool { return name == "webdav" })
	if !caps.IsEnabled("webdav") {
		t.Errorf("OrgCapsFunc: want webdav enabled")
	}
	if caps.IsEnabled("smb") {
		t.Errorf("OrgCapsFunc: want smb disabled")
	}
}
