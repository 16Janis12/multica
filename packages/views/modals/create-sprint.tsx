"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Calendar, FolderKanban, Zap } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { projectListOptions } from "@multica/core/projects/queries";
import { sprintListOptions, useCreateSprint } from "@multica/core/sprints";
import type { Sprint } from "@multica/core/types";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Label } from "@multica/ui/components/ui/label";
import { toast } from "sonner";

interface CreateSprintModalProps {
  onClose: () => void;
  data?: Record<string, unknown> | null;
}

export function CreateSprintModal({ onClose, data }: CreateSprintModalProps) {
  const wsId = useWorkspaceId();
  const createSprint = useCreateSprint();

  const initialName = typeof data?.name === "string" ? data.name : "";
  const initialProjectId = typeof data?.projectId === "string" ? data.projectId : "";

  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const { data: existingSprints = [] } = useQuery(
    sprintListOptions(wsId, initialProjectId ? { project_id: initialProjectId } : undefined),
  );

  const defaultSuggestedName = useMemo(() => {
    if (initialName) return initialName;
    return `Sprint ${existingSprints.length + 1}`;
  }, [initialName, existingSprints.length]);

  const [name, setName] = useState(initialName || defaultSuggestedName);
  const [goal, setGoal] = useState("");
  const [projectId, setProjectId] = useState(initialProjectId);

  const todayIso = useMemo(() => new Date().toISOString().slice(0, 10), []);
  const twoWeeksLaterIso = useMemo(
    () => new Date(Date.now() + 14 * 24 * 60 * 60 * 1000).toISOString().slice(0, 10),
    [],
  );

  const [startDate, setStartDate] = useState(todayIso);
  const [endDate, setEndDate] = useState(twoWeeksLaterIso);

  const canSubmit = name.trim().length > 0 && !createSprint.isPending;

  const handleSubmit = async (e?: React.FormEvent) => {
    e?.preventDefault();
    if (!canSubmit) return;

    try {
      const sprint = await createSprint.mutateAsync({
        name: name.trim(),
        goal: goal.trim() || undefined,
        project_id: projectId || undefined,
        start_date: startDate || undefined,
        end_date: endDate || undefined,
      });

      toast.success("Sprint created");
      if (typeof data?.onSuccess === "function") {
        (data.onSuccess as (sprint: Sprint) => void)(sprint);
      }
      onClose();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to create sprint");
    }
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-[480px]">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-base font-semibold">
            <Zap className="size-4 text-emerald-500 fill-emerald-500" />
            Create Sprint
          </DialogTitle>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4 pt-2">
          <div className="space-y-1.5">
            <Label htmlFor="sprint-name" className="text-xs font-medium">
              Sprint Name <span className="text-destructive">*</span>
            </Label>
            <Input
              id="sprint-name"
              autoFocus
              placeholder="e.g. Sprint 1"
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="h-9 text-sm"
              onKeyDown={(e) => {
                if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
                  handleSubmit();
                }
              }}
            />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="sprint-goal" className="text-xs font-medium">
              Sprint Goal
            </Label>
            <Textarea
              id="sprint-goal"
              placeholder="What do we aim to accomplish in this sprint?"
              value={goal}
              onChange={(e) => setGoal(e.target.value)}
              className="resize-none text-xs min-h-[72px]"
              rows={3}
            />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="sprint-project" className="text-xs font-medium flex items-center gap-1.5">
              <FolderKanban className="size-3 text-muted-foreground" />
              Project
            </Label>
            <select
              id="sprint-project"
              value={projectId}
              onChange={(e) => setProjectId(e.target.value)}
              className="w-full h-8 rounded-md border border-input bg-background px-2.5 py-1 text-xs text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              <option value="">No Project (Workspace-wide)</option>
              {projects.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.title}
                </option>
              ))}
            </select>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label htmlFor="sprint-start-date" className="text-xs font-medium flex items-center gap-1.5">
                <Calendar className="size-3 text-muted-foreground" />
                Start Date
              </Label>
              <Input
                id="sprint-start-date"
                type="date"
                value={startDate}
                onChange={(e) => setStartDate(e.target.value)}
                className="h-8 text-xs"
              />
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="sprint-end-date" className="text-xs font-medium flex items-center gap-1.5">
                <Calendar className="size-3 text-muted-foreground" />
                End Date
              </Label>
              <Input
                id="sprint-end-date"
                type="date"
                value={endDate}
                onChange={(e) => setEndDate(e.target.value)}
                className="h-8 text-xs"
              />
            </div>
          </div>

          <DialogFooter className="pt-2">
            <Button type="button" variant="outline" size="sm" onClick={onClose}>
              Cancel
            </Button>
            <Button
              type="submit"
              size="sm"
              disabled={!canSubmit}
              className="bg-brand text-white hover:bg-brand/90"
            >
              {createSprint.isPending ? "Creating..." : "Create Sprint"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
