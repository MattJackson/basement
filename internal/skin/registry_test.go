// Package skin: registry tests covering Register / Get / All plus
// the BuiltInDefault contract that v1.13.0a's "visual diff = zero"
// acceptance gate depends on.

package skin

import (
	"errors"
	"testing"
)

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := New()
	s := Skin{Name: "basement-default", DisplayName: "Default"}
	if err := r.Register(s); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, ok := r.Get("basement-default")
	if !ok {
		t.Fatal("Get(basement-default): want found")
	}
	if got.Name != s.Name || got.DisplayName != s.DisplayName {
		t.Errorf("Get returned a different value than was registered: %+v", got)
	}

	if _, ok := r.Get("nonexistent"); ok {
		t.Errorf("Get(nonexistent): want !ok")
	}
}

func TestRegistry_DuplicateName_Errors(t *testing.T) {
	r := New()
	if err := r.Register(Skin{Name: "acme"}); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	err := r.Register(Skin{Name: "acme", DisplayName: "Different but same name"})
	if err == nil {
		t.Fatal("second Register with same name: want error")
	}
	if !errors.Is(err, ErrDuplicateSkin) {
		t.Errorf("want errors.Is(err, ErrDuplicateSkin), got %v", err)
	}
}

func TestRegistry_EmptyName_Errors(t *testing.T) {
	r := New()
	if err := r.Register(Skin{Name: ""}); err == nil {
		t.Errorf("Register(empty-name): want error")
	}
}

func TestRegistry_All_Sorted(t *testing.T) {
	r := New()
	// Register out of order so a non-sorting impl would fail.
	for _, name := range []string{"zeta", "acme", "basement-default", "mu", "alpha"} {
		if err := r.Register(Skin{Name: name}); err != nil {
			t.Fatalf("Register %s: %v", name, err)
		}
	}
	all := r.All()
	want := []string{"acme", "alpha", "basement-default", "mu", "zeta"}
	if len(all) != len(want) {
		t.Fatalf("All: got %d skins, want %d", len(all), len(want))
	}
	for i, s := range all {
		if s.Name != want[i] {
			t.Errorf("All[%d]: got %s want %s", i, s.Name, want[i])
		}
	}
}

func TestRegistry_All_EmptyRegistry(t *testing.T) {
	r := New()
	all := r.All()
	if len(all) != 0 {
		t.Errorf("All on empty registry: got %d skins, want 0", len(all))
	}
	// Must return non-nil so JSON encoding renders [] not null.
	if all == nil {
		t.Errorf("All on empty registry: returned nil; want empty slice (JSON shape)")
	}
}

// Concurrent Register / Get must not race when -race is on. This is
// the same shape internal/gateway/registry_test.go uses for the
// mirrored guarantee.
func TestRegistry_ConcurrentAccess(t *testing.T) {
	r := New()
	if err := r.Register(BuiltInDefault()); err != nil {
		t.Fatalf("seed Register: %v", err)
	}

	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			_ = r.All()
			_, _ = r.Get(BuiltInDefaultName)
		}
		close(done)
	}()

	for i := 0; i < 1000; i++ {
		_, _ = r.Get(BuiltInDefaultName)
	}
	<-done
}

