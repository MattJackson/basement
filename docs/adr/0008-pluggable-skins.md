# ADR-0008: Pluggable skins (basement-default + bring-your-own brand)

- **Status**: Accepted (foundation lands in v1.13.0a; upload UI in v1.13.0b; additional built-in skins + typography/density/borderRadius/footer/loginHero wiring in v1.13.0c)
- **Date**: 2026-05-23
- **Decision-maker**: Operator
- **Builds on**: the v1.10.x light/dark CSS-variable token scheme in `frontend/src/index.css`; the `internal/gateway` and `internal/driver` registry pattern (the third axis of pluggability inside basement).
- **Triggered by**: operators running basement at a non-trivial scale (MSPs, internal-PaaS teams, white-label deployments) need to put their own logo + product name + accent palette on the surface without forking the codebase. Today every chrome element is hard-coded to "Basement" with the shadcn defaults.

## Context

basement is shipping into deployments where the operator's brand needs
to live next to the storage product. The current chrome:

- Hard-codes the product name "Basement" in headers, page titles, the
  login card, the favicon and the manifest.
- Pulls colours from the shadcn light/dark token set baked into
  `index.css`. There is no operator-facing surface to override them.
- Has no logo upload story — the SVG logo is a literal React component
  in `frontend/src/shared/ui/Logo.tsx`.

The competitive landscape (the post-MinIO refugees in particular) is
explicit about white-label being table stakes. We want a story
operators can describe in one sentence:

> "Drop a `.basement-skin.json` file in admin settings and your tenant
> wears your brand."

We DO NOT want to ship a CSS-injection escape hatch — that path becomes
a defacto API the operator pins to and reverts every basement upgrade
into a layout-regression hunt. Skins are a closed contract: 8
parameters, validated on upload, rendered through CSS variables only.

## Decision

Adopt the same registry pattern the driver + gateway axes already use:

```
+---------------------+        registry           +-------------------+
| Skin (struct)       |  Register / Get / All     |   *skin.Registry  |
|   Palette (l + d)   | -----------------------> |   in-memory only   |
|   Assets (logos)    |                          +-------------------+
|   Typography                                            ^
|   etc.                                                  |
+---------------------+                          +-------------------+
                                                 | basement-default  |
                                                 | (registered boot) |
                                                 +-------------------+
                                                 | <future built-ins>|
                                                 | <user-uploaded>   |
                                                 +-------------------+
```

The registry is process-local and rebuilt at boot. Built-in skins are
registered by code; user-uploaded skins are de-serialised from
`.basement-skin.json` files persisted under `{dataDir}/skins/` (v1.13.0b
introduces the upload + persistence; v1.13.0a only ships the
registry + the one reference skin).

The org-capabilities store gains two fields:

- `activeSkin` (string, default `"basement-default"`) — the currently-
  rendered skin for every user that doesn't override.
- `skinPolicy` (string, one of `"default" | "locked" | "user-choice"`,
  default `"default"`) — whether per-user overrides are permitted.
  v1.13.0a always renders `activeSkin`; the per-user override path
  lands in v1.13.0c alongside the user-side skin picker.

The light/dark theme toggle is **always per-user** regardless of
`skinPolicy` — a brand identity doesn't dictate whether a given user
sees light or dark mode. The toggle moves from the page chrome into
the UserMenu (`Theme ▸ System / Light / Dark`) so the operator chrome
is brand-clean.

### What's customisable (the 8 elements)

| # | Element | Format | Falls back to |
|---|---------|--------|---------------|
| 1 | Logo | light + dark + favicon variants (PNG/SVG, data URI in JSON) | basement-default's SVG |
| 2 | Product name | string | `"Basement"` |
| 3 | Color palette | semantic tokens (primary/bg/fg/muted/accent/destructive/warning/success/info × light+dark) as HSL strings | basement-default values |
| 4 | Typography | sans + mono family names + optional `fontUrl` for a hosted webfont | system stack |
| 5 | Border radius | CSS length (e.g. `"0.5rem"`) | `0.5rem` |
| 6 | Density | `"compact" \| "comfortable" \| "spacious"` | `"comfortable"` |
| 7 | Footer | `{text, links: [{label, url}]}` | none |
| 8 | Login hero | `{imageDataUri, tagline}` | none |

### What's NOT customisable

- **Component shapes + positions** — operators can't restructure the
  nav, hide the persona pill, rearrange the cluster detail layout.
  Skins do not contain layout JSON.
