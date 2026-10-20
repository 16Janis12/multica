// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, screen } from "@testing-library/react";
import type { Agent, AgentRuntime } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { AgentDetailInspector } from "./agent-detail-inspector";

vi.mock("../../common/avatar-upload-control", () => ({
  AvatarUploadControl: () => <div data-testid="avatar-upload" />,
}));

vi.mock("./inspector/runtime-picker", () => ({
  RuntimePicker: () => <div data-testid="runtime-picker" />,
}));

const mockRuntime: AgentRuntime = {
  id: "runtime-1",
  workspace_id: "ws-1",
  daemon_id: "daemon-1",
  name: "Claude Runner",
  runtime_mode: "local",
  provider: "claude",
  launch_header: "",
  status: "online",
  device_info: "MacBook",
  metadata: {},
  owner_id: "user-1",
  visibility: "private",
  last_seen_at: null,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

const baseAgent: Agent = {
  id: "agent-1",
  workspace_id: "ws-1",
  name: "Pool Bot",
  description: "Test agent",
  runtime_id: "runtime-1",
  runtime_candidate_ids: ["runtime-1"],
  allow_member_candidates: false,
} as Agent;

describe("AgentDetailInspector member runtime pool contribution", () => {
  let queryClient: QueryClient;

  beforeEach(() => {
    vi.clearAllMocks();
    queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
  });

  afterEach(() => {
    cleanup();
  });

  it("allows owner/admin to toggle allow_member_candidates", async () => {
    const onUpdate = vi.fn(async () => {});
    renderWithI18n(
      <QueryClientProvider client={queryClient}>
        <AgentDetailInspector
          agent={baseAgent}
          runtime={mockRuntime}
          runtimes={[mockRuntime]}
          members={[]}
          currentUserId="user-1"
          canEdit={true}
          onUpdate={onUpdate}
        />
      </QueryClientProvider>,
    );

    const toggle = screen.getByRole("switch", { name: /allow teammates to add runtimes/i });
    expect(toggle).not.toBeDisabled();
    expect(toggle).toHaveAttribute("aria-checked", "false");

    fireEvent.click(toggle);
    expect(onUpdate).toHaveBeenCalledWith("agent-1", {
      allow_member_candidates: true,
    });
  });

  it("enables candidate pool for teammate when allow_member_candidates is true", () => {
    const onUpdate = vi.fn(async () => {});
    const agentWithMembersAllowed: Agent = {
      ...baseAgent,
      allow_member_candidates: true,
    };

    renderWithI18n(
      <QueryClientProvider client={queryClient}>
        <AgentDetailInspector
          agent={agentWithMembersAllowed}
          runtime={mockRuntime}
          runtimes={[mockRuntime]}
          members={[]}
          currentUserId="teammate-2"
          canEdit={false}
          onUpdate={onUpdate}
        />
      </QueryClientProvider>,
    );

    // Switch is disabled for non-manager
    const toggle = screen.getByRole("switch", { name: /allow teammates to add runtimes/i });
    expect(toggle).toHaveAttribute("aria-disabled", "true");
    expect(toggle).toHaveAttribute("aria-checked", "true");

    // Candidate pool trigger is enabled for teammate
    const poolTrigger = screen.getByRole("button", {
      name: /candidates in pool/i,
    });
    expect(poolTrigger).not.toBeDisabled();

    // Contributor notice is visible
    expect(
      screen.getByText(/You can contribute your own runtimes to this candidate pool/i),
    ).toBeInTheDocument();
  });

  it("disables candidate pool for teammate when allow_member_candidates is false", () => {
    const onUpdate = vi.fn(async () => {});
    renderWithI18n(
      <QueryClientProvider client={queryClient}>
        <AgentDetailInspector
          agent={baseAgent}
          runtime={mockRuntime}
          runtimes={[mockRuntime]}
          members={[]}
          currentUserId="teammate-2"
          canEdit={false}
          onUpdate={onUpdate}
        />
      </QueryClientProvider>,
    );

    const poolTrigger = screen.getByRole("button", {
      name: /candidates in pool/i,
    });
    expect(poolTrigger).toBeDisabled();
  });
});
