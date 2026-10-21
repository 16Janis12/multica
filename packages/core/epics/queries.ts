import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { CreateEpicRequest, Epic, ListEpicsParams, UpdateEpicRequest } from "../types";

export const epicKeys = {
  all: (wsId: string) => ["epics", wsId] as const,
  list: (wsId: string, projectId?: string) =>
    [...epicKeys.all(wsId), "list", projectId ?? "all"] as const,
  detail: (wsId: string, id: string) =>
    [...epicKeys.all(wsId), "detail", id] as const,
};

export function epicListOptions(
  wsId: string,
  paramsOrProjectId?: string | ListEpicsParams,
) {
  const projectId =
    typeof paramsOrProjectId === "string"
      ? paramsOrProjectId
      : paramsOrProjectId?.project_id;
  return queryOptions({
    queryKey: epicKeys.list(wsId, projectId),
    queryFn: () =>
      api.listEpics(
        typeof paramsOrProjectId === "object"
          ? paramsOrProjectId
          : projectId
            ? { project_id: projectId }
            : undefined,
      ),
  });
}

export function epicDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: epicKeys.detail(wsId, id),
    queryFn: () => api.getEpic(id),
    enabled: Boolean(id),
  });
}

export async function createEpic(
  arg1: string | CreateEpicRequest,
  arg2?: CreateEpicRequest,
): Promise<Epic> {
  const data = typeof arg1 === "string" ? arg2! : arg1;
  return api.createEpic(data);
}

export async function updateEpic(
  id: string,
  data: UpdateEpicRequest,
): Promise<Epic> {
  return api.updateEpic(id, data);
}

export async function deleteEpic(id: string): Promise<void> {
  return api.deleteEpic(id);
}
