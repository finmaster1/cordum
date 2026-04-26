import { describe, expect, it, beforeEach } from "vitest";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { http, HttpResponse, server } from "@/test-utils/msw";
import { renderWithProviders } from "@/test-utils/render";
import JobsPage from "./JobsPage";

function backendJob(overrides: Record<string, unknown> = {}) {
  return {
    id: "job-chat-1",
    state: "succeeded",
    topic: "job.mock-bank.transfer",
    tenant: "tenant-default",
    actor_id: "chat-assistant@tenant-default",
    actor_type: "agent",
    attempts: 1,
    updated_at: Date.parse("2026-04-26T07:00:00Z"),
    trace_id: "trace-chat-1",
    ...overrides,
  };
}

function mockJobs(items: Array<Record<string, unknown>>) {
  server.use(
    http.get("*/api/v1/jobs", () =>
      HttpResponse.json({
        items,
        total: items.length,
      }),
    ),
  );
}

function renderJobs(initialEntry = "/jobs") {
  return renderWithProviders(<JobsPage />, { initialEntries: [initialEntry] });
}

describe("JobsPage agent_id column and filtering (task-f13505cc)", () => {
  beforeEach(() => {
    server.resetHandlers();
  });

  it("renders a visible Agent column header", async () => {
    mockJobs([backendJob()]);
    renderJobs();

    const agentHeader = await screen.findByRole("columnheader", { name: /agent/i });
    expect(agentHeader).not.toBeNull();
  });

  it("renders the chat-assistant agent name truncated before @ with full tooltip", async () => {
    mockJobs([backendJob()]);
    renderJobs();

    await screen.findByRole("columnheader", { name: /agent/i });
    const agentCell = screen.getByLabelText("chat-assistant@tenant-default");
    expect(agentCell).not.toBeNull();
    expect(agentCell.getAttribute("title")).toBe("chat-assistant@tenant-default");
    expect(within(agentCell).queryByText("chat-assistant")).not.toBeNull();
  });

  it("renders an em dash when actor_id is missing", async () => {
    mockJobs([backendJob({ id: "job-system", actor_id: undefined, tenant: undefined })]);
    renderJobs();

    await screen.findByRole("columnheader", { name: /agent/i });
    const row = screen.getByText("job-system").closest("tr");
    expect(row).not.toBeNull();
    const cells = within(row as HTMLTableRowElement).getAllByRole("cell");
    expect(cells[4].textContent).toBe("—");
  });

  it("adds a copilot badge only for chat-assistant identities", async () => {
    mockJobs([
      backendJob({ id: "job-chat", actor_id: "chat-assistant@tenant-default" }),
      backendJob({ id: "job-other", actor_id: "chat-assistant-malicious@tenant-default" }),
    ]);
    renderJobs();

    await screen.findByText("job-chat");
    expect(screen.getAllByText("copilot").length).toBe(1);
  });

  it("filters by agentId from the URL", async () => {
    mockJobs([
      backendJob({ id: "job-chat", actor_id: "chat-assistant@tenant-default" }),
      backendJob({ id: "job-workflow", actor_id: "workflow-runner@tenant-default" }),
    ]);
    renderJobs("/jobs?agentId=chat-assistant");

    await waitFor(() => {
      expect(screen.queryByText("job-chat")).not.toBeNull();
    });
    expect(screen.queryByText("job-workflow")).toBeNull();
  });

  it("sorts by Agent column on repeated header clicks", async () => {
    mockJobs([
      backendJob({ id: "job-b", actor_id: "zeta-agent@tenant-default" }),
      backendJob({ id: "job-a", actor_id: "alpha-agent@tenant-default" }),
    ]);
    const { container } = renderJobs();

    const agentHeader = await screen.findByRole("columnheader", { name: /agent/i });
    expect(agentHeader.getAttribute("aria-sort")).toBe("none");

    fireEvent.click(agentHeader);
    expect(agentHeader.getAttribute("aria-sort")).toBe("descending");

    fireEvent.click(agentHeader);
    expect(agentHeader.getAttribute("aria-sort")).toBe("ascending");

    await waitFor(() => {
      const bodyRows = Array.from(container.querySelectorAll("tbody tr"));
      expect(bodyRows[0]?.textContent).toContain("job-a");
    });
  });
});
