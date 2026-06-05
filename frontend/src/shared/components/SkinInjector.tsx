import { useEffect } from "react";
import { useSkin } from "@/shared/hooks/useSkin";

/**
 * v1.13.0c: SkinInjector - applies skin styles to the document.
 *
 * SECURITY: skin fields are operator-uploaded and rendered into the live
 * DOM/CSS (including the pre-auth login page). The server validates asset
 * data-URIs + URLs at upload time, but this is the rendering boundary, so
 * we re-validate here (defense in depth) before any value reaches an href,
 * a <link>, or a CSS declaration. There is NO innerHTML/dangerouslySet-
 * InnerHTML anywhere; text is rendered as JSX children (auto-escaped).
 */

// Only http(s)/mailto absolute URLs or app-relative paths may become an href.
// Blocks javascript:/data:/vbscript: etc.
export function safeHref(url: string | undefined): string | null {
  if (!url) return null;
  if (/^\/[^/\\]/.test(url)) return url; // app-relative path like /docs
  try {
    const u = new URL(url, window.location.origin);
    if (u.protocol === "http:" || u.protocol === "https:" || u.protocol === "mailto:") {
      return url;
    }
  } catch {
    /* not a parseable URL */
  }
  return null;
}

// Webfont stylesheet URLs must be https.
const HTTPS_RE = /^https:\/\//i;
// CSS length for border-radius (e.g. 8px, 0.5rem). Bare "0" is a valid
// unitless CSS length and is accepted; the server mirrors this shape.
const CSS_LEN_RE = /^(0|\d+(\.\d+)?(px|rem|em|%))$/;
// font-family stack: letters/digits/space/comma/quotes/dash/underscore only —
// rejects ( ) ; { } / which would break out of the declaration or inject url().
const SAFE_FONT_RE = /^[a-zA-Z0-9 ,_'"-]+$/;
// Login-hero image must be an image data URI (or https). Blocks data:text/html etc.
const SAFE_IMAGE_RE = /^(data:image\/(png|jpe?g|gif|webp|svg\+xml|x-icon|vnd\.microsoft\.icon);base64,|https:\/\/)/i;

export function SkinInjector() {
  const { skin, isLoading } = useSkin();

  useEffect(() => {
    if (!skin || isLoading) return;

    // Inject webfont stylesheet only for an https URL. Compare existing
    // links by .href in JS (NOT by building a selector from the operator
    // value — that was a selector-injection / crash vector).
    const fontUrl = skin.typography?.fontUrl;
    if (fontUrl && HTTPS_RE.test(fontUrl)) {
      const exists = Array.from(
        document.head.querySelectorAll('link[rel="stylesheet"]'),
      ).some((l) => (l as HTMLLinkElement).href === fontUrl);
      if (!exists) {
        const link = document.createElement("link");
        link.rel = "stylesheet";
        link.href = fontUrl;
        document.head.appendChild(link);
      }
    }

    // Apply typography and border radius via CSS custom properties — only
    // when the value matches a strict safe pattern, so an operator value
    // can't break out of the CSS declaration and inject rules.
    const root = document.documentElement;

    if (skin.typography?.sans && SAFE_FONT_RE.test(skin.typography.sans)) {
      root.style.setProperty("--skin-sans-font", skin.typography.sans);
    }

    if (skin.typography?.mono && SAFE_FONT_RE.test(skin.typography.mono)) {
      root.style.setProperty("--skin-mono-font", skin.typography.mono);
    }

    if (skin.borderRadius && CSS_LEN_RE.test(skin.borderRadius)) {
      root.style.setProperty("--skin-border-radius", skin.borderRadius);
    }
  }, [skin, isLoading]);

  return null;
}

/**
 * v1.13.0c: OperatorFooter - renders the footer text and links when non-nil.
 */
export function OperatorFooter() {
  const { skin, isLoading } = useSkin();

  if (isLoading || !skin?.footer) {
    return null;
  }

  const footer = skin.footer;

  // Truncate to 5 links max per spec
  const displayLinks = footer.links ? footer.links.slice(0, 5) : [];

  return (
    <footer className="border-t bg-card/80 backdrop-blur mt-auto">
      <div className="max-w-[1280px] mx-auto px-4 sm:px-6 lg:px-8 py-4">
        {footer.text && (
          <p className="text-sm text-muted-foreground mb-2">{footer.text}</p>
        )}
        {displayLinks.length > 0 && (
          <div className="flex flex-wrap gap-3">
            {displayLinks.map((link, idx) => {
              const href = safeHref(link.url);
              // An unsafe URL (javascript:/data:/etc.) renders as plain text,
              // not a clickable href — never execute on click.
              if (!href) {
                return (
                  <span
                    key={idx}
                    className="text-sm text-muted-foreground"
                  >
                    {link.label}
                  </span>
                );
              }
              return (
                <a
                  key={idx}
                  href={href}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="text-sm text-muted-foreground hover:text-foreground transition-colors"
                >
                  {link.label}
                </a>
              );
            })}
          </div>
        )}
      </div>
    </footer>
  );
}

/**
 * v1.13.0c: LoginHero display component for use in LoginForm.
 */
export function LoginHeroDisplay({ imageDataUri, tagline }: { imageDataUri?: string; tagline?: string }) {
  // PRE-AUTH surface: only render an image source that is an image data URI
  // or an https URL. Anything else (javascript:/data:text/...) is dropped.
  if (!imageDataUri || !SAFE_IMAGE_RE.test(imageDataUri)) {
    return null;
  }

  return (
    <div className="w-full max-w-md mb-6">
      <img
        src={imageDataUri}
        alt="Brand hero"
        className="w-full h-auto rounded-lg shadow-sm"
      />
      {tagline && (
        <p className="text-center text-sm text-muted-foreground mt-3">{tagline}</p>
      )}
    </div>
  );
}
