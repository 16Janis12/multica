"use client";

import { useMemo, useState } from "react";
import { ChevronDown, Layers, Monitor, X } from "lucide-react";
import { isRuntimeUsableForUser } from "@multica/core/runtimes";
import type { AgentRuntime, MemberWithUser } from "@multica/core/types";
import { ProviderLogo } from "../../../runtimes/components/provider-logo";
import {
  buildRuntimeMachines,
  runtimeRowLabel,
} from "../../../runtimes/components/runtime-machines";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { cn } from "@multica/ui/lib/utils";

interface RuntimeCandidatePoolPickerProps {
  candidateIds: string[];
  defaultRuntimeId?: string;
  runtimes: AgentRuntime[];
  members: MemberWithUser[];
  currentUserId: string | null;
  canEdit?: boolean;
  onChange: (candidateIds: string[]) => Promise<void> | void;
}

export function RuntimeCandidatePoolPicker({
  candidateIds,
  defaultRuntimeId,
  runtimes,
  currentUserId,
  canEdit = true,
  onChange,
}: RuntimeCandidatePoolPickerProps) {
  const [open, setOpen] = useState(false);

  const usableRuntimes = useMemo(() => {
    return runtimes.filter((r) => isRuntimeUsableForUser(r, currentUserId));
  }, [runtimes, currentUserId]);

  const allMachines = useMemo(
    () => buildRuntimeMachines(usableRuntimes, { now: Date.now(), currentUserId }),
    [usableRuntimes, currentUserId],
  );

  const selectedRuntimes = useMemo(() => {
    return runtimes.filter((r) => candidateIds.includes(r.id));
  }, [runtimes, candidateIds]);

  const toggleCandidate = async (runtimeId: string) => {
    if (!canEdit) return;
    const exists = candidateIds.includes(runtimeId);
    let updated: string[];
    if (exists) {
      updated = candidateIds.filter((id) => id !== runtimeId);
    } else {
      updated = [...candidateIds, runtimeId];
    }
    await onChange(updated);
  };

  const clearAll = async (e: React.MouseEvent) => {
    e.stopPropagation();
    if (!canEdit) return;
    await onChange([]);
  };

  const getRuntimeDisplay = (r: AgentRuntime) => {
    const machine = allMachines.find((m) =>
      m.runtimes.some((rt) => rt.id === r.id),
    );
    const label = runtimeRowLabel(r, machine?.title ?? "");
    const title = machine?.title ?? "";
    return label !== title && title ? `${label} · ${title}` : label;
  };

  return (
    <div className="flex flex-col gap-2">
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger
          disabled={!canEdit}
          className={cn(
            "flex min-h-10 w-full items-center justify-between gap-2 rounded-lg border border-input bg-transparent px-3 py-1.5 text-left text-body transition-colors",
            canEdit
              ? "hover:bg-muted focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
              : "cursor-not-allowed opacity-60",
          )}
        >
          <div className="flex flex-1 flex-wrap items-center gap-1.5 min-w-0">
            {selectedRuntimes.length === 0 ? (
              <span className="flex items-center gap-2 text-muted-foreground">
                <Layers className="size-4 shrink-0" />
                <span>Select candidate runtimes for dynamic pool...</span>
              </span>
            ) : (
              <div className="flex flex-wrap items-center gap-1.5">
                <span className="inline-flex items-center gap-1 text-xs font-medium text-foreground bg-muted px-2 py-0.5 rounded">
                  <Layers className="size-3.5" />
                  <span>{selectedRuntimes.length} candidates in pool</span>
                </span>
                {selectedRuntimes.slice(0, 2).map((r) => (
                  <span
                    key={r.id}
                    className="inline-flex items-center gap-1 text-xs bg-muted/50 border border-border px-2 py-0.5 rounded max-w-[160px] truncate"
                  >
                    <ProviderLogo provider={r.provider} className="size-3 shrink-0" />
                    <span className="truncate">{getRuntimeDisplay(r)}</span>
                  </span>
                ))}
                {selectedRuntimes.length > 2 && (
                  <span className="text-xs text-muted-foreground">
                    +{selectedRuntimes.length - 2} more
                  </span>
                )}
              </div>
            )}
          </div>
          <div className="flex items-center gap-1 shrink-0">
            {canEdit && selectedRuntimes.length > 0 && (
              <span
                role="button"
                tabIndex={0}
                onClick={clearAll}
                className="rounded p-0.5 text-muted-foreground hover:bg-muted hover:text-foreground"
                title="Clear candidate pool"
              >
                <X className="size-3.5" />
              </span>
            )}
            <ChevronDown className="size-4 text-muted-foreground" />
          </div>
        </PopoverTrigger>

        <PopoverContent
          align="start"
          className="w-[var(--anchor-width)] min-w-[20rem] max-w-lg p-2 max-h-80 overflow-y-auto"
        >
          <div className="space-y-3">
            <div className="text-xs font-medium text-muted-foreground px-1">
              Select runtimes to include in this agent's dynamic dispatch pool:
            </div>
            {allMachines.length === 0 ? (
              <div className="p-3 text-center text-xs text-muted-foreground">
                No usable runtimes found in workspace.
              </div>
            ) : (
              allMachines.map((machine) => (
                <div key={machine.id} className="space-y-1">
                  <div className="flex items-center justify-between px-1.5 py-1 text-xs font-semibold text-muted-foreground bg-muted/40 rounded">
                    <div className="flex items-center gap-1.5 truncate">
                      <Monitor className="size-3.5 shrink-0" />
                      <span className="truncate">{machine.title}</span>
                    </div>
                    <span className="text-[10px] text-muted-foreground">
                      {machine.onlineCount}/{machine.runtimes.length} online
                    </span>
                  </div>
                  <div className="pl-2 space-y-0.5">
                    {machine.runtimes.map((r) => {
                      const isChecked = candidateIds.includes(r.id);
                      const isDefault = defaultRuntimeId === r.id;
                      const isOnline = r.status === "online";
                      return (
                        <label
                          key={r.id}
                          className="flex items-center gap-2.5 px-2 py-1.5 rounded hover:bg-muted cursor-pointer text-xs"
                        >
                          <Checkbox
                            checked={isChecked}
                            onCheckedChange={() => toggleCandidate(r.id)}
                            disabled={!canEdit}
                          />
                          <ProviderLogo provider={r.provider} className="size-3.5 shrink-0" />
                          <span className="flex-1 truncate font-medium">
                            {runtimeRowLabel(r, machine.title)}
                            {isDefault && (
                              <span className="ml-1.5 text-[10px] text-muted-foreground font-normal">
                                (default)
                              </span>
                            )}
                          </span>
                          <span
                            className={cn(
                              "size-2 rounded-full shrink-0",
                              isOnline ? "bg-success" : "bg-muted-foreground/30",
                            )}
                            title={isOnline ? "Online" : "Offline"}
                          />
                        </label>
                      );
                    })}
                  </div>
                </div>
              ))
            )}
          </div>
        </PopoverContent>
      </Popover>
    </div>
  );
}
