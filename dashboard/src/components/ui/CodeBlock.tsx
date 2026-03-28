import { useState, useCallback } from "react";
import { Copy, Check, ChevronDown } from "lucide-react";
import { toast } from "sonner";
import { cn } from "@/lib/utils";

const MAX_DISPLAY_BYTES = 100 * 1024; // 100KB — truncate beyond this

export interface CodeBlockProps {
  children?: string | null;
  title?: string;
  language?: string;
  maxHeight?: number;
  collapsible?: boolean;
  defaultExpanded?: boolean;
  copyable?: boolean;
  className?: string;
}

export function CodeBlock({
  children,
  title,
  language,
  maxHeight = 400,
  collapsible = false,
  defaultExpanded = true,
  copyable = true,
  className,
}: CodeBlockProps) {
  const [expanded, setExpanded] = useState(defaultExpanded);
  const [copied, setCopied] = useState(false);
  const [showFull, setShowFull] = useState(false);

  const raw = children ?? "";
  const isTruncated = !showFull && raw.length > MAX_DISPLAY_BYTES;
  const displayContent = isTruncated ? raw.slice(0, MAX_DISPLAY_BYTES) : raw;
  const isEmpty = !raw.trim();

  const handleCopy = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(raw);
      setCopied(true);
      toast.success("Copied to clipboard");
      setTimeout(() => setCopied(false), 2000);
    } catch {
      toast.error("Copy failed");
    }
  }, [raw]);

  return (
    <div
      role="region"
      aria-label={title ?? "Code block"}
      className={cn("rounded-2xl border border-border/50 overflow-hidden", className)}
    >
      {/* Title bar — Mac-style chrome */}
      <div className="flex items-center gap-3 px-4 py-2.5 bg-[#161b1e] border-b border-white/5">
        {/* Traffic lights (decorative) */}
        <div className="flex gap-1.5 shrink-0" aria-hidden="true">
          <span className="w-2.5 h-2.5 rounded-full bg-[#ff5f57]" />
          <span className="w-2.5 h-2.5 rounded-full bg-[#febc2e]" />
          <span className="w-2.5 h-2.5 rounded-full bg-[#28c840]" />
        </div>

        {/* Title */}
        {title && (
          <span className="flex-1 text-center text-xs font-mono text-white/50 truncate">
            {title}
          </span>
        )}
        {!title && <span className="flex-1" />}

        {/* Right side: language badge + copy + collapse toggle */}
        <div className="flex items-center gap-2 shrink-0">
          {language && (
            <span className="text-[10px] font-mono uppercase tracking-wider text-white/30 px-1.5 py-0.5 rounded bg-white/5">
              {language}
            </span>
          )}
          {copyable && !isEmpty && (
            <button
              type="button"
              onClick={handleCopy}
              aria-label="Copy code"
              className="p-1 rounded text-white/30 hover:text-white/70 transition-colors"
            >
              {copied ? <Check className="w-3.5 h-3.5" /> : <Copy className="w-3.5 h-3.5" />}
            </button>
          )}
          {collapsible && (
            <button
              type="button"
              onClick={() => setExpanded(!expanded)}
              aria-label={expanded ? "Collapse code" : "Expand code"}
              className="p-1 rounded text-white/30 hover:text-white/70 transition-colors"
            >
              <ChevronDown
                className={cn(
                  "w-3.5 h-3.5 transition-transform duration-150",
                  expanded && "rotate-180",
                )}
              />
            </button>
          )}
        </div>
      </div>

      {/* Content */}
      <div
        className={cn(
          "transition-all duration-200 overflow-hidden",
          !expanded && collapsible && "max-h-0",
        )}
      >
        {isEmpty ? (
          <div className="px-4 py-6 bg-[#0f1416] text-center text-xs text-white/20 font-mono">
            No content
          </div>
        ) : (
          <div
            className="bg-[#0f1416] overflow-y-auto"
            style={{ maxHeight: expanded ? maxHeight : 0 }}
          >
            <pre className="px-4 py-3 font-mono text-xs leading-relaxed text-white/85 whitespace-pre overflow-x-auto">
              {displayContent}
              {isTruncated && (
                <>
                  {"\n"}
                  <span className="text-white/30">
                    ... (truncated, {Math.round(raw.length / 1024)}KB total){" "}
                    <button
                      type="button"
                      onClick={() => setShowFull(true)}
                      className="underline text-white/40 hover:text-white/60"
                    >
                      Show all
                    </button>
                  </span>
                </>
              )}
            </pre>
          </div>
        )}
      </div>
    </div>
  );
}
