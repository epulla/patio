import "@xyflow/react/dist/style.css";
import {
  Background,
  Controls,
  Handle,
  MarkerType,
  Position,
  ReactFlow,
  ReactFlowProvider,
  addEdge,
  useEdgesState,
  useNodesState,
  useReactFlow,
  type Connection,
  type Edge,
  type Node,
  type NodeProps,
} from "@xyflow/react";
import { useEffect, useMemo, useState, type DragEvent, type FormEvent } from "react";
import { api, type Bot, type Workflow, type WorkflowEdge } from "./api";
import { ErrorText, navigate } from "./App";
import { BotsHeading } from "./Bots";
import { confirmDialog } from "./confirm";
import History from "./History";
import { layout } from "./flowLayout";
import { Icon, IconButton } from "./icons";
import { useText } from "./i18n";

const text = {
  en: {
    workflows: "Workflows", new: "New workflow", back: "Workflows", gone: "This workflow no longer exists.",
    help: "A workflow is a graph of bots. Each bot runs in its own chat; an arrow is a handoff. A bot with several incoming arrows waits for all of them before it starts.",
    empty: "No workflows yet", count: (n: number) => `${n} workflow${n === 1 ? "" : "s"}`, steps: (n: number) => `${n} bot${n === 1 ? "" : "s"}`,
    name: "Name", description: "Description", hopBudget: "Handoff budget", hopBudgetTitle: "Most handoffs one run may make before Patio pauses it",
    palette: "Bots", paletteHelp: "Drag a bot onto the canvas, or click to add it. Connect bots by dragging from the bottom dot to the top dot.",
    add: (name: string) => `Add ${name}`, layout: "Auto layout", save: "Save", saved: "Saved", create: "Create workflow",
    delete: (name: string) => `Delete ${name}`, confirmDelete: (name: string) => `Delete workflow "${name}"?`,
    start: "Start", join: "Waits for all", unknownBot: "Missing bot",
    edge: "Handoff", reportBack: "Report back when done", instruction: "Instruction", instructionPlaceholder: "When the code is done, hand it to Sammy for review.",
    timeout: "Timeout (minutes)", timeoutTitle: "0 uses the default of 30 minutes", removeEdge: "Remove handoff",
    node: "Step", removeNode: "Remove step", bot: "Bot", canvas: "Workflow graph",
    run: "Run", runPrompt: "Task for the first bot", runPlaceholder: "Add a dark mode toggle to the settings page", runHint: "Save before running.",
    noRoot: "Exactly one bot must have no incoming arrow; it's where the run starts.",
  },
  es: {
    workflows: "Flujos", new: "Nuevo flujo", back: "Flujos", gone: "Este flujo ya no existe.",
    help: "Un flujo es un grafo de bots. Cada bot trabaja en su propio chat; una flecha es un traspaso. Un bot con varias flechas de entrada espera a todas antes de empezar.",
    empty: "Aún no hay flujos", count: (n: number) => `${n} flujo${n === 1 ? "" : "s"}`, steps: (n: number) => `${n} bot${n === 1 ? "" : "s"}`,
    name: "Nombre", description: "Descripción", hopBudget: "Límite de traspasos", hopBudgetTitle: "Máximo de traspasos que una ejecución puede hacer antes de que Patio la pause",
    palette: "Bots", paletteHelp: "Arrastra un bot al lienzo o haz clic para agregarlo. Conecta bots arrastrando del punto inferior al superior.",
    add: (name: string) => `Agregar ${name}`, layout: "Ordenar", save: "Guardar", saved: "Guardado", create: "Crear flujo",
    delete: (name: string) => `Eliminar ${name}`, confirmDelete: (name: string) => `¿Eliminar el flujo "${name}"?`,
    start: "Inicio", join: "Espera a todos", unknownBot: "Bot no encontrado",
    edge: "Traspaso", reportBack: "Informar al terminar", instruction: "Instrucción", instructionPlaceholder: "Cuando el código esté listo, pásalo a Sammy para revisión.",
    timeout: "Tiempo límite (minutos)", timeoutTitle: "0 usa el valor predeterminado de 30 minutos", removeEdge: "Quitar traspaso",
    node: "Paso", removeNode: "Quitar paso", bot: "Bot", canvas: "Grafo del flujo",
    run: "Ejecutar", runPrompt: "Tarea para el primer bot", runPlaceholder: "Agrega un interruptor de modo oscuro en la página de ajustes", runHint: "Guarda antes de ejecutar.",
    noRoot: "Exactamente un bot debe quedar sin flechas de entrada; ahí empieza la ejecución.",
  },
};

type StepData = { bot?: Bot; root: boolean; join: boolean };
type StepNode = Node<StepData, "step">;
type HandoffEdge = Edge<Omit<WorkflowEdge, "id" | "from" | "to">>;

