import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { epicKeys } from "./queries";
import { useWorkspaceId } from "../hooks";
import type {
  CreateEpicRequest,
  UpdateEpicRequest,
  Epic,
} from "../types";

export function useCreateEpic() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: CreateEpicRequest) => api.createEpic(data),
    onSuccess: (epic) => {
      qc.setQueryData<Epic[]>(epicKeys.list(wsId, epic.project_id ?? undefined), (old) =>
        old && !old.some((e) => e.id === epic.id) ? [...old, epic] : old,
      );
      qc.setQueryData<Epic[]>(epicKeys.list(wsId), (old) =>
        old && !old.some((e) => e.id === epic.id) ? [...old, epic] : old,
      );
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: epicKeys.all(wsId) });
    },
  });
}

export function useUpdateEpic() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, ...data }: { id: string } & UpdateEpicRequest) =>
      api.updateEpic(id, data),
    onMutate: async ({ id, ...data }) => {
      const listKey = epicKeys.list(wsId);
      await qc.cancelQueries({ queryKey: listKey });
      const prevList = qc.getQueryData<Epic[]>(listKey);
      qc.setQueryData<Epic[]>(listKey, (old) =>
        old ? old.map((e) => (e.id === id ? { ...e, ...data } : e)) : old,
      );
      return { prevList };
    },
    onError: (_err, _vars, context) => {
      if (context?.prevList) {
        qc.setQueryData(epicKeys.list(wsId), context.prevList);
      }
    },
    onSettled: (_data, _err, { id }) => {
      qc.invalidateQueries({ queryKey: epicKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: epicKeys.detail(wsId, id) });
    },
  });
}

export function useDeleteEpic() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.deleteEpic(id),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: epicKeys.all(wsId) });
    },
  });
}
