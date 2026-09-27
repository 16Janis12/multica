import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { AgentRuntime } from "@multica/core/types";
import { RuntimeCapacityCard } from "./runtime-capacity-card";

vi.mock("@multica/core/runtimes/queries", () => ({
  runtimeCapacityOptions: (runtimeId: string) => ({
    queryKey: ["runtimes", "capacity", runtimeId],
    queryFn: async () => ({
      provider: "antigravity",
      effective_tier: "AMPLE",
      session_5h: {
        id: "session-5h",
        label: "5-Hour Session",
        used_percent: 15.0,
        remaining_percent: 85.0,
        time_until_reset: "2h 45m",
        tier: "AMPLE",
      },
      weekly_7d: {
        id: "weekly-7d",
        label: "7-Day Weekly Limit",
        used_percent: 40.0,
        remaining_percent: 60.0,
        time_until_reset: "3d 12h",
        tier: "AMPLE",
      },
      checked_at: new Date().toISOString(),
      supported: true,
    }),
  }),
}));

vi.mock("../../i18n", () => ({
  useTimeAgo: () => () => "just now",
  useT: () => ({ t: (fn: any) => fn({}) }),
}));

const mockRuntime: AgentRuntime = {
  id: "rt-123",
  workspace_id: "ws-123",
  daemon_id: "daemon-123",
  name: "Antigravity Node",
  runtime_mode: "local",
  launch_header: "",
  provider: "antigravity",
  status: "online",
  device_info: "host.local",
  metadata: {},
  owner_id: "user-123",
  visibility: "public",
  created_at: new Date().toISOString(),
  updated_at: new Date().toISOString(),
  last_seen_at: new Date().toISOString(),
};

describe("RuntimeCapacityCard", () => {
  it("renders live quota metrics and windows", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });

    render(
      <QueryClientProvider client={queryClient}>
        <RuntimeCapacityCard runtime={mockRuntime} />
      </QueryClientProvider>,
    );

    expect(
      screen.getByText("Runtime Capacity & Rate Limits"),
    ).toBeInTheDocument();
    const badges = await screen.findAllByText("Ample Capacity");
    expect(badges.length).toBeGreaterThan(0);
    expect(screen.getByText("5-Hour Session")).toBeInTheDocument();
    expect(screen.getByText("85.0%")).toBeInTheDocument();
    expect(screen.getByText("Resets in 2h 45m")).toBeInTheDocument();
    expect(screen.getByText("7-Day Weekly Limit")).toBeInTheDocument();
    expect(screen.getByText("60.0%")).toBeInTheDocument();
  });
});