- **Critical UI affordances** — delete-confirms, lock badges, danger
  zones, the version pill in the UserMenu. Removing them is a safety
  hazard and re-introducing them by accident in a skin-validation
  layer is more code than it's worth.
- **Admin chrome layout** — same reasoning. The admin shell carries
  responsibility for the deploy; its shape stays constant.

### Format

A skin file is a single JSON document. All asset bytes are inlined as
`data:` URIs so the operator can hand a single file off (no asset
bundle, no extracted-zip directory tree, no path-resolution pitfalls).
The on-disk shape is:

```json
{
  "name": "acme-corp",
  "displayName": "ACME Corp",
  "version": "1.0.0",
  "productName": "ACME Storage",
  "assets": {
    "logoLight": "data:image/svg+xml;base64,...",
    "logoDark":  "data:image/svg+xml;base64,...",
    "favicon":   "data:image/png;base64,..."
  },
  "palette": {
    "light": { "primary": "240 5.9% 10%", "bg": "...", ... },
    "dark":  { "primary": "0 0% 98%",     "bg": "...", ... }
  },
  "typography": {
    "sans":    "Inter, ui-sans-serif, system-ui, sans-serif",
    "mono":    "JetBrains Mono, ui-monospace, monospace",
    "fontUrl": "https://fonts.example/acme.css"
  },
  "borderRadius": "0.5rem",
  "density":      "comfortable",
  "footer":       { "text": "© ACME 2026", "links": [...] },
  "loginHero":    { "imageDataUri": "data:image/...", "tagline": "Built for ACME." }
}
```

Validation (v1.13.0b on the upload path): name regex `^[a-z0-9-]{1,64}$`,
total file size ≤ 2 MiB (assets dominate), palette tokens must all
be HSL strings, `density` must be one of the three literals.

### Migration

- **A legacy `org_capabilities.json` without `activeSkin` or
  `skinPolicy`** reads as `activeSkin = "basement-default"`,
  `skinPolicy = "default"`. No on-disk mutation; the next `Update()`
  persists the now-present fields.
- **basement-default mirrors the current Tailwind palette exactly**.
  Pre/post cycle visual diff = zero. This is the operator-locked
  acceptance gate for v1.13.0a — every other skin work happens on top
  of a known-good baseline.

### Why not just expose CSS variables?

We already do, in the sense that `index.css` is hand-edited. The
problem with making that the operator interface is:

1. CSS is a moving target every time we touch a primitive (a shadcn
   upgrade can rename a token; the operator's overrides silently
   no-op).
2. There's no validation surface — an operator can put `color: red !important`
   on `*` and watch every accessibility affordance disappear.
3. There's no portability — an operator can't hand their skin to a
   sibling team without describing the exact CSS-variable file path
   to drop it under.

A typed contract (the `Skin` struct) gives us all three: stable names
the FE consumes, server-side validation on upload, single-file
portability.

### Why not Tailwind-config overrides?

Same family of problems plus: Tailwind config is build-time. Operators
flipping a skin would need to rebuild the FE. The whole point is a
runtime-installable skin per deploy.

## Out of scope for v1.13.0a

- Upload UI for `.basement-skin.json` files. Endpoint + storage land in
  v1.13.0b.
- Additional built-in skins beyond basement-default. v1.13.0b ships 4
  more (TBD names; the operator + senior pick the seed pack).
- Typography / borderRadius / density / footer / loginHero RENDER
  paths. v1.13.0a wires the fields end-to-end (struct → JSON → API)
  but the FE doesn't consume them yet. v1.13.0c wires them through
  the shells.
- Per-user skin override + skinPolicy enforcement. v1.13.0c — needs
  a per-user preference column + a settings page.

## Acceptance

- ADR present, `internal/skin/` registry compiles + tests green.
- `GET /api/v1/skins` returns `[basement-default]`.
- `org_capabilities.json` adds `activeSkin` + `skinPolicy` and migrates
  cleanly.
- UserMenu carries `Theme ▸ System / Light / Dark` and the page-chrome
  toggle is removed from the shells.
- CSS variable layer in `index.css` named `--basement-*` (additive
  wrapper around the existing shadcn `--color-*` set so a downgrade
  rolls back cleanly). basement-default's palette values mirror the
  current shadcn tokens 1:1.
- Visual diff against the pre-cycle deploy: zero pixels.
