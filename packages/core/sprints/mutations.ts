import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { sprintKeys } from "./queries";
import { useWorkspaceId } from "../hooks";
import type {
  CreateSprintRequest,
  UpdateSprintRequest,
  Sprint,
} from "../types";

export function useCreateSprint() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: CreateSprintRequest) => api.createSprint(data),
    onSuccess: (sprint) => {
      qc.setQueryData<Sprint[]>(sprintKeys.list(wsId, sprint.project_id ?? undefined), (old) =>
        old && !old.some((s) => s.id === sprint.id) ? [...old, sprint] : old,
      );
      qc.setQueryData<Sprint[]>(sprintKeys.list(wsId), (old) =>
        old && !old.some((s) => s.id === sprint.id) ? [...old, sprint] : old,
      );
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: sprintKeys.all(wsId) });
    },
  });
}

export function useUpdateSprint() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, ...data }: { id: string } & UpdateSprintRequest) =>
      api.updateSprint(id, data),
    onSettled: (_data, _err, { id }) => {
      qc.invalidateQueries({ queryKey: sprintKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: sprintKeys.detail(wsId, id) });
    },
  });
}

export function useStartSprint() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.startSprint(id),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: sprintKeys.all(wsId) });
    },
  });
}

export function useCompleteSprint() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.completeSprint(id),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: sprintKeys.all(wsId) });
    },
  });
}
