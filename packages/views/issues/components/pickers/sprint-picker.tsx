"use client";

import { useState, useMemo } from "react";
import type { Sprint, UpdateIssueRequest } from "@multica/core/types";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { sprintListOptions, useCreateSprint } from "@multica/core/sprints";
import { useModalStore } from "@multica/core/modals";
import { toast } from "sonner";
import { DeferredPopup } from "../../../common/deferred-popup";
import {
  PropertyPicker,
  PickerItem,
  PickerSection,
  PickerEmpty,
  PICKER_TRIGGER_CLASS,
} from "./property-picker";
import { Zap, Plus, X } from "lucide-react";

interface SprintPickerProps {
  sprintId: string | null;
  projectId?: string;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  trigger?: React.ReactNode;
  triggerRender?: React.ReactElement<Record<string, unknown>>;
  open?: boolean;
  onOpenChange?: (v: boolean) => void;
  align?: "start" | "center" | "end";
  defaultOpen?: boolean;
}

export function SprintPicker(props: SprintPickerProps) {
  const hasDeferredTriggerContent =
    props.trigger !== undefined || props.triggerRender?.props.children != null;
  const canDefer =
    props.open === undefined &&
    props.onOpenChange === undefined &&
    !props.defaultOpen &&
    hasDeferredTriggerContent;
  if (!canDefer) {
    return <SprintPickerImpl {...props} />;
  }
  return (
    <DeferredPopup
      trigger={props.trigger}
      triggerRender={props.triggerRender}
      triggerClassName={PICKER_TRIGGER_CLASS}
    >
      {(open, onOpenChange) => (
        <SprintPickerImpl {...props} open={open} onOpenChange={onOpenChange} />
      )}
    </DeferredPopup>
  );
}

