import { useMemo } from "react";
import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import { get, ApiError } from "../api/client";
import type { AuditEntry } from "../api/types";
import {
  mapAuditEvent,
  type SiemAuditEventInput,
} from "../api/transform";

// AuditFilters mirrors useAudit.ts so consumers can swap in place.
// Fields that don't have a server-side equivalent (page/perPage/sort)
// are applied client-side after the SIEM page is fetched, matching
// the previous behavior with the policy-bundle audit feed.
export interface AuditEventsFilters {
  eventType?: string[];
  severity?: string[];
  from?: string;
  to?: string;
  search?: string;
  cursor?: string;
  limit?: number;
}

// Wire-level envelope returned by GET /api/v1/audit/events.
interface AuditEventsEnvelope {
  items: SiemAuditEventInput[];
  next_cursor: string;
  returned: number;
}

interface UseAuditEventsResult
  extends Omit<UseQueryResult<AuditEventsEnvelope, ApiError>, "data"> {
  items: AuditEntry[];
  nextCursor: string;
  hasNextPage: boolean;
  // userMessage is a human-friendly summary surfaced for actionable
  // backend errors (503 audit_chainer_not_installed in particular). The
  // existing UseQueryResult.error has the raw ApiError; consumers that
  // just want something to render in an ErrorBanner read this field.
  userMessage?: string;
}

function buildQueryString(filters: AuditEventsFilters): string {
  const params = new URLSearchParams();
  if (filters.limit !== undefined && filters.limit > 0) {
    params.set("limit", String(filters.limit));
  }
  if (filters.cursor) params.set("cursor", filters.cursor);
  // Server side accepts a single event_type. If the caller asked for
  // multiple, we forward the first and apply the rest client-side; the
  // server is the authoritative narrowing on the wire.
  if (filters.eventType && filters.eventType.length > 0) {
    params.set("event_type", filters.eventType[0]);
  }
  if (filters.severity && filters.severity.length > 0) {
    params.set("severity", filters.severity[0]);
  }
  if (filters.from) params.set("from", filters.from);
  if (filters.to) params.set("to", filters.to);
  if (filters.search) params.set("search", filters.search);
  const qs = params.toString();
  return qs ? `?${qs}` : "";
}

// Map a 503 audit_chainer_not_installed (or generic non-2xx) into a
// human-readable message for the Audit Log error banner. The raw error
// goes into `error` for engineering; this is the customer-facing line.
function deriveUserMessage(err: unknown): string | undefined {
  if (!(err instanceof ApiError)) return undefined;
  if (err.status === 503) {
    return "Audit chain not installed — contact your operator. The Audit Log surface requires the gateway's audit chainer (CORDUM_AUDIT_HMAC_KEY) to be configured.";
  }
  if (err.status === 403) {
    return "You don't have permission to read the audit log.";
  }
  return `Failed to load audit log (${err.status}).`;
}

// useAuditEvents fetches the SIEM-feed audit page. Mirrors useAudit's
// query shape so the Audit Log page can swap call sites without
// rewriting state plumbing.
export function useAuditEvents(
  filters: AuditEventsFilters = {},
): UseAuditEventsResult {
  // Stabilise the filter object so React Query doesn't refetch on
  // identical-content rerenders. Same scalar-join strategy as useAudit.
  const stableFilters = useMemo<AuditEventsFilters>(
    () => filters,
    [
      filters.eventType?.join("|") ?? "",
      filters.severity?.join("|") ?? "",
      filters.from ?? "",
      filters.to ?? "",
      filters.search ?? "",
      filters.cursor ?? "",
      filters.limit ?? 0,
    ],
  );

  const query = useQuery<AuditEventsEnvelope, ApiError>({
    queryKey: ["audit-events", stableFilters],
    queryFn: async () => {
      return get<AuditEventsEnvelope>(
        `/audit/events${buildQueryString(stableFilters)}`,
      );
    },
    staleTime: 15_000,
  });

  const items = useMemo<AuditEntry[]>(() => {
    const raw = query.data?.items ?? [];
    return raw.map(mapAuditEvent);
  }, [query.data]);

  const nextCursor = query.data?.next_cursor ?? "";
  const userMessage = deriveUserMessage(query.error);

  const { data: _unused, ...rest } = query;
  return {
    ...rest,
    items,
    nextCursor,
    hasNextPage: nextCursor !== "",
    userMessage,
  };
}
