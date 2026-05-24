import { useEffect } from "react";
import { useSkin } from "@/shared/hooks/useSkin";

/**
 * v1.13.0c: SkinInjector - applies skin styles to the document.
 * 
 * Handles:
 * 1. Webfont injection via <link rel="stylesheet"> for typography.fontUrl
 * 2. CSS custom property injection for skin-sans-font, skin-mono-font, skin-border-radius
 * 3. Footer rendering when non-nil
 * 4. LoginHero display on login route (handled separately in LoginForm)
 */
export function SkinInjector() {
  const { skin, isLoading } = useSkin();

  useEffect(() => {
    if (!skin || isLoading) return;

    // Inject webfont stylesheet if fontUrl is set
    if (skin.typography?.fontUrl) {
      const existingLink = document.querySelector(`link[href="${skin.typography.fontUrl}"]`) as HTMLLinkElement;
      if (!existingLink) {
        const link = document.createElement("link");
        link.rel = "stylesheet";
        link.href = skin.typography.fontUrl;
        document.head.appendChild(link);
      }
    }

    // Apply typography and border radius via CSS custom properties
    const root = document.documentElement;
    
    if (skin.typography?.sans) {
      root.style.setProperty("--skin-sans-font", skin.typography.sans);
    }
    
    if (skin.typography?.mono) {
      root.style.setProperty("--skin-mono-font", skin.typography.mono);
    }

    if (skin.borderRadius) {
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
            {displayLinks.map((link, idx) => (
              <a
                key={idx}
                href={link.url}
                target="_blank"
                rel="noopener noreferrer"
                className="text-sm text-muted-foreground hover:text-foreground transition-colors"
              >
                {link.label}
              </a>
            ))}
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
  if (!imageDataUri) {
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
