import "@xyflow/react/dist/style.css";
import { Background, Controls, Handle, Position, ReactFlow, type Edge, type Node, type NodeProps } from "@xyflow/react";
import { useEffect, useState } from "react";
import { api, type ChatTree, type FlowStatus } from "./api";
import { ErrorText, navigate } from "./App";
import { layout } from "./flowLayout";
import { flowEdgeDefaults } from "./Workflows";
import { Icon } from "./icons";
import { useText } from "./i18n";

const text = {
  en: {
    title: "Flow", back: "Back to chat", help: "Every chat in this run. Click a step to open its chat.",
    status: { running: "Running", queued: "Queued", waiting: "Waiting for handoffs", held: "Paused", error: "Error", idle: "Idle" } as Record<FlowStatus, string>,
    reports: (received: number, expected: number) => `${received}/${expected} reports`,
    graph: "Run graph",
  },
  es: {
    title: "Flujo", back: "Volver al chat", help: "Todos los chats de esta ejecución. Haz clic en un paso para abrir su chat.",
    status: { running: "En curso", queued: "En cola", waiting: "Esperando traspasos", held: "En pausa", error: "Error", idle: "Inactivo" } as Record<FlowStatus, string>,
    reports: (received: number, expected: number) => `${received}/${expected} informes`,
    graph: "Grafo de la ejecución",
  },
};

type RunNode = Node<ChatTree["nodes"][number], "run">;

function RunCard({ data }: NodeProps<RunNode>) {
  const t = useText(text);
  return (
    <div className={`flow-node flow-status-${data.status}`}>
      <Handle type="target" position={Position.Top} isConnectable={false} />
      <span className="flow-node-name">
        <span className="bot-dot" style={{ background: data.bot_color || "var(--muted)" }} aria-hidden="true" />
        <span className="flow-node-title">{data.bot_name || data.title}</span>
      </span>
      <span className="flow-node-meta">
        <span className="flow-badge flow-status">{t.status[data.status]}</span>
        {data.join && <span className="flow-badge">{t.reports(data.join.received, data.join.expected)}</span>}
      </span>
      <span className="flow-node-meta">{data.title}</span>
      <span className="flow-node-meta">{[data.connector_name, data.model].filter(Boolean).join(" · ")}</span>
      <Handle type="source" position={Position.Bottom} isConnectable={false} />
    </div>
  );
}

const nodeTypes = { run: RunCard };

export default function ChatFlow({ id, refresh }: { id: string; refresh: number }) {
  const t = useText(text);
  const [tree, setTree] = useState<ChatTree>();
  const [error, setError] = useState<unknown>();

  useEffect(() => {
    let cancelled = false;
    const load = () =>
      api
        .get<ChatTree>(`/api/chats/${id}/tree`)
        .then((next) => !cancelled && (setTree(next), setError(undefined)))
        .catch((reason) => !cancelled && setError(reason));
    load();
    const timer = window.setInterval(load, 2000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [id, refresh]);

  const positions = tree && layout(tree.nodes.map((node) => node.chat_id), tree.edges);
  const nodes: RunNode[] = (tree?.nodes ?? []).map((node) => ({ id: node.chat_id, type: "run", position: positions?.get(node.chat_id) ?? { x: 0, y: 0 }, data: node, selected: node.chat_id === id }));
  const edges: Edge[] = (tree?.edges ?? []).map((edge) => ({
    id: `${edge.from}-${edge.to}`,
    source: edge.from,
    target: edge.to,
    ...flowEdgeDefaults,
    animated: edge.status === "pending",
    className: `flow-edge-${edge.status}`,
  }));

  return (
    <div className="stack">
      <button type="button" className="back" onClick={() => navigate(`/ai/chats/${id}`)}>
        <Icon name="arrowLeft" /> {t.back}
      </button>
      <div className="page-heading">
        <div>
          <p className="eyebrow">AI</p>
          <h1>{tree?.workflow ? `${t.title}: ${tree.workflow.name}` : t.title}</h1>
        </div>
      </div>
      <p className="muted">{t.help}</p>
      <ErrorText error={error} />
      {tree && (
        <div className="flow-stage flow-run" aria-label={t.graph}>
          <ReactFlow
            key={tree.nodes.length}
            nodes={nodes}
            edges={edges}
            nodeTypes={nodeTypes}
            nodesDraggable={false}
            nodesConnectable={false}
            onNodeClick={(_, node) => navigate(`/ai/chats/${node.id}`)}
            fitView
            fitViewOptions={{ padding: 0.3, maxZoom: 1 }}
            proOptions={{ hideAttribution: true }}
          >
            <Background gap={20} />
            <Controls showInteractive={false} position="bottom-left" />
          </ReactFlow>
        </div>
      )}
    </div>
  );
}
