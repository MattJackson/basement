// Package skin defines the pluggable-skin contract per ADR-0008.
//
// A Skin is the operator-facing identity surface of a basement
// deployment: logo, product name, palette, typography. Skins are
// registered at boot via the Registry; the basement-default skin is
// registered by code and mirrors the current shadcn palette so a
// fresh install renders identically to v1.12.x.
//
// v1.13.0a (foundation) ships:
//   - The Skin / Registry types (this file + registry.go).
//   - basement-default registered at boot.
//   - GET /api/v1/skins read endpoint surfacing the registry.
//   - OrgCapability fields ActiveSkin + SkinPolicy.
//
// Subsequent cycles add user-uploaded skins (v1.13.0b), additional
// built-in skins (v1.13.0b), and the FE wiring for typography /
// density / borderRadius / footer / loginHero (v1.13.0c). The struct
// shape is the cross-cycle contract — fields land in the struct now
// even when the FE consumer arrives later, so the on-disk
// .basement-skin.json shape stabilises before v1.13.0b uploads start
// persisting files.

package skin

// BuiltInDefaultName is the registry key for the reference skin that
// ships with every basement deploy. It is the default value of
// OrgCapabilities.ActiveSkin and is the fallback the FE renders when
// an operator-selected skin is missing from the registry (e.g. an
// uploaded skin file was deleted after selection — v1.13.0b will warn
// loud, but the UI must still render).
const BuiltInDefaultName = "basement-default"

// Density is the operator-selectable layout-density enum. Pinned to
// these three literals to keep the FE renderer (v1.13.0c) tractable:
// each density maps to a fixed set of padding + gap tokens; arbitrary
// values would require a richer parser and would let an operator break
// alignment.
type Density string

const (
	DensityCompact     Density = "compact"
	DensityComfortable Density = "comfortable"
	DensitySpacious    Density = "spacious"
)

// IsValid reports whether d is one of the three permitted literals.
// Helpers like this live with the type so the validation rule has a
// single home; the upload handler in v1.13.0b will call IsValid before
// accepting a posted file.
func (d Density) IsValid() bool {
	switch d {
	case DensityCompact, DensityComfortable, DensitySpacious:
		return true
	}
	return false
}

// Skin is the full operator-facing identity bundle described in
// ADR-0008. Every field has a defined "fall back to basement-default"
// path; this is enforced by the loader in v1.13.0b — for v1.13.0a
// only the basement-default skin populates the registry so every
// field is non-zero.
type Skin struct {
	// Name is the registry key. Format: lowercase alphanumeric + dashes,
	// 1-64 chars. The uploader in v1.13.0b validates against this regex.
	Name string `json:"name"`

	// DisplayName is the operator-facing label rendered in the skin
	// selector list (/admin/system → Skins) and in the user-side
	// picker (v1.13.0c).
	DisplayName string `json:"displayName"`

	// Version follows the skin AUTHOR's own versioning — basement does
	// not interpret it. Displayed in the selector so an operator can
	// distinguish v1 of acme-corp from v2.
	Version string `json:"version"`

	// ProductName replaces the literal "Basement" in headers, page
	// titles, the login card and the manifest. Empty falls back to
	// "Basement" in the FE consumer.
	ProductName string `json:"productName"`

	// Assets carries logo + favicon bytes inlined as data URIs (see
	// ADR-0008 "Format"). v1.13.0a populates these for
	// basement-default from the built-in SVG.
	Assets Assets `json:"assets"`

	// Palette is the semantic-token set, separately keyed for light
	// + dark mode. The FE consumes these into the CSS variable layer
	// at runtime.
	Palette PaletteSet `json:"palette"`

	// Typography is the operator's font-family preference plus an
	// optional hosted-font URL (e.g. a Google Fonts stylesheet).
	// v1.13.0a populates the system stack for basement-default;
	// v1.13.0c wires this through to the FE.
	Typography Typography `json:"typography"`

	// BorderRadius is the CSS length used for rounded primitives
	// (buttons, cards, modals). v1.13.0c wires consumption.
	BorderRadius string `json:"borderRadius"`

	// Density is the layout-density enum. Validates via Density.IsValid.
	Density Density `json:"density"`

	// Footer is the optional operator footer text + links. Nil means
	// "no footer renders". v1.13.0c wires consumption.
	Footer *Footer `json:"footer,omitempty"`

	// LoginHero is the optional pre-auth marketing surface (image +
	// tagline) shown on /admin/login when the operator wants the
	// login card to share the canvas with their brand image. Nil
	// renders the current login layout unchanged. v1.13.0c wires
	// consumption.
	LoginHero *LoginHero `json:"loginHero,omitempty"`
}