export default function Workflows({ id, refresh }: { id?: string; refresh: number }) {
  const t = useText(text);
  const [workflows, setWorkflows] = useState<Workflow[]>();
  const [bots, setBots] = useState<Bot[]>();
  const [error, setError] = useState<unknown>();
  const [reload, setReload] = useState(0);

  useEffect(() => {
    Promise.all([api.get<Workflow[]>("/api/workflows"), api.get<Bot[]>("/api/bots")])
      .then(([list, botList]) => {
        setWorkflows(list);
        setBots(botList);
        setError(undefined);
      })
      .catch(setError);
  }, [refresh, reload]);

  if (id) {
    const workflow = workflows?.find((w) => w.id === id);
    if (!workflows || !bots) return <ErrorText error={error} />;
    if (id !== "new" && !workflow) {
      return (
        <div className="stack">
          <BackLink />
          <p className="muted">{t.gone}</p>
        </div>
      );
    }
    return (
      <ReactFlowProvider key={id}>
        <WorkflowEditor
          workflow={workflow}
          bots={bots}
          refresh={refresh}
          onSaved={(saved) => {
            setReload((value) => value + 1);
            if (!workflow) navigate(`/ai/workflows/${saved.id}`);
          }}
          onDeleted={() => {
            setReload((value) => value + 1);
            navigate("/ai/workflows");
          }}
        />
      </ReactFlowProvider>
    );
  }

  return (
    <div className="stack">
      <BotsHeading
        tab="workflows"
        actions={
          <button className="primary" onClick={() => navigate("/ai/workflows/new")}>
            <Icon name="plus" /> {t.new}
          </button>
        }
      />
      <p className="muted">{t.help}</p>
      <ErrorText error={error} />
      {!!workflows?.length && (
        <>
          <div className="list-toolbar">
            <span className="muted">{t.count(workflows.length)}</span>
          </div>
          <ul className="panel skill-list" aria-label={t.workflows}>
            {workflows.map((workflow) => (
              <li key={workflow.id}>
                <button className="skill-item" onClick={() => navigate(`/ai/workflows/${workflow.id}`)}>
                  <span className="skill-name">{workflow.name}</span>
                  <span className="skill-description">
                    {workflow.description}
                    {workflow.description && " · "}
                    {t.steps(workflow.graph.nodes.length)}
                  </span>
                </button>
              </li>
            ))}
          </ul>
        </>
      )}
      {workflows?.length === 0 && <p className="muted">{t.empty}</p>}
    </div>
  );
}

function BackLink() {
  const t = useText(text);
  return (
    <button type="button" className="back" onClick={() => navigate("/ai/workflows")}>
      <Icon name="arrowLeft" /> {t.back}
    </button>
  );
}

function StepCard({ data, selected }: NodeProps<StepNode>) {
  const t = useText(text);
  return (
    <div className={`flow-node${selected ? " selected" : ""}`}>
      <Handle type="target" position={Position.Top} />
      <span className="flow-node-name">
        <span className="bot-dot" style={{ background: data.bot?.color || "var(--muted)" }} aria-hidden="true" /> {data.bot?.name ?? t.unknownBot}
      </span>
      <span className="flow-node-meta">
        {data.root && <span className="flow-badge">{t.start}</span>}
        {data.join && <span className="flow-badge">{t.join}</span>}
        {data.bot?.description}
      </span>
      <Handle type="source" position={Position.Bottom} />
    </div>
  );
}

const nodeTypes = { step: StepCard };
const arrow = { type: MarkerType.ArrowClosed };

function toFlow(workflow: Workflow | undefined, bots: Bot[]): { nodes: StepNode[]; edges: HandoffEdge[] } {
  const graph = workflow?.graph ?? { nodes: [], edges: [] };
  return {
    nodes: graph.nodes.map((node) => ({ id: node.id, type: "step", position: { x: node.x, y: node.y }, data: { bot: bots.find((b) => b.id === node.bot_id), root: false, join: false } })),
    edges: graph.edges.map(({ id, from, to, ...data }) => ({ id, source: from, target: to, data, markerEnd: arrow })),
  };
}

