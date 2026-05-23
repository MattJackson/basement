// Package skin: BuiltInDefault returns the reference skin that ships
// with every basement deploy. Its palette mirrors the current
// frontend/src/index.css shadcn-derived token set EXACTLY so visual
// diff against the pre-cycle deploy is zero (the operator-locked
// acceptance gate for v1.13.0a per ADR-0008).
//
// When this file disagrees with index.css, index.css is the source of
// truth and this file is the bug — the FE consumer renders through
// CSS variables and basement-default's job is only to expose the
// current values through the typed Skin contract so the FE skin
// loader (v1.13.0b/c) doesn't need a special "if name = default,
// short-circuit" branch.

package skin

// BuiltInDefault returns the canonical basement-default skin. main.go
// registers this at boot before any user-uploaded skin loader runs.
//
// Values mirror frontend/src/index.css (the @theme block + the .dark
// override) one-for-one. Warning + Success + Info aren't present in
// the current index.css — they're added here as placeholders that
// pull a sensible HSL tone from the shadcn convention (success =
// green 140 60% 40%, warning = amber 38 92% 50%, info = blue 217 91%
// 60%). The FE doesn't consume these tokens in v1.13.0a (no surface
// references --basement-success today); v1.13.0c will wire them.
// Until then they're forward-compatible defaults so future skins
// can override them without a struct shape change.
func BuiltInDefault() Skin {
	return Skin{
		Name:        BuiltInDefaultName,
		DisplayName: "Basement Default",
		Version:     "1.0.0",
		ProductName: "Basement",
		Assets: Assets{
			// Logos + favicon left empty — basement-default deliberately
			// falls back to the FE's compiled-in Logo.tsx + the static
			// /favicon.ico in public/. Inlining the SVG bytes here
			// would duplicate the source of truth without buying
			// anything (the only consumer of the data URI path is the
			// upload story landing in v1.13.0b).
		},
		Palette: PaletteSet{
			Light: Palette{
				// hsl(240 5.9% 10%)
				Primary: "240 5.9% 10%",
				// hsl(0 0% 100%)
				Background: "0 0% 100%",
				// hsl(240 10% 3.9%)
				Foreground: "240 10% 3.9%",
				// hsl(240 4.8% 95.9%)
				Muted: "240 4.8% 95.9%",
				// hsl(240 4.8% 95.9%)
				Accent: "240 4.8% 95.9%",
				// hsl(0 84.2% 60.2%)
				Destructive: "0 84.2% 60.2%",
				// Placeholders — see file-level comment.
				Warning: "38 92% 50%",
				Success: "142 71% 45%",
				Info:    "217 91% 60%",
			},
			Dark: Palette{
				// hsl(0 0% 98%)
				Primary: "0 0% 98%",
				// hsl(240 10% 3.9%)
				Background: "240 10% 3.9%",
				// hsl(0 0% 98%)
				Foreground: "0 0% 98%",
				// hsl(240 3.7% 15.9%)
				Muted: "240 3.7% 15.9%",
				// hsl(240 3.7% 15.9%)
				Accent: "240 3.7% 15.9%",
				// hsl(0 62.8% 50%)
				Destructive: "0 62.8% 50%",
				// Placeholders — see file-level comment.
				Warning: "38 92% 55%",
				Success: "142 71% 50%",
				Info:    "217 91% 65%",
			},
		},
		Typography: Typography{
			// Mirrors the html/body font-family stack in index.css.
			Sans: `ui-sans-serif, system-ui, sans-serif, "Apple Color Emoji", ` +
				`"Segoe UI Emoji", "Segoe UI Symbol", "Noto Color Emoji"`,
			Mono: `ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, ` +
				`"Liberation Mono", monospace`,
		},
		// Mirrors --radius: 0.5rem in index.css.
		BorderRadius: "0.5rem",
		Density:      DensityComfortable,
		// Footer + LoginHero left nil — basement-default renders the
		// current shells unchanged, which carry no operator footer
		// and no pre-auth marketing surface.
	}
}