function SprintPickerImpl({
  sprintId,
  projectId,
  onUpdate,
  trigger: customTrigger,
  triggerRender,
  open: controlledOpen,
  onOpenChange: controlledOnOpenChange,
  align,
  defaultOpen = false,
}: SprintPickerProps) {
  const wsId = useWorkspaceId();
  const [internalOpen, setInternalOpen] = useState(defaultOpen);
  const [search, setSearch] = useState("");
  const open = controlledOpen ?? internalOpen;
  const setOpen = controlledOnOpenChange ?? setInternalOpen;
  const createSprint = useCreateSprint();

  const { data: sprints = [] } = useQuery(
    sprintListOptions(wsId, projectId ? { project_id: projectId } : undefined),
  );

  const currentSprint = useMemo(
    () => (sprintId ? sprints.find((s) => s.id === sprintId) : null),
    [sprints, sprintId],
  );

  const filteredSprints = useMemo(() => {
    if (!search.trim()) return sprints;
    const q = search.toLowerCase();
    return sprints.filter(
      (s) =>
        s.name.toLowerCase().includes(q) ||
        (s.goal && s.goal.toLowerCase().includes(q)),
    );
  }, [sprints, search]);

  const activeSprints = useMemo(
    () => filteredSprints.filter((s) => s.status === "active"),
    [filteredSprints],
  );
  const plannedSprints = useMemo(
    () => filteredSprints.filter((s) => s.status === "planned"),
    [filteredSprints],
  );
  const otherSprints = useMemo(
    () => filteredSprints.filter((s) => s.status !== "active" && s.status !== "planned"),
    [filteredSprints],
  );

  const trimmedQuery = search.trim();
  const canCreate =
    !!trimmedQuery &&
    !sprints.some((s) => s.name.toLowerCase() === trimmedQuery.toLowerCase());

  return (
    <PropertyPicker
      open={open}
      onOpenChange={(v) => {
        setOpen(v);
        if (!v) setSearch("");
      }}
      width="w-60"
      align={align}
      searchable
      searchPlaceholder="Search sprints…"
      onSearchChange={setSearch}
      triggerRender={triggerRender}
      trigger={
        customTrigger ??
        (currentSprint ? (
          <div className="flex items-center gap-1.5 min-w-0">
            <Zap
              className={`w-3.5 h-3.5 shrink-0 ${
                currentSprint.status === "active"
                  ? "text-emerald-500 fill-emerald-500"
                  : "text-muted-foreground"
              }`}
              aria-hidden="true"
            />
            <span className="truncate text-pretty text-xs font-medium">
              {currentSprint.name}
            </span>
          </div>
        ) : (
          <div className="flex items-center gap-1.5 text-muted-foreground text-xs">
            <Zap className="w-3.5 h-3.5 shrink-0" aria-hidden="true" />
            <span>No sprint</span>
          </div>
        ))
      }
    >
      <PickerItem
        selected={!sprintId}
        onClick={() => {
          onUpdate({ sprint_id: null });
          setOpen(false);
        }}
        data-picker-empty
      >
        <span className="flex items-center gap-2 text-muted-foreground">
          <X className="w-3.5 h-3.5 shrink-0" aria-hidden="true" />
          <span>No sprint</span>
        </span>
      </PickerItem>

      {filteredSprints.length === 0 && !canCreate && <PickerEmpty />}

      {activeSprints.length > 0 && (
        <PickerSection label="Active Sprint">
          {activeSprints.map((sprint) => (
            <PickerItem
              key={sprint.id}
              selected={sprint.id === sprintId}
              onClick={() => {
                onUpdate({ sprint_id: sprint.id });
                setOpen(false);
              }}
            >
              <div className="flex items-center justify-between w-full min-w-0 gap-2">
                <span className="truncate text-pretty text-xs font-medium">
                  {sprint.name}
                </span>
                {sprint.end_date && (
                  <span className="text-[10px] font-mono tabular-nums text-muted-foreground shrink-0">
                    Ends {sprint.end_date.slice(5)}
                  </span>
                )}
              </div>
            </PickerItem>
          ))}
        </PickerSection>
      )}

      {plannedSprints.length > 0 && (
        <PickerSection label="Upcoming">
          {plannedSprints.map((sprint) => (
            <PickerItem
              key={sprint.id}
              selected={sprint.id === sprintId}
              onClick={() => {
                onUpdate({ sprint_id: sprint.id });
                setOpen(false);
              }}
            >
              <div className="flex items-center justify-between w-full min-w-0 gap-2">
                <span className="truncate text-pretty text-xs">
                  {sprint.name}
                </span>
                {sprint.start_date && (
                  <span className="text-[10px] font-mono tabular-nums text-muted-foreground shrink-0">
                    {sprint.start_date.slice(5)}
                  </span>
                )}
              </div>
            </PickerItem>
          ))}
        </PickerSection>
      )}

      {otherSprints.length > 0 && (
        <PickerSection label="Past">
          {otherSprints.map((sprint) => (
            <PickerItem
              key={sprint.id}
              selected={sprint.id === sprintId}
              onClick={() => {
                onUpdate({ sprint_id: sprint.id });
                setOpen(false);
              }}
            >
              <div className="flex items-center justify-between w-full min-w-0 gap-2">
                <span className="truncate text-pretty text-xs text-muted-foreground">
                  {sprint.name}
                </span>
                <span className="text-[10px] uppercase font-mono tabular-nums text-muted-foreground shrink-0">
                  {sprint.status}
                </span>
              </div>
            </PickerItem>
          ))}
        </PickerSection>
      )}

      {canCreate && (
        <PickerItem
          selected={false}
          onClick={async () => {
            try {
              const sprint = await createSprint.mutateAsync({
                name: trimmedQuery,
                project_id: projectId || undefined,
              });
              onUpdate({ sprint_id: sprint.id });
              setOpen(false);
            } catch (err) {
              toast.error(err instanceof Error ? err.message : "Failed to create sprint");
            }
          }}
        >
          <Plus className="w-3.5 h-3.5 shrink-0 text-muted-foreground" />
          <span className="truncate">
            Create <span className="font-medium">&ldquo;{trimmedQuery}&rdquo;</span>
          </span>
        </PickerItem>
      )}

      <PickerItem
        selected={false}
        onClick={() => {
          setOpen(false);
          useModalStore.getState().open("create-sprint", {
            name: trimmedQuery,
            projectId,
            onSuccess: (sprint: Sprint) => onUpdate({ sprint_id: sprint.id }),
          });
        }}
      >
        <Plus className="w-3.5 h-3.5 shrink-0 text-muted-foreground" />
        <span className="text-muted-foreground">New sprint…</span>
      </PickerItem>
    </PropertyPicker>
  );
}
