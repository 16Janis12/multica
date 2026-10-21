"use client";

import { useState, useMemo } from "react";
import type { UpdateIssueRequest } from "@multica/core/types";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { epicListOptions } from "@multica/core/epics";
import { DeferredPopup } from "../../../common/deferred-popup";
import {
  PropertyPicker,
  PickerItem,
  PickerEmpty,
  PICKER_TRIGGER_CLASS,
} from "./property-picker";
import { Bookmark, X } from "lucide-react";

interface EpicPickerProps {
  epicId: string | null;
  projectId?: string;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  trigger?: React.ReactNode;
  triggerRender?: React.ReactElement<Record<string, unknown>>;
  open?: boolean;
  onOpenChange?: (v: boolean) => void;
  align?: "start" | "center" | "end";
  defaultOpen?: boolean;
}

export function EpicPicker(props: EpicPickerProps) {
  const hasDeferredTriggerContent =
    props.trigger !== undefined || props.triggerRender?.props.children != null;
  const canDefer =
    props.open === undefined &&
    props.onOpenChange === undefined &&
    !props.defaultOpen &&
    hasDeferredTriggerContent;
  if (!canDefer) {
    return <EpicPickerImpl {...props} />;
  }
  return (
    <DeferredPopup
      trigger={props.trigger}
      triggerRender={props.triggerRender}
      triggerClassName={PICKER_TRIGGER_CLASS}
    >
      {(open, onOpenChange) => (
        <EpicPickerImpl {...props} open={open} onOpenChange={onOpenChange} />
      )}
    </DeferredPopup>
  );
}

function EpicPickerImpl({
  epicId,
  projectId,
  onUpdate,
  trigger: customTrigger,
  triggerRender,
  open: controlledOpen,
  onOpenChange: controlledOnOpenChange,
  align,
  defaultOpen = false,
}: EpicPickerProps) {
  const wsId = useWorkspaceId();
  const [internalOpen, setInternalOpen] = useState(defaultOpen);
  const [search, setSearch] = useState("");
  const open = controlledOpen ?? internalOpen;
  const setOpen = controlledOnOpenChange ?? setInternalOpen;

  const { data: epics = [] } = useQuery(
    epicListOptions(wsId, projectId ? { project_id: projectId } : undefined),
  );

  const currentEpic = useMemo(
    () => (epicId ? epics.find((e) => e.id === epicId) : null),
    [epics, epicId],
  );

  const filteredEpics = useMemo(() => {
    if (!search.trim()) return epics;
    const q = search.toLowerCase();
    return epics.filter(
      (e) =>
        e.title.toLowerCase().includes(q) ||
        (e.identifier && e.identifier.toLowerCase().includes(q)),
    );
  }, [epics, search]);

  return (
    <PropertyPicker
      open={open}
      onOpenChange={(v) => {
        setOpen(v);
        if (!v) setSearch("");
      }}
      width="w-56"
      align={align}
      searchable
      searchPlaceholder="Search epics…"
      onSearchChange={setSearch}
      triggerRender={triggerRender}
      trigger={
        customTrigger ??
        (currentEpic ? (
          <div className="flex items-center gap-1.5 min-w-0">
            <span
              className="w-2 h-2 rounded-full shrink-0"
              style={{ backgroundColor: currentEpic.color || "#8b5cf6" }}
              aria-hidden="true"
            />
            <span className="truncate text-pretty text-xs font-medium">
              {currentEpic.title}
            </span>
          </div>
        ) : (
          <div className="flex items-center gap-1.5 text-muted-foreground text-xs">
            <Bookmark className="w-3.5 h-3.5 shrink-0" aria-hidden="true" />
            <span>No epic</span>
          </div>
        ))
      }
    >
      <PickerItem
        selected={!epicId}
        onClick={() => {
          onUpdate({ epic_id: null });
          setOpen(false);
        }}
        data-picker-empty
      >
        <span className="flex items-center gap-2 text-muted-foreground">
          <X className="w-3.5 h-3.5 shrink-0" aria-hidden="true" />
          <span>No epic</span>
        </span>
      </PickerItem>

      {filteredEpics.length === 0 && search.trim() ? (
        <PickerEmpty />
      ) : (
        filteredEpics.map((epic) => (
          <PickerItem
            key={epic.id}
            selected={epic.id === epicId}
            onClick={() => {
              onUpdate({ epic_id: epic.id });
              setOpen(false);
            }}
          >
            <div className="flex items-center justify-between w-full min-w-0 gap-2">
              <div className="flex items-center gap-2 min-w-0">
                <span
                  className="w-2.5 h-2.5 rounded-full shrink-0"
                  style={{ backgroundColor: epic.color || "#8b5cf6" }}
                  aria-hidden="true"
                />
                <span className="truncate text-pretty text-xs">{epic.title}</span>
              </div>
              {epic.identifier && (
                <span
                  translate="no"
                  className="text-[10px] font-mono tabular-nums text-muted-foreground shrink-0 uppercase"
                >
                  {epic.identifier}
                </span>
              )}
            </div>
          </PickerItem>
        ))
      )}
    </PropertyPicker>
  );
}
