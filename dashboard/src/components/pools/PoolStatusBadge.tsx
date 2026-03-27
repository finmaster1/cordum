interface PoolStatusBadgeProps {
  status?: string;
}

const statusStyles: Record<string, string> = {
  active: "bg-success/10 text-success",
  draining: "bg-warning/10 text-warning animate-pulse",
  inactive: "bg-muted/10 text-muted-foreground",
};

export function PoolStatusBadge({ status }: PoolStatusBadgeProps) {
  const resolved = status || "active";
  const style = statusStyles[resolved] || statusStyles.active;

  return (
    <span className={`inline-flex items-center rounded px-1.5 py-0.5 text-xs font-medium uppercase tracking-wider ${style}`}>
      {resolved}
    </span>
  );
}
