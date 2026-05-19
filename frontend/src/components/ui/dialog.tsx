import * as React from "react";
import { cn } from "@/lib/utils";

interface DialogProps {
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  /**
   * Set false to disable backdrop-click and Escape dismissal. Use
   * for irreversible disclosures (e.g. create-key secret-shown-once)
   * where accidental dismissal loses data.
   */
  dismissible?: boolean;
  children: React.ReactNode;
  className?: string;
}

export function Dialog({ open, onOpenChange, dismissible = true, children, className }: DialogProps) {
  const handleBackdropClick = (e: React.MouseEvent<HTMLDivElement>) => {
    if (!dismissible) return;
    if (e.target === e.currentTarget) {
      onOpenChange?.(false);
    }
  };

  React.useEffect(() => {
    if (!dismissible) return;
    const handleEscape = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        onOpenChange?.(false);
      }
    };

    document.addEventListener("keydown", handleEscape);
    return () => document.removeEventListener("keydown", handleEscape);
  }, [onOpenChange, dismissible]);

  if (!open) return null;

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
      onClick={handleBackdropClick}
    >
      <div
        className={cn(
          "mx-4 w-full max-w-lg rounded-lg bg-background p-6 shadow-lg",
          "flex flex-col gap-5",
          "focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2",
          className
        )}
        role="dialog"
        aria-modal="true"
      >
        {children}
      </div>
    </div>
  );
}

interface DialogContentProps extends React.HTMLAttributes<HTMLDivElement> {
  children: React.ReactNode;
}

export function DialogContent({ children, className }: DialogContentProps) {
  return <div className={className}>{children}</div>;
}

interface DialogHeaderProps extends React.HTMLAttributes<HTMLDivElement> {
  children: React.ReactNode;
}

export function DialogHeader({ children, className }: DialogHeaderProps) {
  return (
    <div className={cn("flex flex-col space-y-2 text-center sm:text-left", className)}>
      {children}
    </div>
  );
}

interface DialogTitleProps extends React.HTMLAttributes<HTMLHeadingElement> {
  children: React.ReactNode;
}

export function DialogTitle({ children, className }: DialogTitleProps) {
  return (
    <h2 
      className={cn("text-lg font-semibold leading-none tracking-tight", className)}
    >
      {children}
    </h2>
  );
}

interface DialogDescriptionProps extends React.HTMLAttributes<HTMLParagraphElement> {
  children: React.ReactNode;
}

export function DialogDescription({ children, className }: DialogDescriptionProps) {
  return (
    <p 
      className={cn("text-sm text-muted-foreground", className)}
    >
      {children}
    </p>
  );
}

interface DialogFooterProps extends React.HTMLAttributes<HTMLDivElement> {
  children: React.ReactNode;
}

export function DialogFooter({ children, className }: DialogFooterProps) {
  return (
    <div
      className={cn(
        "flex flex-col-reverse gap-2 sm:flex-row sm:justify-end sm:gap-2 sm:space-x-0 pt-2",
        className
      )}
    >
      {children}
    </div>
  );
}

interface DialogCloseProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  children: React.ReactNode;
}

export function DialogClose({ children, ...props }: DialogCloseProps) {
  return (
    <button 
      type="button"
      {...props}
      className={cn("inline-flex items-center justify-center rounded-md text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-50", props.className)}
    >
      {children}
    </button>
  );
}
