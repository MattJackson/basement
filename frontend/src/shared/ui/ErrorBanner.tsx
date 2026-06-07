import { cn } from "@/lib/utils";

interface ErrorBannerProps {
  message: string;
  onRetry?: () => void;
}

export function ErrorBanner({ message, onRetry }: ErrorBannerProps) {
  const className = "w-full rounded-lg border bg-destructive/10 p-4";

  // When a retry handler is provided the banner is interactive — render a
  // real <button> so it's keyboard-operable (Enter/Space), focusable, and
  // exposes a button role to assistive tech. A bare <div onClick> did none
  // of these.
  if (onRetry) {
    return (
      <button
        type="button"
        onClick={onRetry}
        aria-label="Retry"
        className={cn(
          className,
          "cursor-pointer text-left focus:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
        )}
      >
        <p className="text-sm text-destructive">{message}</p>
      </button>
    );
  }

  return (
    <div className={className} role="alert">
      <p className="text-sm text-destructive">{message}</p>
    </div>
  );
}
