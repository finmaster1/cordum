import { describe, it, expect } from "vitest";
import { render, screen, within } from "@testing-library/react";
import { ApprovalInlinePrompt } from "./ApprovalInlinePrompt";
import type { AttachedToolCall } from "@/types/chatAssistant";

function makePendingCall(overrides?: Partial<AttachedToolCall>): AttachedToolCall {
  return {
    toolCallId: "tc-1",
    tool: "cordum_approve_job",
    args: { job_id: "job-7", reason: "scheduled rotation" },
    approval: { approvalId: "appr-1", status: "pending" },
    ...overrides,
  };
}

describe("ApprovalInlinePrompt — overreliance affordances", () => {
  it("names the defense layer that paused the call", () => {
    render(<ApprovalInlinePrompt toolCall={makePendingCall()} />);
    expect(screen.getByText(/cordum approval gate paused this call/i)).toBeTruthy();
  });

  it("renders the verify-before-approving guidance copy", () => {
    render(<ApprovalInlinePrompt toolCall={makePendingCall()} />);
    expect(
      screen.getByText(/not allowed to run this mutating tool without an explicit human decision/i),
    ).toBeTruthy();
    expect(screen.getByText(/only approve when you are confident/i)).toBeTruthy();
  });

  it("shows the exact tool call arguments so the operator can verify", () => {
    render(<ApprovalInlinePrompt toolCall={makePendingCall()} />);
    const argBlock = screen.getByLabelText("tool call arguments");
    expect(argBlock.textContent).toContain("job-7");
    expect(argBlock.textContent).toContain("scheduled rotation");
  });

  it("uses a verify-and-approve label, not a bare approve", () => {
    render(<ApprovalInlinePrompt toolCall={makePendingCall()} />);
    const region = screen.getByRole("region", { name: /approval required/i });
    const approveButton = within(region).getByRole("button", { name: /approve cordum_approve_job/i });
    expect(approveButton.textContent?.toLowerCase()).toContain("verify and approve");
  });

  it("notes the audit chain records every decision", () => {
    render(<ApprovalInlinePrompt toolCall={makePendingCall()} />);
    expect(screen.getByText(/audit chain records every decision/i)).toBeTruthy();
  });

  it("renders nothing when the approval is not pending", () => {
    const { container } = render(
      <ApprovalInlinePrompt
        toolCall={makePendingCall({ approval: { approvalId: "appr-1", status: "resolved" } })}
      />,
    );
    expect(container.firstChild).toBeNull();
  });

  it("renders nothing when there is no approval attached", () => {
    const { container } = render(
      <ApprovalInlinePrompt toolCall={makePendingCall({ approval: undefined })} />,
    );
    expect(container.firstChild).toBeNull();
  });
});
