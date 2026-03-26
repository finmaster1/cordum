import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import type { Node, Edge } from "reactflow";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";

import type { Workflow, WorkflowRun } from "@/api/types";
import {
  useWorkflow,
  useRuns,
  useRun,
  useCreateWorkflow,
  useUpdateWorkflow,
  useDeleteWorkflow,
  useStartRun,
} from "@/hooks/useWorkflows";

import type { UnifiedNodeData, StudioMode, StudioGraphData } from "./types";
import { definitionToGraph, graphToDefinition } from "./graphBridge";
import { StudioToolbar } from "./StudioToolbar";
import { StudioSidebar } from "./StudioSidebar";
import { StudioCanvas } from "./StudioCanvas";
import { StudioNodePanel } from "./StudioNodePanel";

// ---------------------------------------------------------------------------
// Empty graph for new workflows
// ---------------------------------------------------------------------------

const EMPTY_GRAPH: StudioGraphData = { nodes: [], edges: [] };

// ---------------------------------------------------------------------------
// WorkflowStudio
// ---------------------------------------------------------------------------

export function WorkflowStudio() {
  const { id: workflowId } = useParams<{ id: string }>();
  const [searchParams, setSearchParams] = useSearchParams();
  const navigate = useNavigate();

  const isNew = !workflowId || workflowId === "new";

  // --- Mode ---
  const initialMode: StudioMode = isNew ? "edit" : (searchParams.get("mode") as StudioMode) || "view";
  const [mode, setMode] = useState<StudioMode>(initialMode);

  // --- Workflow data ---
  const { data: workflow, isLoading: isLoadingWorkflow } = useWorkflow(isNew ? null : workflowId);
  const { data: runs = [] } = useRuns(isNew ? null : workflowId, { limit: 20 });

  // --- Selected run ---
  const selectedRunId = searchParams.get("run");
  const { data: selectedRun } = useRun(selectedRunId);

  // --- Mutations ---
  const createWorkflow = useCreateWorkflow();
  const updateWorkflow = useUpdateWorkflow();
  const deleteWorkflow = useDeleteWorkflow();
  const startRun = useStartRun();

  // --- Local state ---
  const [name, setName] = useState("");
  const [selectedNode, setSelectedNode] = useState<Node<UnifiedNodeData> | null>(null);
  const graphRef = useRef<{ nodes: Node<UnifiedNodeData>[]; edges: Edge[] } | null>(null);

  // Sync name from loaded workflow
  useEffect(() => {
    if (workflow?.name && !isNew) {
      setName(workflow.name);
    }
  }, [workflow?.name, isNew]);

  // --- Graph computation ---
  const graph = useMemo<StudioGraphData>(() => {
    if (isNew) return EMPTY_GRAPH;
    if (!workflow) return EMPTY_GRAPH;
    return definitionToGraph(workflow, mode, selectedRun);
  }, [workflow, mode, selectedRun, isNew]);

  // --- Mode change handler ---
  const handleModeChange = useCallback(
    (newMode: StudioMode) => {
      setMode(newMode);
      setSelectedNode(null);
      setSearchParams((prev) => {
        const next = new URLSearchParams(prev);
        if (newMode === "view") {
          next.delete("mode");
        } else {
          next.set("mode", newMode);
        }
        return next;
      });
    },
    [setSearchParams],
  );

  // --- Run selection handler ---
  const handleSelectRun = useCallback(
    (runId: string | null) => {
      setSelectedNode(null);
      setSearchParams((prev) => {
        const next = new URLSearchParams(prev);
        if (runId) {
          next.set("run", runId);
        } else {
          next.delete("run");
        }
        return next;
      });
    },
    [setSearchParams],
  );

  // --- Node selection handler ---
  const handleNodeSelect = useCallback(
    (node: Node<UnifiedNodeData> | null) => {
      setSelectedNode(node);
    },
    [],
  );

  // --- Build workflow payload from current graph state ---
  const buildPayload = useCallback((): Partial<Workflow> => {
    const currentGraph = graphRef.current;
    if (!currentGraph) return { name, steps: [] };

    const definition = graphToDefinition(currentGraph.nodes, currentGraph.edges, {
      name,
      description: workflow?.description,
      timeout_sec: workflow?.timeout_sec,
      metadata: workflow?.metadata,
      input_schema: workflow?.input_schema,
      config: workflow?.config,
    });

    return definition;
  }, [name, workflow]);

  // --- Save handler ---
  const handleSave = useCallback(() => {
    const payload = buildPayload();
    if (!payload.name?.trim()) {
      toast.error("Workflow name is required");
      return;
    }

    if (isNew) {
      createWorkflow.mutate(payload as Partial<Workflow> & { id?: string }, {
        onSuccess: (data) => {
          if (data?.id) {
            navigate(`/workflows/${data.id}?mode=edit`, { replace: true });
          }
        },
      });
    } else if (workflowId) {
      updateWorkflow.mutate({ ...payload, id: workflowId } as Partial<Workflow> & { id: string });
    }
  }, [buildPayload, isNew, workflowId, createWorkflow, updateWorkflow, navigate]);

  // --- Deploy handler (save + switch to view) ---
  const handleDeploy = useCallback(() => {
    const payload = buildPayload();
    if (!payload.name?.trim()) {
      toast.error("Workflow name is required");
      return;
    }

    const onSuccess = (data?: { id?: string }) => {
      const id = data?.id ?? workflowId;
      if (id) {
        navigate(`/workflows/${id}`, { replace: true });
        setMode("view");
      }
    };

    if (isNew) {
      createWorkflow.mutate(payload as Partial<Workflow> & { id?: string }, { onSuccess });
    } else if (workflowId) {
      updateWorkflow.mutate({ ...payload, id: workflowId } as Partial<Workflow> & { id: string }, { onSuccess });
    }
  }, [buildPayload, isNew, workflowId, createWorkflow, updateWorkflow, navigate]);

  // --- Run handler ---
  const handleRun = useCallback(() => {
    if (!workflowId || isNew) return;
    startRun.mutate(
      { workflowId },
      {
        onSuccess: (data) => {
          if (data?.run_id) {
            handleSelectRun(data.run_id);
          }
        },
      },
    );
  }, [workflowId, isNew, startRun, handleSelectRun]);

  // --- Delete handler ---
  const handleDelete = useCallback(() => {
    if (!workflowId || isNew) return;
    if (!window.confirm("Delete this workflow? This cannot be undone.")) return;
    deleteWorkflow.mutate(workflowId, {
      onSuccess: () => navigate("/workflows"),
    });
  }, [workflowId, isNew, deleteWorkflow, navigate]);

  // --- Node config save handler (edit mode) ---
  const handleNodeConfigSave = useCallback(
    (nodeId: string, data: { label: string; config: Record<string, unknown> }) => {
      if (!graphRef.current) return;

      const updatedNodes = graphRef.current.nodes.map((n) => {
        if (n.id !== nodeId) return n;
        return {
          ...n,
          data: {
            ...n.data,
            label: data.label,
            // Merge config fields back into node data
            topic: data.config.topic as string | undefined ?? n.data.topic,
            condition: data.config.condition as string | undefined ?? n.data.condition,
            worker_id: data.config.worker_id as string | undefined ?? n.data.worker_id,
            for_each: data.config.for_each as string | undefined ?? n.data.for_each,
            max_parallel: data.config.max_parallel as number | undefined ?? n.data.max_parallel,
            input: data.config.input as Record<string, unknown> | undefined ?? n.data.input,
            timeout_sec: data.config.timeout_sec as number | undefined ?? n.data.timeout_sec,
            delay_sec: data.config.delay_sec as number | undefined ?? n.data.delay_sec,
            delay_until: data.config.delay_until as string | undefined ?? n.data.delay_until,
            on_error: data.config.on_error as string | undefined ?? n.data.on_error,
            config: data.config,
          },
        };
      });

      graphRef.current = { ...graphRef.current, nodes: updatedNodes };
      toast.success("Node updated");
    },
    [],
  );

  // --- Node delete handler (edit mode) ---
  const handleNodeDelete = useCallback(
    (nodeId: string) => {
      if (!graphRef.current) return;
      graphRef.current = {
        nodes: graphRef.current.nodes.filter((n) => n.id !== nodeId),
        edges: graphRef.current.edges.filter((e) => e.source !== nodeId && e.target !== nodeId),
      };
      setSelectedNode(null);
      toast.success("Node removed");
    },
    [],
  );

  // --- Close node panel ---
  const handleClosePanel = useCallback(() => {
    setSelectedNode(null);
  }, []);

  // --- Loading state ---
  if (!isNew && isLoadingWorkflow) {
    return (
      <div className="flex h-full w-full items-center justify-center bg-surface-0">
        <Loader2 className="h-8 w-8 animate-spin text-accent" />
      </div>
    );
  }

  const isSaving = createWorkflow.isPending || updateWorkflow.isPending;
  const isRunning = startRun.isPending;

  return (
    <div className="flex h-full w-full flex-col bg-surface-0 overflow-hidden">
      {/* Toolbar */}
      <StudioToolbar
        mode={mode}
        workflow={workflow ?? null}
        run={selectedRun ?? null}
        name={name}
        onNameChange={setName}
        onModeChange={handleModeChange}
        onSave={handleSave}
        onDeploy={handleDeploy}
        onRun={handleRun}
        onDelete={handleDelete}
        isSaving={isSaving}
        isRunning={isRunning}
      />

      {/* Main area: sidebar + canvas + node panel */}
      <div className="flex flex-1 overflow-hidden">
        {/* Left sidebar */}
        <StudioSidebar
          mode={mode}
          workflow={workflow ?? null}
          runs={runs}
          selectedRunId={selectedRunId}
          onSelectRun={handleSelectRun}
        />

        {/* Canvas */}
        <StudioCanvas
          key={`${workflowId}-${mode}-${selectedRunId}`}
          initialGraph={graph}
          mode={mode}
          onNodeSelect={handleNodeSelect}
          graphRef={graphRef}
          className="flex-1"
        />

        {/* Right panel */}
        <StudioNodePanel
          mode={mode}
          selectedNode={selectedNode}
          workflow={workflow ?? null}
          run={selectedRun ?? null}
          allNodes={graphRef.current?.nodes ?? graph.nodes}
          onClose={handleClosePanel}
          onNodeConfigSave={handleNodeConfigSave}
          onNodeDelete={handleNodeDelete}
        />
      </div>
    </div>
  );
}
