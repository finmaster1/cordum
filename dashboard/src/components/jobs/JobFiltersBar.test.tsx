import { describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { useLocation } from "react-router-dom";
import { renderWithProviders } from "@/test-utils/render";
import { JobFiltersBar, type JobFilterValues } from "./JobFiltersBar";

function FilterHarness({ onChange }: { onChange: (filters: JobFilterValues) => void }) {
  const location = useLocation();
  return (
    <>
      <JobFiltersBar onChange={onChange} />
      <output aria-label="current location">{location.search}</output>
    </>
  );
}

describe("JobFiltersBar agentId filter (task-f13505cc)", () => {
  it("debounces agent_id into the URL and clearAll resets it", async () => {
    const onChange = vi.fn();
    renderWithProviders(<FilterHarness onChange={onChange} />, { initialEntries: ["/jobs"] });

    const input = screen.getByPlaceholderText("Agent ID") as HTMLInputElement;
    fireEvent.change(input, { target: { value: "chat-assistant" } });

    await waitFor(() => {
      expect(screen.getByLabelText("current location").textContent).toContain("agentId=chat-assistant");
    }, { timeout: 1500 });
    await waitFor(() => {
      expect(onChange).toHaveBeenLastCalledWith(
        expect.objectContaining({ agentId: "chat-assistant" }),
      );
    }, { timeout: 1500 });

    const clear = screen.getByRole("button", { name: /clear all/i });
    fireEvent.click(clear);

    await waitFor(() => {
      expect(screen.getByLabelText("current location").textContent).not.toContain("agentId=");
    });
    expect((screen.getByPlaceholderText("Agent ID") as HTMLInputElement).value).toBe("");
  });
});
