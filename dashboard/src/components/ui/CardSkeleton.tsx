import { Card, CardHeader, CardTitle } from "./Card";
import { cn } from "../../lib/utils";

interface CardSkeletonProps {
  rows?: number;
  title?: string;
  className?: string;
}

const ROW_WIDTHS = ["w-1/3", "w-full", "w-2/3", "w-4/5", "w-1/2"];

export function CardSkeleton({ rows = 3, title, className }: CardSkeletonProps) {
  return (
    <Card className={className}>
      {title && (
        <CardHeader>
          <CardTitle className="text-sm">{title}</CardTitle>
        </CardHeader>
      )}
      <div className="space-y-3 animate-pulse">
        {Array.from({ length: rows }, (_, i) => (
          <div
            key={i}
            className={cn(
              "h-4 rounded bg-surface2",
              ROW_WIDTHS[i % ROW_WIDTHS.length],
            )}
          />
        ))}
      </div>
    </Card>
  );
}
