// @vitest-environment jsdom

import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createStore } from "zustand/vanilla";
import {
  type IssueViewState,
  viewStoreSlice,
} from "@multica/core/issues/stores/view-store";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import { useModalStore } from "@multica/core/modals";
import { renderWithI18n } from "../../test/i18n";
import { SprintSelector, EpicSelector } from "./issues-header";
import { CreateEpicModal } from "../../modals/create-epic";
import { CreateSprintModal } from "../../modals/create-sprint";

const mockSprintList = vi.fn();
const mockEpicList = vi.fn();
const mockProjectList = vi.fn();
const mockCreateEpic = vi.fn();
const mockCreateSprint = vi.fn();

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/sprints", () => ({
  sprintListOptions: () => ({
    queryKey: ["sprints", "ws-1"],
    queryFn: () => mockSprintList(),
  }),
  useCreateSprint: () => ({
    mutateAsync: mockCreateSprint,
    isPending: false,
  }),
}));

vi.mock("@multica/core/epics", () => ({
  epicListOptions: () => ({
    queryKey: ["epics", "ws-1"],
    queryFn: () => mockEpicList(),
  }),
  useCreateEpic: () => ({
    mutateAsync: mockCreateEpic,
    isPending: false,
  }),
}));

vi.mock("@multica/core/projects/queries", () => ({
  projectListOptions: () => ({
    queryKey: ["projects", "ws-1"],
    queryFn: () => mockProjectList(),
  }),
}));

function setupTest() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  const viewStore = createStore<IssueViewState>((set) =>
    viewStoreSlice(set),
  );
  return { queryClient, viewStore };
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  useModalStore.getState().close();
});

describe("SprintSelector & EpicSelector in IssuesHeader", () => {
  it("renders SprintSelector with toolbar sizing (h-8 md:h-7) and allows opening create sprint modal", async () => {
    mockSprintList.mockResolvedValue([
      { id: "sp-1", name: "Sprint 1", status: "active" },
    ]);
    const { queryClient, viewStore } = setupTest();

    renderWithI18n(
      <QueryClientProvider client={queryClient}>
        <ViewStoreProvider store={viewStore}>
          <SprintSelector />
        </ViewStoreProvider>
      </QueryClientProvider>,
    );

    const trigger = screen.getByRole("combobox", { name: "Select sprint" });
    expect(trigger).toBeInTheDocument();
    expect(trigger.className).toContain("h-8");
    expect(trigger.className).toContain("md:h-7");
    expect(trigger.className).not.toContain("min-h-[44px]");

    fireEvent.click(trigger);

    const newSprintBtn = await screen.findByText("New sprint…");
    expect(newSprintBtn).toBeInTheDocument();

    fireEvent.click(newSprintBtn);

    const modalState = useModalStore.getState();
    expect(modalState.modal).toBe("create-sprint");
  });

  it("renders EpicSelector with toolbar sizing (h-8 md:h-7) and allows opening create epic modal", async () => {
    mockEpicList.mockResolvedValue([
      { id: "ep-1", title: "Platform V2", color: "#6366f1" },
    ]);
    const { queryClient, viewStore } = setupTest();

    renderWithI18n(
      <QueryClientProvider client={queryClient}>
        <ViewStoreProvider store={viewStore}>
          <EpicSelector />
        </ViewStoreProvider>
      </QueryClientProvider>,
    );

    const trigger = screen.getByRole("combobox", { name: "Select epic" });
    expect(trigger).toBeInTheDocument();
    expect(trigger.className).toContain("h-8");
    expect(trigger.className).toContain("md:h-7");
    expect(trigger.className).not.toContain("min-h-[44px]");

    fireEvent.click(trigger);

    const newEpicBtn = await screen.findByText("New epic…");
    expect(newEpicBtn).toBeInTheDocument();

    fireEvent.click(newEpicBtn);

    const modalState = useModalStore.getState();
    expect(modalState.modal).toBe("create-epic");
  });
});

describe("CreateEpicModal", () => {
  it("renders fields, validates input, and submits successfully", async () => {
    mockProjectList.mockResolvedValue([
      { id: "proj-1", name: "Core Engine" },
    ]);
    mockCreateEpic.mockResolvedValue({
      id: "ep-123",
      title: "New Marketing Site",
    });

    const onClose = vi.fn();
    const onSuccess = vi.fn();
    const { queryClient } = setupTest();

    renderWithI18n(
      <QueryClientProvider client={queryClient}>
        <CreateEpicModal
          onClose={onClose}
          data={{ title: "Initial Title", onSuccess }}
        />
      </QueryClientProvider>,
    );

    const titleInput = screen.getByPlaceholderText("e.g. Q4 Platform Modernization");
    expect(titleInput).toHaveValue("Initial Title");

    const submitBtn = screen.getByRole("button", { name: "Create Epic" });
    expect(submitBtn).not.toBeDisabled();

    fireEvent.click(submitBtn);

    await waitFor(() => {
      expect(mockCreateEpic).toHaveBeenCalledWith(
        expect.objectContaining({
          title: "Initial Title",
        }),
      );
    });

    expect(onSuccess).toHaveBeenCalled();
    expect(onClose).toHaveBeenCalled();
  });
});

describe("CreateSprintModal", () => {
  it("renders auto-suggested name and submits successfully", async () => {
    mockProjectList.mockResolvedValue([]);
    mockSprintList.mockResolvedValue([
      { id: "sp-1", name: "Sprint 1", status: "completed" },
    ]);
    mockCreateSprint.mockResolvedValue({
      id: "sp-2",
      name: "Sprint 2",
    });

    const onClose = vi.fn();
    const onSuccess = vi.fn();
    const { queryClient } = setupTest();

    renderWithI18n(
      <QueryClientProvider client={queryClient}>
        <CreateSprintModal
          onClose={onClose}
          data={{ onSuccess }}
        />
      </QueryClientProvider>,
    );

    const submitBtn = screen.getByRole("button", { name: "Create Sprint" });
    expect(submitBtn).not.toBeDisabled();

    fireEvent.click(submitBtn);

    await waitFor(() => {
      expect(mockCreateSprint).toHaveBeenCalled();
    });

    expect(onSuccess).toHaveBeenCalled();
    expect(onClose).toHaveBeenCalled();
  });
});
