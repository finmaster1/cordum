import {
  useCallback,
  useMemo,
  useRef,
  useState,
  type DragEvent,
} from "react";
import ReactFlow, {
  addEdge,
  Background,
  Controls,
  MiniMap,
  Panel,
  useEdgesState,
  useNodesState,
  type Connection,
  type Edge,
  type Node,
  type NodeTypes,
  type ReactFlowInstance,
} from "reactflow";
import "reactflow/dist/style.css";
import { Info, X, Maximize2, Minimize2 } from "lucide-react";

import { cn } from "@/lib/utils";
import type { UnifiedNodeData, StudioMode, StudioGraphData } from "./types";
import { UnifiedNode } from "./nodes/UnifiedNode";

// ---------------------------------------------------------------------------
// Node type registry (stable reference outside component)
// ---------------------------------------------------------------------------

const nodeTypes: NodeTypes = {
  unified: UnifiedNode,
};

// ---------------------------------------------------------------------------
// Dependency highlighting helpers
// ---------------------------------------------------------------------------

function collectRelated(
  nodeId: string,
  edges: Edge[],
  direction: "ancestors" | "descendants",
): Set<string> {
  const result = new Set<string>();
  const queue = [nodeId];
  while (queue.length > 0) {
    const current = queue.shift()!;
    for (const edge of edges) {
      const match = direction === "ancestors"
        ? edge.target === current && !result.has(edge.source)
        : edge.source === current && !result.has(edge.target);
      if (match) {
        const next = direction === "ancestors" ? edge.source : edge.target;
        result.add(next);
        queue.push(next);
      }
    }
  }
  return result;
}

function applyHighlighting(
  nodes: Node<UnifiedNodeData>[],
  edges: Edge[],
  highlightedId: string | null,
): { nodes: Node<UnifiedNodeData>[]; edges: Edge[] } {
  if (!highlightedId) return { nodes, edges };

  const ancestors = collectRelated(highlightedId, edges, "ancestors");
  const descendants = collectRelated(highlightedId, edges, "descendants");
  const related = new Set([highlightedId, ...ancestors, ...descendants]);

  const styledNodes = nodes.map((node) => ({
    ...node,
    style: {
      ...node.style,
      opacity: related.has(node.id) ? 1 : 0.25,
      transition: "opacity 0.2s ease",
    },
  }));

  const styledEdges = edges.map((edge) => {
    const isAncestorEdge = ancestors.has(edge.source)
      && (ancestors.has(edge.target) || edge.target === highlightedId);
    const isDescendantEdge = descendants.has(edge.target)
      && (descendants.has(edge.source) || edge.source === highlightedId);

    if (isAncestorEdge) {
      return { ...edge, style: { ...(edge.style ?? {}), strokeWidth: 2.5, stroke: "var(--color-info)" }, animated: true };
    }
    if (isDescendantEdge) {
      return { ...edge, style: { ...(edge.style ?? {}), strokeWidth: 2.5, stroke: "var(--color-warning)" }, animated: true };
    }
    return { ...edge, style: { ...(edge.style ?? {}), opacity: 0.15 } };
  });

  return { nodes: styledNodes, edges: styledEdges };
}

// ---------------------------------------------------------------------------
// Legend
// ---------------------------------------------------------------------------

