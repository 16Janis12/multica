import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { CreateSprintRequest, ListSprintsParams, Sprint, UpdateSprintRequest } from "../types";

export const sprintKeys = {
  all: (wsId: string) => ["sprints", wsId] as const,
  list: (wsId: string, projectId?: string) =>
    [...sprintKeys.all(wsId), "list", projectId ?? "all"] as const,
  active: (wsId: string, projectId?: string) =>
    [...sprintKeys.all(wsId), "active", projectId ?? "all"] as const,
  detail: (wsId: string, id: string) =>
    [...sprintKeys.all(wsId), "detail", id] as const,
  burndown: (wsId: string, id: string) =>
    [...sprintKeys.all(wsId), "burndown", id] as const,
};

export function sprintListOptions(
  wsId: string,
  paramsOrProjectId?: string | ListSprintsParams,
) {
  const projectId =
    typeof paramsOrProjectId === "string"
      ? paramsOrProjectId
      : paramsOrProjectId?.project_id;
  return queryOptions({
    queryKey: sprintKeys.list(wsId, projectId),
    queryFn: () =>
      api.listSprints(
        typeof paramsOrProjectId === "object"
          ? paramsOrProjectId
          : projectId
            ? { project_id: projectId }
            : undefined,
      ),
  });
}

export function activeSprintOptions(wsId: string, projectId?: string) {
  return queryOptions({
    queryKey: sprintKeys.active(wsId, projectId),
    queryFn: () => api.getActiveSprint(projectId),
  });
}

export function sprintBurndownOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: sprintKeys.burndown(wsId, id),
    queryFn: () => api.getSprintBurndown(id),
    enabled: Boolean(id),
  });
}

export async function createSprint(
  arg1: string | CreateSprintRequest,
  arg2?: CreateSprintRequest,
): Promise<Sprint> {
  const data = typeof arg1 === "string" ? arg2! : arg1;
  return api.createSprint(data);
}

export async function updateSprint(
  id: string,
  data: UpdateSprintRequest,
): Promise<Sprint> {
  return api.updateSprint(id, data);
}

export async function startSprint(id: string): Promise<Sprint> {
  return api.startSprint(id);
}

export async function completeSprint(id: string): Promise<Sprint> {
  return api.completeSprint(id);
}