// Assets carries the logo + favicon variants. Bytes inlined as
// data: URIs so a .basement-skin.json file is self-contained and the
// operator never deals with asset paths.
type Assets struct {
	// LogoLight renders on the light palette. SVG strongly preferred
	// (sharp at every zoom); PNG accepted. Required for any non-default
	// skin.
	LogoLight string `json:"logoLight,omitempty"`

	// LogoDark renders on the dark palette. Same format constraints.
	// Optional — the loader (v1.13.0b) falls back to LogoLight when
	// absent (a single mark designed for both palettes is common).
	LogoDark string `json:"logoDark,omitempty"`

	// Favicon is the browser-tab icon. PNG strongly preferred (broader
	// browser support than SVG favicons). Optional — falls back to the
	// built-in basement favicon.
	Favicon string `json:"favicon,omitempty"`
}

// Palette is the semantic-token set for ONE mode (light or dark).
// Every token is an HSL string of the shape "H S% L%" — the FE
// renders these into CSS `hsl()` calls. ADR-0008 freezes this set;
// adding a token is a v1.13.x+ migration (existing skins miss the new
// token and fall back to basement-default's value).
type Palette struct {
	Primary     string `json:"primary"`
	Background  string `json:"bg"`
	Foreground  string `json:"fg"`
	Muted       string `json:"muted"`
	Accent      string `json:"accent"`
	Destructive string `json:"destructive"`
	Warning     string `json:"warning"`
	Success     string `json:"success"`
	Info        string `json:"info"`
}

// PaletteSet is the light + dark pair. An operator's skin MUST
// populate both (the v1.13.0b uploader will reject a file with only
// one mode — palette consistency across light/dark is part of brand
// identity).
type PaletteSet struct {
	Light Palette `json:"light"`
	Dark  Palette `json:"dark"`
}

// Typography is the operator's font preference. Falls back to the
// system stack for absent fields.
type Typography struct {
	// Sans is the CSS font-family stack for regular text. The first
	// family is the operator's preferred face; subsequent families
	// are fallbacks. basement-default uses the system stack to avoid
	// pulling a webfont on first paint.
	Sans string `json:"sans"`

	// Mono is the CSS font-family stack for monospace surfaces
	// (audit log timestamps, version pill, code snippets).
	Mono string `json:"mono"`

	// FontURL is an optional URL to a webfont stylesheet (e.g.
	// "https://fonts.googleapis.com/css2?family=Inter&display=swap").
	// v1.13.0c injects this as a <link rel="stylesheet"> when set.
	// The FE consumer must validate the URL is https + same-origin or
	// from an operator-listed allowlist — skin files can carry
	// untrusted URLs.
	FontURL string `json:"fontUrl,omitempty"`
}

// Footer is the optional operator footer (text + a small set of
// links). Rendered below the main content area in every shell when
// non-nil. Five-link cap is editorial, not technical — the renderer
// in v1.13.0c will truncate beyond five.
type Footer struct {
	Text  string       `json:"text,omitempty"`
	Links []FooterLink `json:"links,omitempty"`
}

// FooterLink is one entry in a footer's links array.
type FooterLink struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// LoginHero is the optional pre-auth marketing surface. Lives next to
// the login card on wide viewports; collapses below the card on
// narrow viewports.
type LoginHero struct {
	// ImageDataURI is the hero image inlined as a data: URI. Same
	// format rules as Assets.LogoLight.
	ImageDataURI string `json:"imageDataUri,omitempty"`

	// Tagline is a short brand line displayed under the image
	// (e.g. "Built for ACME."). Empty means "image only".
	Tagline string `json:"tagline,omitempty"`
}
