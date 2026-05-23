// Package skin: Built-in skins registered at boot (v1.13.0b).
//
// These skins provide distinctive visual identities for basement deployments:
//   - basement-high-contrast: WCAG AAA compliant high contrast mode
//   - basement-minimal: Ultra-clean, minimalist aesthetic
//   - basement-95: Retro Windows 95 with bevels and gray palette
//   - basement-terminal: TTY/terminal theme with green-on-black monospace

package skin

import (
	"encoding/json"
)

// BuiltInHighContrast returns the high contrast skin for accessibility.
// Meets WCAG AAA requirements with extreme color contrasts.
func BuiltInHighContrast() Skin {
	return Skin{
		Name:        "basement-high-contrast",
		DisplayName: "High Contrast",
		Version:     "1.0.0",
		ProductName: "Basement",
		Palette: PaletteSet{
			Light: Palette{
				Primary:     "210 100% 30%",
				Background:  "0 0% 100%",
				Foreground:  "0 0% 0%",
				Muted:       "0 0% 95%",
				Accent:      "280 100% 60%",
				Destructive: "0 100% 40%",
				Warning:     "45 100% 50%",
				Success:     "142 100% 30%",
				Info:        "200 100% 40%",
			},
			Dark: Palette{
				Primary:     "210 100% 70%",
				Background:  "0 0% 0%",
				Foreground:  "0 0% 100%",
				Muted:       "0 0% 20%",
				Accent:      "280 100% 75%",
				Destructive: "0 100% 60%",
				Warning:     "45 100% 70%",
				Success:     "142 100% 50%",
				Info:        "200 100% 60%",
			},
		},
		Typography: Typography{
			Sans: `ui-sans-serif, system-ui, sans-serif`,
			Mono: `ui-monospace, SFMono-Regular, monospace`,
		},
		BorderRadius: "0", // Square for stark contrast
		Density:      DensityComfortable,
	}
}

// BuiltInMinimal returns the ultra-clean minimal skin.
// Stripped back to essentials with maximum whitespace.
func BuiltInMinimal() Skin {
	return Skin{
		Name:        "basement-minimal",
		DisplayName: "Minimal",
		Version:     "1.0.0",
		ProductName: "Basement",
		Palette: PaletteSet{
			Light: Palette{
				Primary:     "210 40% 35%",
				Background:  "0 0% 100%",
				Foreground:  "210 20% 20%",
				Muted:       "210 10% 96%",
				Accent:      "210 30% 85%",
				Destructive: "0 40% 60%",
				Warning:     "45 50% 70%",
				Success:     "142 40% 55%",
				Info:        "200 30% 65%",
			},
			Dark: Palette{
				Primary:     "210 40% 75%",
				Background:  "210 30% 8%",
				Foreground:  "210 20% 90%",
				Muted:       "210 30% 15%",
				Accent:      "210 30% 20%",
				Destructive: "0 40% 70%",
				Warning:     "45 50% 60%",
				Success:     "142 40% 60%",
				Info:        "200 30% 70%",
			},
		},
		Typography: Typography{
			Sans: `ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont`,
			Mono: `ui-monospace, SFMono-Regular, monospace`,
		},
		BorderRadius: "0.25rem", // Subtle rounding only
		Density:      DensitySpacious,
	}
}

// BuiltIn95 returns the retro Windows 95 skin.
// FUN! Iconic #c0c0c0 gray palette with beveled borders and pixel-perfect vibes.
func BuiltIn95() Skin {
	return Skin{
		Name:        "basement-95",
		DisplayName: "Windows 95",
		Version:     "1.0.0",
		ProductName: "Basement 95",
		Palette: PaletteSet{
			Light: Palette{
				Primary:     "240 6% 30%",
				Background:  "240 8% 75%", // #c0c0c0 classic Win95 gray
				Foreground:  "0 0% 0%",
				Muted:       "240 5% 90%",
				Accent:      "240 10% 60%",
				Destructive: "0 80% 50%",
				Warning:     "45 100% 50%",
				Success:     "120 60% 40%",
				Info:        "200 70% 50%",
			},
			Dark: Palette{
				Primary:     "240 8% 20%",
				Background:  "240 10% 30%", // Darker gray for dark mode
				Foreground:  "0 0% 95%",
				Muted:       "240 8% 40%",
				Accent:      "240 8% 50%",
				Destructive: "0 60% 55%",
				Warning:     "45 80% 55%",
				Success:     "120 50% 45%",
				Info:        "200 60% 55%",
			},
		},
		Typography: Typography{
			Sans: `"MS Sans Serif", Geneva, sans-serif`,
			Mono: `"Courier New", monospace`,
		},
		BorderRadius: "0", // Sharp corners for retro feel
		Density:      DensityComfortable,
	}
}

// BuiltInTerminal returns the TTY/terminal themed skin.
// FUN! Green-on-black monospace aesthetic inspired by classic terminals.
func BuiltInTerminal() Skin {
	return Skin{
		Name:        "basement-terminal",
		DisplayName: "Terminal",
		Version:     "1.0.0",
		ProductName: "Basement Terminal",
		Palette: PaletteSet{
			Light: Palette{
				Primary:     "142 100% 50%", // Terminal green
				Background:  "0 0% 0%",      // Pure black
				Foreground:  "142 100% 50%", // Green text
				Muted:       "142 30% 30%",  // Dim green
				Accent:      "142 80% 70%",  // Bright green
				Destructive: "0 100% 50%",   // Red error
				Warning:     "45 100% 50%",  // Yellow warning
				Success:     "142 100% 40%", // Green success
				Info:        "200 80% 60%",  // Cyan info
			},
			Dark: Palette{
				Primary:     "142 100% 50%",
				Background:  "0 0% 0%",
				Foreground:  "142 100% 50%",
				Muted:       "142 30% 30%",
				Accent:      "142 80% 70%",
				Destructive: "0 100% 50%",
				Warning:     "45 100% 50%",
				Success:     "142 100% 40%",
				Info:        "200 80% 60%",
			},
		},
		Typography: Typography{
			Sans: `"SF Mono", Monaco, Inconsolata, monospace`,
			Mono: `"SF Mono", Monaco, "Courier New", monospace`,
		},
		BorderRadius: "0", // Sharp corners like a terminal
		Density:      DensityCompact,
	}
}

// RegisterBuiltInSkins registers all built-in skins to the given registry.
// This should be called at boot time after main.go creates the Registry.
func RegisterBuiltInSkins(r *Registry) {
	r.Register(BuiltInDefault())
	r.Register(BuiltInHighContrast())
	r.Register(BuiltInMinimal())
	r.Register(BuiltIn95())
	r.Register(BuiltInTerminal())
}

// SkinJSON returns the JSON representation of a skin for upload/storage.
func SkinJSON(s Skin) ([]byte, error) {
	return json.MarshalIndent(s, "", "  ")
}
