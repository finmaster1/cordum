import type { ReactNode } from "react";
import { CheckCircle } from "lucide-react";
import { Card, CardHeader, CardTitle } from "./Card";
import { cn } from "../../lib/utils";

interface CardEmptyProps {
  icon?: ReactNode;
  message: string;
  title?: string;
  className?: string;
}

export function CardEmpty({ icon, message, title, className }: CardEmptyProps) {
  return (
    <Card className={className}>
      {title && (
        <CardHeader>
          <CardTitle className="text-sm">{title}</CardTitle>
        </CardHeader>
      )}
      <div className={cn("flex items-center justify-center gap-2 py-6 text-sm text-muted-foreground")}>
        {icon ?? <CheckCircle className="h-4 w-4" />}
        {message}
      </div>
    </Card>
  );
}
