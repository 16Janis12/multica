"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Bookmark, Calendar, Check, FolderKanban } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { projectListOptions } from "@multica/core/projects/queries";
import { useCreateEpic } from "@multica/core/epics";
import type { Epic } from "@multica/core/types";
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

const COLOR_PRESETS = [
  "#6366f1", // Indigo
  "#8b5cf6", // Purple
  "#ec4899", // Pink
  "#ef4444", // Red
  "#f97316", // Orange
  "#eab308", // Yellow
  "#10b981", // Emerald
  "#06b6d4", // Cyan
  "#3b82f6", // Blue
  "#64748b", // Slate
];

interface CreateEpicModalProps {
  onClose: () => void;
  data?: Record<string, unknown> | null;
}

export function CreateEpicModal({ onClose, data }: CreateEpicModalProps) {
  const wsId = useWorkspaceId();
  const createEpic = useCreateEpic();

  const initialTitle = typeof data?.title === "string" ? data.title : "";
  const initialProjectId = typeof data?.projectId === "string" ? data.projectId : "";

  const [title, setTitle] = useState(initialTitle);
  const [description, setDescription] = useState("");
  const [color, setColor] = useState(COLOR_PRESETS[0]);
  const [projectId, setProjectId] = useState(initialProjectId);
  const [startDate, setStartDate] = useState("");
  const [dueDate, setDueDate] = useState("");
  const [status, setStatus] = useState<"planned" | "in_progress" | "completed" | "cancelled">("planned");

  const { data: projects = [] } = useQuery(projectListOptions(wsId));

  const canSubmit = title.trim().length > 0 && !createEpic.isPending;

  const handleSubmit = async (e?: React.FormEvent) => {
    e?.preventDefault();
    if (!canSubmit) return;

    try {
      const epic = await createEpic.mutateAsync({
        title: title.trim(),
        description: description.trim() || undefined,
        color: color || undefined,
        project_id: projectId || undefined,
        status,
        start_date: startDate || undefined,
        due_date: dueDate || undefined,
      });

      toast.success("Epic created");
      if (typeof data?.onSuccess === "function") {
        (data.onSuccess as (epic: Epic) => void)(epic);
      }
      onClose();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to create epic");
    }
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-[480px]">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-base font-semibold">
            <Bookmark className="size-4 text-brand" />
            Create Epic
          </DialogTitle>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4 pt-2">
          <div className="space-y-1.5">
            <Label htmlFor="epic-title" className="text-xs font-medium">
              Title <span className="text-destructive">*</span>
            </Label>
            <Input
              id="epic-title"
              autoFocus
              placeholder="e.g. Q4 Platform Modernization"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              className="h-9 text-sm"
              onKeyDown={(e) => {
                if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
                  handleSubmit();
                }
              }}
            />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="epic-description" className="text-xs font-medium">
              Description
            </Label>
            <Textarea
              id="epic-description"
              placeholder="What is the goal and scope of this epic?"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              className="resize-none text-xs min-h-[72px]"
              rows={3}
            />
          </div>

          <div className="space-y-1.5">
            <Label className="text-xs font-medium">Color</Label>
            <div className="flex items-center gap-2 flex-wrap">
              {COLOR_PRESETS.map((preset) => (
                <button
                  key={preset}
                  type="button"
                  onClick={() => setColor(preset)}
                  className="size-6 rounded-full flex items-center justify-center transition-transform hover:scale-110 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  style={{ backgroundColor: preset }}
                  aria-label={`Select color ${preset}`}
                >
                  {color === preset && <Check className="size-3.5 text-white drop-shadow" />}
                </button>
              ))}
            </div>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label htmlFor="epic-project" className="text-xs font-medium flex items-center gap-1.5">
                <FolderKanban className="size-3 text-muted-foreground" />
                Project
              </Label>
              <select
                id="epic-project"
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

            <div className="space-y-1.5">
              <Label htmlFor="epic-status" className="text-xs font-medium">
                Status
              </Label>
              <select
                id="epic-status"
                value={status}
                onChange={(e) =>
                  setStatus(e.target.value as "planned" | "in_progress" | "completed" | "cancelled")
                }
                className="w-full h-8 rounded-md border border-input bg-background px-2.5 py-1 text-xs text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                <option value="planned">Planned</option>
                <option value="in_progress">In Progress</option>
                <option value="completed">Completed</option>
                <option value="cancelled">Cancelled</option>
              </select>
            </div>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label htmlFor="epic-start-date" className="text-xs font-medium flex items-center gap-1.5">
                <Calendar className="size-3 text-muted-foreground" />
                Start Date
              </Label>
              <Input
                id="epic-start-date"
                type="date"
                value={startDate}
                onChange={(e) => setStartDate(e.target.value)}
                className="h-8 text-xs"
              />
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="epic-due-date" className="text-xs font-medium flex items-center gap-1.5">
                <Calendar className="size-3 text-muted-foreground" />
                Due Date
              </Label>
              <Input
                id="epic-due-date"
                type="date"
                value={dueDate}
                onChange={(e) => setDueDate(e.target.value)}
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
              {createEpic.isPending ? "Creating..." : "Create Epic"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
