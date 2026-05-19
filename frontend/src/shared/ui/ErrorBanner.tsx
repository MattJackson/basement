import { cn } from "@/lib/utils";

interface ErrorBannerProps {
  message: string;
  onRetry?: () => void;
}

export function ErrorBanner({ message, onRetry }: ErrorBannerProps) {
  return (
    <div className={cn(
      "w-full rounded-lg border bg-destructive/10 p-4",
      onRetry && "cursor-pointer"
    )}
    onClick={onRetry}>
      <p className="text-sm text-destructive">{message}</p>
    </div>
  );
}
