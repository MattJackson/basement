import * as React from "react";
import { cn } from "@/lib/utils";

interface AlertDialogProps {
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  children: React.ReactNode;
}

export function AlertDialog({ open, onOpenChange, children }: AlertDialogProps) {
  const handleBackdropClick = (e: React.MouseEvent<HTMLDivElement>) => {
    if (e.target === e.currentTarget) {
      onOpenChange?.(false);
    }
  };

  React.useEffect(() => {
    const handleEscape = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        onOpenChange?.(false);
      }
    };

    document.addEventListener("keydown", handleEscape);
    return () => document.removeEventListener("keydown", handleEscape);
  }, [onOpenChange]);

  if (!open) return null;

  return (
    <div 
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
      onClick={handleBackdropClick}
    >
      {children}
    </div>
  );
}

interface AlertDialogContentProps extends React.HTMLAttributes<HTMLDivElement> {
  children: React.ReactNode;
}

export function AlertDialogContent({ children, className }: AlertDialogContentProps) {
  return (
    <div
      className={cn(
        "mx-4 w-full max-w-md rounded-lg bg-background p-6 shadow-lg",
        "flex flex-col gap-4",
        "focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2",
        className
      )}
      role="alertdialog"
      aria-modal="true"
    >
      {children}
    </div>
  );
}

interface AlertDialogHeaderProps extends React.HTMLAttributes<HTMLDivElement> {
  children: React.ReactNode;
}

export function AlertDialogHeader({ children, className }: AlertDialogHeaderProps) {
  return (
    <div 
      className={cn("flex flex-col space-y-2 text-center sm:text-left", className)}
    >
      {children}
    </div>
  );
}

interface AlertDialogTitleProps extends React.HTMLAttributes<HTMLHeadingElement> {
  children: React.ReactNode;
}

export function AlertDialogTitle({ children, className }: AlertDialogTitleProps) {
  return (
    <h2 
      className={cn("text-lg font-semibold", className)}
    >
      {children}
    </h2>
  );
}

interface AlertDialogDescriptionProps extends React.HTMLAttributes<HTMLDivElement> {
  children: React.ReactNode;
}

export function AlertDialogDescription({ children, className }: AlertDialogDescriptionProps) {
  return (
    <div 
      className={cn("text-sm text-muted-foreground", className)}
    >
      {children}
    </div>
  );
}

interface AlertDialogFooterProps extends React.HTMLAttributes<HTMLDivElement> {
  children: React.ReactNode;
}

export function AlertDialogFooter({ children, className }: AlertDialogFooterProps) {
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

interface AlertDialogActionProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  children: React.ReactNode;
}

export function AlertDialogAction({ children, ...props }: AlertDialogActionProps) {
  return (
    <button
      type="button"
      {...props}
      className={cn(
        "inline-flex h-9 items-center justify-center whitespace-nowrap rounded-md px-4 py-2 text-sm font-medium ring-offset-background transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-50 bg-primary text-primary-foreground hover:bg-primary/90",
        props.className
      )}
    >
      {children}
    </button>
  );
}

interface AlertDialogCancelProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  children: React.ReactNode;
}

export function AlertDialogCancel({ children, ...props }: AlertDialogCancelProps) {
  return (
    <button
      type="button"
      {...props}
      className={cn(
        "inline-flex h-9 items-center justify-center whitespace-nowrap rounded-md px-4 py-2 text-sm font-medium ring-offset-background transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-50 border border-input bg-transparent hover:bg-accent hover:text-accent-foreground",
        props.className
      )}
    >
      {children}
    </button>
  );
}
