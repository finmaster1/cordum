import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../lib/api";
import { useToastStore } from "../state/toast";

function invalidatePools(qc: ReturnType<typeof useQueryClient>) {
  qc.invalidateQueries({ queryKey: ["pools"] });
  qc.invalidateQueries({ queryKey: ["workers"] });
  qc.invalidateQueries({ queryKey: ["config", "system", "default"] });
}

export function useCreatePool() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ name, requires, description }: { name: string; requires?: string[]; description?: string }) =>
      api.createPool(name, { requires, description }),
    onSuccess: (_, { name }) => {
      useToastStore.getState().addToast({ type: "success", title: `Pool "${name}" created` });
      invalidatePools(qc);
    },
    onError: (err: Error) => {
      useToastStore.getState().addToast({ type: "error", title: "Failed to create pool", description: err.message });
    },
  });
}

export function useUpdatePool() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ name, ...data }: { name: string; requires?: string[]; description?: string; status?: string }) =>
      api.updatePool(name, data),
    onSuccess: (_, { name }) => {
      useToastStore.getState().addToast({ type: "success", title: `Pool "${name}" updated` });
      invalidatePools(qc);
    },
    onError: (err: Error) => {
      useToastStore.getState().addToast({ type: "error", title: "Failed to update pool", description: err.message });
    },
  });
}

export function useDeletePool() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ name, force }: { name: string; force?: boolean }) =>
      api.deletePool(name, force),
    onSuccess: (_, { name }) => {
      useToastStore.getState().addToast({ type: "success", title: `Pool "${name}" deleted` });
      invalidatePools(qc);
    },
    onError: (err: Error) => {
      useToastStore.getState().addToast({ type: "error", title: "Failed to delete pool", description: err.message });
    },
  });
}

export function useDrainPool() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ name, timeout_seconds }: { name: string; timeout_seconds?: number }) =>
      api.drainPool(name, timeout_seconds ? { timeout_seconds } : undefined),
    onSuccess: (_, { name }) => {
      useToastStore.getState().addToast({ type: "success", title: `Pool "${name}" draining` });
      invalidatePools(qc);
    },
    onError: (err: Error) => {
      useToastStore.getState().addToast({ type: "error", title: "Failed to drain pool", description: err.message });
    },
  });
}

export function useAddTopicToPool() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ pool, topic }: { pool: string; topic: string }) =>
      api.addTopicToPool(pool, topic),
    onSuccess: (_, { pool, topic }) => {
      useToastStore.getState().addToast({ type: "success", title: `Topic "${topic}" added to ${pool}` });
      invalidatePools(qc);
    },
    onError: (err: Error) => {
      useToastStore.getState().addToast({ type: "error", title: "Failed to add topic", description: err.message });
    },
  });
}

export function useRemoveTopicFromPool() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ pool, topic }: { pool: string; topic: string }) =>
      api.removeTopicFromPool(pool, topic),
    onSuccess: (_, { pool, topic }) => {
      useToastStore.getState().addToast({ type: "success", title: `Topic "${topic}" removed from ${pool}` });
      invalidatePools(qc);
    },
    onError: (err: Error) => {
      useToastStore.getState().addToast({ type: "error", title: "Failed to remove topic", description: err.message });
    },
  });
}
