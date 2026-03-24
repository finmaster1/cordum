import { AlertTriangle } from "lucide-react";
import { Button } from "./Button";

interface ErrorBannerProps {
  message?: string;
  title?: string;
  onRetry?: () => void;
  className?: string;
}

export function ErrorBanner({
  message,
  title = "Something went wrong",
  onRetry,
  className,
}: ErrorBannerProps) {
  return (
    <div className={className}>
      <div className="flex flex-col items-center justify-center py-12 px-6 text-center">
        <div className="mb-4 flex h-12 w-12 items-center justify-center rounded-2xl border border-destructive/20 bg-destructive/10 text-destructive shadow-soft">
          <AlertTriangle className="w-5 h-5" />
        </div>
        <h3 className="text-sm font-semibold font-display text-foreground mb-1">
          {title}
        </h3>
        {message && (
          <p className="text-xs text-muted-foreground max-w-sm">{message}</p>
        )}
        {onRetry && (
          <div className="mt-4">
            <Button variant="outline" size="sm" onClick={onRetry}>
              Retry
            </Button>
          </div>
        )}
      </div>
    </div>
  );
}
