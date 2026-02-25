import { cn } from "@/lib/utils";
import { ArrowUpRight, ArrowDownRight, Minus } from "lucide-react";

interface MetricValueProps {
  value: string | number;
  label: string;
  trend?: { value: number; label?: string };
  unit?: string;
  icon?: React.ReactNode;
  className?: string;
  size?: "sm" | "md" | "lg";
}

export function MetricValue({
  value,
  label,
  trend,
  unit,
  icon,
  className,
  size = "md",
}: MetricValueProps) {
  const trendDirection =
    trend && trend.value > 0 ? "up" : trend && trend.value < 0 ? "down" : "flat";

  return (
    <div className={cn("flex flex-col", className)}>
      <div className="flex items-center justify-between mb-3">
        <span className="text-xs font-mono text-muted-foreground uppercase tracking-wider">
          {label}
        </span>
        {icon && <span className="text-cordum">{icon}</span>}
      </div>
      <div className="flex items-baseline gap-2">
        <span
          className={cn(
            "font-mono font-bold text-foreground",
            size === "sm" && "text-xl",
            size === "md" && "text-2xl",
            size === "lg" && "text-4xl",
          )}
        >
          {value}
        </span>
        {unit && (
          <span className="text-xs text-muted-foreground">{unit}</span>
        )}
        {trend && (
          <span
            className={cn(
              "text-xs font-mono flex items-center",
              trendDirection === "up" && "text-emerald-400",
              trendDirection === "down" && "text-red-400",
              trendDirection === "flat" && "text-muted-foreground",
            )}
          >
            {trendDirection === "up" && <ArrowUpRight className="w-3 h-3" />}
            {trendDirection === "down" && <ArrowDownRight className="w-3 h-3" />}
            {trendDirection === "flat" && <Minus className="w-3 h-3" />}
            {trend.value > 0 ? "+" : ""}
            {trend.value}%
          </span>
        )}
      </div>
      {trend?.label && (
        <p className="text-xs text-muted-foreground mt-1">{trend.label}</p>
      )}
    </div>
  );
}