function WorkflowEditor({
  workflow,
  bots,
  refresh,
  onSaved,
  onDeleted,
}: {
  workflow?: Workflow;
  bots: Bot[];
  refresh: number;
  onSaved: (workflow: Workflow) => void;
  onDeleted: () => void;
}) {
  const t = useText(text);
  const initial = useMemo(() => toFlow(workflow, bots), [workflow, bots]);
  const [nodes, setNodes, onNodesChange] = useNodesState<StepNode>(initial.nodes);
  const [edges, setEdges, onEdgesChange] = useEdgesState<HandoffEdge>(initial.edges);
  const [name, setName] = useState(workflow?.name ?? "");
  const [description, setDescription] = useState(workflow?.description ?? "");
  const [hopBudget, setHopBudget] = useState(workflow?.hop_budget ?? 30);
  const [prompt, setPrompt] = useState("");
  const [error, setError] = useState<unknown>();
  const [saved, setSaved] = useState(false);
  const [dirty, setDirty] = useState(!workflow);
  const flow = useReactFlow();

  const incoming = (nodeID: string) => edges.filter((edge) => edge.target === nodeID).length;
  const roots = nodes.filter((node) => incoming(node.id) === 0);
  const shownNodes = nodes.map((node) => ({ ...node, data: { ...node.data, root: roots.length === 1 && roots[0].id === node.id, join: incoming(node.id) > 1 } }));
  const selectedEdge = edges.find((edge) => edge.selected);
  const selectedNode = nodes.find((node) => node.selected);

  function changed() {
    setDirty(true);
    setSaved(false);
  }

  function addBot(bot: Bot, position?: { x: number; y: number }) {
    const last = nodes[nodes.length - 1];
    const id = crypto.randomUUID();
    setNodes((current) => [...current, { id, type: "step", position: position ?? { x: last ? last.position.x : 0, y: last ? last.position.y + 120 : 0 }, data: { bot, root: false, join: false } }]);
    changed();
  }

  function drop(event: DragEvent) {
    event.preventDefault();
    const bot = bots.find((b) => b.id === event.dataTransfer.getData("application/x-patio-bot"));
    if (bot) addBot(bot, flow.screenToFlowPosition({ x: event.clientX, y: event.clientY }));
  }

  function connect(connection: Connection) {
    if (connection.source === connection.target) return;
    setEdges((current) => addEdge({ ...connection, id: crypto.randomUUID(), markerEnd: arrow, data: { report_back: true, instruction: "", timeout_minutes: 0 } }, current));
    changed();
  }

  function updateEdge(patch: Partial<WorkflowEdge>) {
    if (!selectedEdge) return;
    setEdges((current) => current.map((edge) => (edge.id === selectedEdge.id ? { ...edge, data: { ...edge.data!, ...patch } } : edge)));
    changed();
  }

  function autoLayout() {
    const positions = layout(nodes.map((node) => node.id), edges.map((edge) => ({ from: edge.source, to: edge.target })));
    setNodes((current) => current.map((node) => ({ ...node, position: positions.get(node.id) ?? node.position })));
    changed();
    window.requestAnimationFrame(() => flow.fitView());
  }

  const body = {
    name: name.trim(),
    description,
    hop_budget: hopBudget,
    graph: {
      nodes: nodes.map((node) => ({ id: node.id, bot_id: node.data.bot?.id ?? "", x: Math.round(node.position.x), y: Math.round(node.position.y) })),
      edges: edges.map((edge) => ({ id: edge.id, from: edge.source, to: edge.target, ...edge.data! })),
    },
  };

  async function submit(event: FormEvent) {
    event.preventDefault();
    try {
      const result = workflow ? await api.patch<Workflow>(`/api/workflows/${workflow.id}`, body) : await api.post<Workflow>("/api/workflows", body);
      setError(undefined);
      setDirty(false);
      setSaved(true);
      window.setTimeout(() => setSaved(false), 1500);
      onSaved(result);
    } catch (reason) {
      setError(reason);
    }
  }

  async function remove() {
    if (!workflow || !(await confirmDialog(t.confirmDelete(workflow.name)))) return;
    try {
      await api.delete(`/api/workflows/${workflow.id}`);
      onDeleted();
    } catch (reason) {
      setError(reason);
    }
  }

  async function run(event: FormEvent) {
    event.preventDefault();
    if (!workflow) return;
    try {
      const result = await api.post<{ chat_id: string }>(`/api/workflows/${workflow.id}/run`, { prompt });
      navigate(`/ai/chats/${result.chat_id}`);
    } catch (reason) {
      setError(reason);
    }
  }

  return (
    <div className="stack">
      <BackLink />
      <div className="page-heading">
        <h1>{workflow ? workflow.name : t.new}</h1>
      </div>
      <form className="panel skill-editor" onSubmit={submit} aria-label={workflow ? workflow.name : t.new}>
        <div className="skill-fields">
          <label>
            {t.name}
            <input autoFocus={!workflow} required placeholder="Dev flow" value={name} onChange={(event) => (setName(event.target.value), changed())} />
          </label>
          <label className="grow">
            {t.description}
            <input value={description} onChange={(event) => (setDescription(event.target.value), changed())} />
          </label>
          <label title={t.hopBudgetTitle}>
            {t.hopBudget}
            <input type="number" min={1} max={500} required value={hopBudget} onChange={(event) => (setHopBudget(Number(event.target.value)), changed())} />
          </label>
        </div>
        <div className="flow-editor">
          <aside className="flow-palette" aria-label={t.palette}>
            <p className="muted">{t.paletteHelp}</p>
            {bots.map((bot) => (
              <button
                key={bot.id}
                type="button"
                className="flow-palette-item"
                draggable
                aria-label={t.add(bot.name)}
                onDragStart={(event) => event.dataTransfer.setData("application/x-patio-bot", bot.id)}
                onClick={() => addBot(bot)}
              >
                <span className="bot-dot" style={{ background: bot.color || "var(--muted)" }} aria-hidden="true" /> {bot.name}
              </button>
            ))}
          </aside>
          <div className="flow-canvas" aria-label={t.canvas} onDragOver={(event) => event.preventDefault()} onDrop={drop}>
            <ReactFlow
              nodes={shownNodes}
              edges={edges}
              nodeTypes={nodeTypes}
              onNodesChange={(changes) => {
                onNodesChange(changes);
                if (changes.some((change) => change.type !== "select" && change.type !== "dimensions")) changed();
              }}
              onEdgesChange={(changes) => {
                onEdgesChange(changes);
                if (changes.some((change) => change.type !== "select")) changed();
              }}
              onConnect={connect}
              fitView
              proOptions={{ hideAttribution: true }}
            >
              <Background />
              <Controls showInteractive={false} />
            </ReactFlow>
          </div>
          {(selectedEdge || selectedNode) && (
            <aside className="flow-side">
              {selectedEdge && (
                <>
                  <h3>{t.edge}</h3>
                  <label className="check">
                    <input type="checkbox" checked={selectedEdge.data!.report_back} onChange={(event) => updateEdge({ report_back: event.target.checked })} /> {t.reportBack}
                  </label>
                  <label>
                    {t.instruction}
                    <textarea rows={4} placeholder={t.instructionPlaceholder} value={selectedEdge.data!.instruction} onChange={(event) => updateEdge({ instruction: event.target.value })} />
                  </label>
                  <label title={t.timeoutTitle}>
                    {t.timeout}
                    <input type="number" min={0} max={1440} value={selectedEdge.data!.timeout_minutes} onChange={(event) => updateEdge({ timeout_minutes: Number(event.target.value) })} />
                  </label>
                  <button type="button" className="danger-text" onClick={() => (setEdges((current) => current.filter((edge) => edge.id !== selectedEdge.id)), changed())}>
                    <Icon name="trash" /> {t.removeEdge}
                  </button>
                </>
              )}
              {selectedNode && !selectedEdge && (
                <>
                  <h3>{t.node}</h3>
                  <label>
                    {t.bot}
                    <select
                      value={selectedNode.data.bot?.id ?? ""}
                      onChange={(event) => {
                        const bot = bots.find((b) => b.id === event.target.value);
                        setNodes((current) => current.map((node) => (node.id === selectedNode.id ? { ...node, data: { ...node.data, bot } } : node)));
                        changed();
                      }}
                    >
                      {bots.map((bot) => (
                        <option key={bot.id} value={bot.id}>
                          {bot.name}
                        </option>
                      ))}
                    </select>
                  </label>
                  <button
                    type="button"
                    className="danger-text"
                    onClick={() => {
                      setNodes((current) => current.filter((node) => node.id !== selectedNode.id));
                      setEdges((current) => current.filter((edge) => edge.source !== selectedNode.id && edge.target !== selectedNode.id));
                      changed();
                    }}
                  >
                    <Icon name="trash" /> {t.removeNode}
                  </button>
                </>
              )}
            </aside>
          )}
        </div>
        {nodes.length > 0 && roots.length !== 1 && <p className="muted">{t.noRoot}</p>}
        <ErrorText error={error} />
        <div className="connector-buttons">
          <button className="primary" disabled={!dirty || nodes.length === 0 || roots.length !== 1}>
            {saved ? t.saved : workflow ? t.save : t.create}
          </button>
          <button type="button" onClick={autoLayout} disabled={nodes.length === 0}>
            {t.layout}
          </button>
          {workflow && <IconButton type="button" className="danger-text" icon="trash" label={t.delete(workflow.name)} onClick={remove} />}
        </div>
      </form>
      {workflow && (
        <form className="panel skill-editor" onSubmit={run} aria-label={t.run}>
          <label>
            {t.runPrompt}
            <textarea rows={3} required placeholder={t.runPlaceholder} value={prompt} onChange={(event) => setPrompt(event.target.value)} />
          </label>
          {dirty && <p className="muted">{t.runHint}</p>}
          <div className="connector-buttons">
            <button className="primary" disabled={dirty || !prompt.trim()}>
              <Icon name="play" /> {t.run}
            </button>
          </div>
        </form>
      )}
      {workflow && <History itemType="workflow" itemId={workflow.id} refresh={refresh} />}
    </div>
  );
}
