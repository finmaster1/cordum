import { cn } from "@/lib/utils";

type BadgeVariant = "healthy" | "warning" | "danger" | "info" | "muted" | "cordum";

/* Exact match to showcase: uses emerald/amber/red/blue Tailwind colors */
const variants: Record<BadgeVariant, string> = {
  healthy: "bg-emerald-500/15 text-emerald-400 border-emerald-500/20",
  warning: "bg-amber-500/15 text-amber-400 border-amber-500/20",
  danger: "bg-red-500/15 text-red-400 border-red-500/20",
  info: "bg-blue-500/15 text-blue-400 border-blue-500/20",
  muted: "bg-gray-500/15 text-gray-400 border-gray-500/20",
  cordum: "bg-cordum/12 text-cordum border-cordum/20",
};

const dotColors: Record<BadgeVariant, string> = {
  healthy: "bg-emerald-400",
  warning: "bg-amber-400",
  danger: "bg-red-400",
  info: "bg-blue-400",
  muted: "bg-gray-400",
  cordum: "bg-cordum",
};

interface StatusBadgeProps {
  variant?: BadgeVariant;
  children: React.ReactNode;
  dot?: boolean;
  className?: string;
  pulse?: boolean;
}

export function StatusBadge({
  variant = "muted",
  children,
  dot = false,
  className,
  pulse = false,
}: StatusBadgeProps) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full text-[11px] font-medium border",
        variants[variant],
        className,
      )}
    >
      {dot && (
        <span className="relative flex h-1.5 w-1.5">
          {pulse && (
            <span
              className={cn(
                "absolute inset-0 rounded-full status-pulse",
                dotColors[variant],
              )}
            />
          )}
          <span
            className={cn(
              "relative inline-flex rounded-full h-1.5 w-1.5",
              dotColors[variant],
            )}
          />
        </span>
      )}
      {children}
    </span>
  );
}
