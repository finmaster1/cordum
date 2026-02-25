import { forwardRef, type ButtonHTMLAttributes } from "react";
import { cn } from "@/lib/utils";
import { Loader2 } from "lucide-react";

type ButtonVariant = "primary" | "secondary" | "ghost" | "danger" | "outline";
type ButtonSize = "sm" | "md" | "lg" | "icon";

/* Exact match to showcase button styles */
const variantStyles: Record<ButtonVariant, string> = {
  primary:
    "bg-cordum text-surface-0 hover:bg-cordum-dim font-semibold",
  secondary:
    "bg-secondary text-secondary-foreground hover:bg-surface-3",
  ghost:
    "text-muted-foreground hover:text-foreground hover:bg-surface-2",
  danger:
    "bg-red-500/15 text-red-400 hover:bg-red-500/25 border border-red-500/20",
  outline:
    "border border-border text-foreground hover:bg-surface-2",
};

const sizeStyles: Record<ButtonSize, string> = {
  sm: "h-8 px-3 text-xs rounded-md gap-1.5",
  md: "h-9 px-4 text-sm rounded-md gap-2",
  lg: "h-11 px-6 text-sm rounded-lg gap-2",
  icon: "h-9 w-9 rounded-md",
};

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant;
  size?: ButtonSize;
  loading?: boolean;
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(
  ({ variant = "primary", size = "md", loading, className, children, disabled, ...props }, ref) => {
    return (
      <button
        ref={ref}
        disabled={disabled || loading}
        className={cn(
          "inline-flex items-center justify-center font-medium transition-all duration-150 whitespace-nowrap",
          "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-cordum/40 focus-visible:ring-offset-2 focus-visible:ring-offset-background",
          "disabled:opacity-50 disabled:pointer-events-none",
          "active:scale-[0.98]",
          variantStyles[variant],
          sizeStyles[size],
          className,
        )}
        {...props}
      >
        {loading && <Loader2 className="w-3.5 h-3.5 animate-spin" />}
        {children}
      </button>
    );
  },
);

Button.displayName = "Button";