// BuiltInDefault is the v1.13.0a acceptance gate: every palette token
// is non-empty, the name matches the constant, and the densities are
// valid. A regression here would break the FE's render of an
// "empty" basement-default and ship a blank-canvas chrome.
func TestBuiltInDefault_PopulatedAndValid(t *testing.T) {
	s := BuiltInDefault()
	if s.Name != BuiltInDefaultName {
		t.Errorf("Name: got %q, want %q", s.Name, BuiltInDefaultName)
	}
	if s.DisplayName == "" {
		t.Errorf("DisplayName: empty")
	}
	if s.ProductName == "" {
		t.Errorf("ProductName: empty (FE would render no product label)")
	}
	if !s.Density.IsValid() {
		t.Errorf("Density: %q is not a valid density literal", s.Density)
	}
	if s.BorderRadius == "" {
		t.Errorf("BorderRadius: empty")
	}
	if s.Typography.Sans == "" {
		t.Errorf("Typography.Sans: empty")
	}
	if s.Typography.Mono == "" {
		t.Errorf("Typography.Mono: empty")
	}

	checkPalette := func(label string, p Palette) {
		// Spot-check every semantic token — a missing entry would
		// silently break the FE's CSS variable injection.
		tokens := map[string]string{
			"primary":     p.Primary,
			"background":  p.Background,
			"foreground":  p.Foreground,
			"muted":       p.Muted,
			"accent":      p.Accent,
			"destructive": p.Destructive,
			"warning":     p.Warning,
			"success":     p.Success,
			"info":        p.Info,
		}
		for name, val := range tokens {
			if val == "" {
				t.Errorf("Palette.%s.%s: empty", label, name)
			}
		}
	}
	checkPalette("Light", s.Palette.Light)
	checkPalette("Dark", s.Palette.Dark)
}

// The v1.13.0a "visual diff = zero" gate hinges on basement-default's
// palette mirroring the current index.css values one-for-one. Pin
// each token so a future palette tweak that forgets to update
// index.css (or vice-versa) trips this test before it ships.
//
// HSL values are taken from frontend/src/index.css's @theme block
// (light) and .dark override (dark). If index.css moves, update this
// test in the same commit.
func TestBuiltInDefault_MirrorsIndexCSS(t *testing.T) {
	s := BuiltInDefault()

	wantLight := map[string]string{
		"Primary":     "240 5.9% 10%",
		"Background":  "0 0% 100%",
		"Foreground":  "240 10% 3.9%",
		"Muted":       "240 4.8% 95.9%",
		"Accent":      "240 4.8% 95.9%",
		"Destructive": "0 84.2% 60.2%",
	}
	wantDark := map[string]string{
		"Primary":     "0 0% 98%",
		"Background":  "240 10% 3.9%",
		"Foreground":  "0 0% 98%",
		"Muted":       "240 3.7% 15.9%",
		"Accent":      "240 3.7% 15.9%",
		"Destructive": "0 62.8% 50%",
	}
	got := map[string]map[string]string{
		"light": {
			"Primary":     s.Palette.Light.Primary,
			"Background":  s.Palette.Light.Background,
			"Foreground":  s.Palette.Light.Foreground,
			"Muted":       s.Palette.Light.Muted,
			"Accent":      s.Palette.Light.Accent,
			"Destructive": s.Palette.Light.Destructive,
		},
		"dark": {
			"Primary":     s.Palette.Dark.Primary,
			"Background":  s.Palette.Dark.Background,
			"Foreground":  s.Palette.Dark.Foreground,
			"Muted":       s.Palette.Dark.Muted,
			"Accent":      s.Palette.Dark.Accent,
			"Destructive": s.Palette.Dark.Destructive,
		},
	}
	for token, want := range wantLight {
		if got["light"][token] != want {
			t.Errorf("light.%s: got %q, want %q (index.css drift)",
				token, got["light"][token], want)
		}
	}
	for token, want := range wantDark {
		if got["dark"][token] != want {
			t.Errorf("dark.%s: got %q, want %q (index.css drift)",
				token, got["dark"][token], want)
		}
	}
	// BorderRadius matches the --radius literal in index.css.
	if s.BorderRadius != "0.5rem" {
		t.Errorf("BorderRadius: got %q, want %q (index.css --radius)",
			s.BorderRadius, "0.5rem")
	}
}

func TestDensity_IsValid(t *testing.T) {
	cases := []struct {
		in   Density
		want bool
	}{
		{DensityCompact, true},
		{DensityComfortable, true},
		{DensitySpacious, true},
		{"", false},
		{"medium", false},
		{"COMPACT", false}, // case-sensitive
	}
	for _, c := range cases {
		if got := c.in.IsValid(); got != c.want {
			t.Errorf("Density(%q).IsValid() = %v, want %v", c.in, got, c.want)
		}
	}
}