function DAGLegend({ onClose }: { onClose: () => void }) {
  return (
    <div className="w-56 space-y-2.5 rounded-2xl border border-border bg-[color:var(--surface-glass)] p-3 text-xs shadow-soft backdrop-blur-md">
      <div className="flex items-center justify-between">
        <span className="font-semibold text-ink">Legend</span>
        <button type="button" onClick={onClose} className="p-0.5 text-muted-foreground hover:text-ink">
          <X className="h-3 w-3" />
        </button>
      </div>
      <div className="space-y-1.5">
        <span className="block text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">Status</span>
        <div className="flex items-center gap-2"><span className="h-2.5 w-2.5 rounded-full bg-[var(--color-success)]" />Succeeded</div>
        <div className="flex items-center gap-2"><span className="h-2.5 w-2.5 rounded-full bg-destructive" />Failed</div>
        <div className="flex items-center gap-2"><span className="h-2.5 w-2.5 rounded-full bg-[var(--color-info)] animate-pulse" />Running</div>
        <div className="flex items-center gap-2"><span className="h-2.5 w-2.5 rounded-full bg-muted" />Pending</div>
        <div className="flex items-center gap-2"><span className="h-2.5 w-2.5 rounded-full bg-[var(--color-warning)]" />Waiting</div>
      </div>
      <div className="space-y-1.5">
        <span className="block text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">Edges</span>
        <div className="flex items-center gap-2"><span className="h-0.5 w-5 bg-[var(--accent)]" />Critical path</div>
        <div className="flex items-center gap-2"><span className="h-0.5 w-5 bg-[var(--color-success)]" />Completed</div>
        <div className="flex items-center gap-2"><span className="h-0.5 w-5 bg-destructive" />To failed</div>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Canvas props
// ---------------------------------------------------------------------------

export interface StudioCanvasProps {
  initialGraph: StudioGraphData;
  mode: StudioMode;
  onNodeSelect?: (node: Node<UnifiedNodeData> | null) => void;
  /** Expose live graph for parent to read (save operations) */
  graphRef?: React.MutableRefObject<{ nodes: Node<UnifiedNodeData>[]; edges: Edge[] } | null>;
  className?: string;
}

// ---------------------------------------------------------------------------
// Node ID generator
// ---------------------------------------------------------------------------

let nodeCounter = 0;
function nextNodeId(): string {
  nodeCounter += 1;
  return `step-${Date.now()}-${nodeCounter}`;
}

// ---------------------------------------------------------------------------
// StudioCanvas
// ---------------------------------------------------------------------------

export function StudioCanvas({
  initialGraph,
  mode,
  onNodeSelect,
  graphRef,
  className,
}: StudioCanvasProps) {
  const [nodes, setNodes, handleNodesChange] = useNodesState(initialGraph.nodes);
  const [edges, setEdges, handleEdgesChange] = useEdgesState(initialGraph.edges);
  const rfInstance = useRef<ReactFlowInstance | null>(null);

  // Dependency highlighting (view mode)
  const [highlightedNode, setHighlightedNode] = useState<string | null>(null);
  const [fullscreen, setFullscreen] = useState(false);
  const [showLegend, setShowLegend] = useState(false);

  const isEdit = mode === "edit";

  // Keep parent ref in sync
  if (graphRef) {
    graphRef.current = { nodes, edges };
  }

  // Apply highlighting in view mode
  const { nodes: displayNodes, edges: displayEdges } = useMemo(
    () => isEdit ? { nodes, edges } : applyHighlighting(nodes, edges, highlightedNode),
    [nodes, edges, highlightedNode, isEdit],
  );

  // --- Handlers ---

  const handleNodeClick = useCallback(
    (_: React.MouseEvent, node: Node<UnifiedNodeData>) => {
      if (!isEdit) {
        // Toggle dependency highlighting in view mode
        setHighlightedNode((prev) => (prev === node.id ? null : node.id));
      }
      onNodeSelect?.(node);
    },
    [isEdit, onNodeSelect],
  );

  const handlePaneClick = useCallback(() => {
    setHighlightedNode(null);
    onNodeSelect?.(null);
  }, [onNodeSelect]);

  const handleConnect = useCallback(
    (connection: Connection) => {
      if (!isEdit) return;
      setEdges((eds) => addEdge({ ...connection, type: "smoothstep" }, eds));
    },
    [isEdit, setEdges],
  );

  const handleNodesDelete = useCallback(
    (deleted: Node[]) => {
      if (!isEdit) return;
      // Prevent deleting start nodes
      const deletable = deleted.filter((n) => n.id !== "start" && n.type !== "start");
      if (deletable.length === 0) return;
      // Also clean up edges connected to deleted nodes
      const deletedIds = new Set(deletable.map((n) => n.id));
      setEdges((eds) => eds.filter((e) => !deletedIds.has(e.source) && !deletedIds.has(e.target)));
    },
    [isEdit, setEdges],
  );

  // Drag-and-drop from sidebar palette (edit mode)
  const handleDragOver = useCallback((event: DragEvent) => {
    event.preventDefault();
    event.dataTransfer.dropEffect = "move";
  }, []);

  const handleDrop = useCallback(
    (event: DragEvent) => {
      event.preventDefault();
      if (!isEdit || !rfInstance.current) return;

      const stepType = event.dataTransfer.getData("application/workflow-studio");
      if (!stepType) return;

      const position = rfInstance.current.screenToFlowPosition({
        x: event.clientX,
        y: event.clientY,
      });

      const id = nextNodeId();
      const newNode: Node<UnifiedNodeData> = {
        id,
        type: "unified",
        position,
        data: {
          label: stepType.replace(/-/g, " "),
          stepId: id,
          stepType,
          mode,
          config: {},
        },
        draggable: true,
        connectable: true,
      };

      setNodes((nds) => [...nds, newNode]);
    },
    [isEdit, mode, setNodes],
  );

  // --- Render ---

  const canvasContent = (
    <div className={cn("h-full w-full", !fullscreen && className)}>
      <ReactFlow
        nodes={displayNodes}
        edges={displayEdges}
        onNodesChange={isEdit ? handleNodesChange : undefined}
        onEdgesChange={isEdit ? handleEdgesChange : undefined}
        onConnect={handleConnect}
        onNodeClick={handleNodeClick}
        onPaneClick={handlePaneClick}
        onNodesDelete={handleNodesDelete}
        onDragOver={isEdit ? handleDragOver : undefined}
        onDrop={isEdit ? handleDrop : undefined}
        onInit={(instance) => { rfInstance.current = instance; }}
        nodeTypes={nodeTypes}
        nodesDraggable={isEdit}
        nodesConnectable={isEdit}
        elementsSelectable
        deleteKeyCode={isEdit ? ["Delete", "Backspace"] : []}
        defaultEdgeOptions={{ type: "smoothstep", animated: false }}
        fitView
        snapToGrid={isEdit}
        snapGrid={[20, 20]}
      >
        <Background gap={20} size={1} />
        <Controls showInteractive={isEdit} />
        <MiniMap
          nodeStrokeWidth={3}
          className="!bg-surface-1 !border-border"
        />

        {/* Top-right panel buttons */}
        <Panel position="top-right" className="flex gap-1">
          {!isEdit && (
            <button
              type="button"
              onClick={() => setShowLegend((v) => !v)}
              className="rounded-full border border-border bg-surface-1 p-1.5 text-muted-foreground transition-colors hover:bg-surface-2 hover:text-ink"
              title="Legend"
            >
              <Info className="h-4 w-4" />
            </button>
          )}
          <button
            type="button"
            onClick={() => setFullscreen((v) => !v)}
            className="rounded-full border border-border bg-surface-1 p-1.5 text-muted-foreground transition-colors hover:bg-surface-2 hover:text-ink"
            title={fullscreen ? "Exit fullscreen" : "Fullscreen"}
          >
            {fullscreen ? <Minimize2 className="h-4 w-4" /> : <Maximize2 className="h-4 w-4" />}
          </button>
        </Panel>

        {/* Legend (view mode) */}
        {showLegend && !isEdit && (
          <Panel position="bottom-left">
            <DAGLegend onClose={() => setShowLegend(false)} />
          </Panel>
        )}
      </ReactFlow>
    </div>
  );

  if (fullscreen) {
    return (
      <div className="fixed inset-0 z-50 bg-surface-1">
        {canvasContent}
      </div>
    );
  }

  return canvasContent;
}
