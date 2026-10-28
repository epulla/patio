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
import { useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { api, type Bot, type Workflow, type WorkflowEdge } from "./api";
import { ErrorText, navigate } from "./App";
import { BotsHeading } from "./Bots";
import { confirmDialog } from "./confirm";
import History from "./History";
import Menu from "./Menu";
import { layout } from "./flowLayout";
import { Icon, IconButton } from "./icons";
import { useText } from "./i18n";

const text = {
  en: {
    workflows: "Workflows", new: "New workflow", back: "Workflows", gone: "This workflow no longer exists.",
    help: "A workflow is a graph of bots. Each bot runs in its own chat; an arrow is a handoff. A bot with several incoming arrows waits for all of them before it starts.",
    empty: "No workflows yet", count: (n: number) => `${n} workflow${n === 1 ? "" : "s"}`, steps: (n: number) => `${n} bot${n === 1 ? "" : "s"}`,
    name: "Workflow name", description: "Description", hopBudget: "Handoff budget", hopBudgetTitle: "Most handoffs one run may make before Patio pauses it",
    settings: "Settings", addBot: "Add bot", layout: "Auto layout", save: "Save", saved: "Saved", create: "Create",
    delete: (name: string) => `Delete ${name}`, confirmDelete: (name: string) => `Delete workflow "${name}"?`,
    start: "Start", join: "Waits for all", unknownBot: "Missing bot", reportsBack: "reports back",
    hint: "Select a step or arrow to edit it. Drag from a step's bottom dot to another step's top dot to connect them.",
    emptyCanvas: "Add a bot to start.",
    edge: "Handoff", reportBack: "Report back when done", instruction: "Instruction", instructionPlaceholder: "When the code is done, hand it to Sammy for review.",
    timeout: "Timeout (minutes)", timeoutTitle: "0 uses the default of 30 minutes", removeEdge: "Remove handoff",
    node: "Step", removeNode: "Remove step", bot: "Bot", canvas: "Workflow graph", closePanel: "Close",
    run: "Run", runTitle: (name: string) => `Run ${name}`, runPrompt: "Task for the first bot", runPlaceholder: "Add a dark mode toggle to the settings page", starting: "Starting…", cancel: "Cancel",
    noRoot: "Exactly one bot must have no incoming arrow; it's where the run starts.",
  },
  es: {
    workflows: "Flujos", new: "Nuevo flujo", back: "Flujos", gone: "Este flujo ya no existe.",
    help: "Un flujo es un grafo de bots. Cada bot trabaja en su propio chat; una flecha es un traspaso. Un bot con varias flechas de entrada espera a todas antes de empezar.",
    empty: "Aún no hay flujos", count: (n: number) => `${n} flujo${n === 1 ? "" : "s"}`, steps: (n: number) => `${n} bot${n === 1 ? "" : "s"}`,
    name: "Nombre del flujo", description: "Descripción", hopBudget: "Límite de traspasos", hopBudgetTitle: "Máximo de traspasos que una ejecución puede hacer antes de que Patio la pause",
    settings: "Ajustes", addBot: "Agregar bot", layout: "Ordenar", save: "Guardar", saved: "Guardado", create: "Crear",
    delete: (name: string) => `Eliminar ${name}`, confirmDelete: (name: string) => `¿Eliminar el flujo "${name}"?`,
    start: "Inicio", join: "Espera a todos", unknownBot: "Bot no encontrado", reportsBack: "informa",
    hint: "Selecciona un paso o una flecha para editarlo. Arrastra del punto inferior de un paso al punto superior de otro para conectarlos.",
    emptyCanvas: "Agrega un bot para empezar.",
    edge: "Traspaso", reportBack: "Informar al terminar", instruction: "Instrucción", instructionPlaceholder: "Cuando el código esté listo, pásalo a Sammy para revisión.",
    timeout: "Tiempo límite (minutos)", timeoutTitle: "0 usa el valor predeterminado de 30 minutos", removeEdge: "Quitar traspaso",
    node: "Paso", removeNode: "Quitar paso", bot: "Bot", canvas: "Grafo del flujo", closePanel: "Cerrar",
    run: "Ejecutar", runTitle: (name: string) => `Ejecutar ${name}`, runPrompt: "Tarea para el primer bot", runPlaceholder: "Agrega un interruptor de modo oscuro en la página de ajustes", starting: "Iniciando…", cancel: "Cancelar",
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
        <span className="bot-dot" style={{ background: data.bot?.color || "var(--muted)" }} aria-hidden="true" />
        <span className="flow-node-title">{data.bot?.name ?? t.unknownBot}</span>
        {data.root && <span className="flow-badge">{t.start}</span>}
        {data.join && <span className="flow-badge">{t.join}</span>}
      </span>
      {data.bot?.description && <span className="flow-node-meta">{data.bot.description}</span>}
      <Handle type="source" position={Position.Bottom} />
    </div>
  );
}

const nodeTypes = { step: StepCard };
export const flowEdgeDefaults = { type: "smoothstep", markerEnd: { type: MarkerType.ArrowClosed, width: 18, height: 18 } } as const;

function edgeLabel(data: HandoffEdge["data"], reportsBack: string) {
  if (!data) return undefined;
  const instruction = data.instruction.length > 32 ? `${data.instruction.slice(0, 31)}…` : data.instruction;
  return [instruction, data.report_back ? `↩ ${reportsBack}` : ""].filter(Boolean).join(" · ") || undefined;
}

function toFlow(workflow: Workflow | undefined, bots: Bot[], reportsBack: string): { nodes: StepNode[]; edges: HandoffEdge[] } {
  const graph = workflow?.graph ?? { nodes: [], edges: [] };
  return {
    nodes: graph.nodes.map((node) => ({ id: node.id, type: "step", position: { x: node.x, y: node.y }, data: { bot: bots.find((b) => b.id === node.bot_id), root: false, join: false } })),
    edges: graph.edges.map(({ id, from, to, ...data }) => ({ ...flowEdgeDefaults, id, source: from, target: to, data, label: edgeLabel(data, reportsBack) })),
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
  const initial = useMemo(() => toFlow(workflow, bots, t.reportsBack), [workflow, bots, t.reportsBack]);
  const [nodes, setNodes, onNodesChange] = useNodesState<StepNode>(initial.nodes);
  const [edges, setEdges, onEdgesChange] = useEdgesState<HandoffEdge>(initial.edges);
  const [name, setName] = useState(workflow?.name ?? "");
  const [description, setDescription] = useState(workflow?.description ?? "");
  const [hopBudget, setHopBudget] = useState(workflow?.hop_budget ?? 30);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [running, setRunning] = useState(false);
  const [error, setError] = useState<unknown>();
  const [saved, setSaved] = useState(false);
  const [dirty, setDirty] = useState(!workflow);
  const flow = useReactFlow();

  const incoming = (nodeID: string) => edges.filter((edge) => edge.target === nodeID).length;
  const roots = nodes.filter((node) => incoming(node.id) === 0);
  const shownNodes = nodes.map((node) => ({ ...node, data: { ...node.data, root: roots.length === 1 && roots[0].id === node.id, join: incoming(node.id) > 1 } }));
  const selectedEdge = edges.find((edge) => edge.selected);
  const selectedNode = selectedEdge ? undefined : nodes.find((node) => node.selected);

  function changed() {
    setDirty(true);
    setSaved(false);
  }

  function fit() {
    window.requestAnimationFrame(() => flow.fitView({ padding: 0.3, maxZoom: 1, duration: 200 }));
  }

  function addBot(bot: Bot) {
    const lowest = nodes.reduce<StepNode | undefined>((low, node) => (!low || node.position.y > low.position.y ? node : low), undefined);
    const id = crypto.randomUUID();
    setNodes((current) => [
      ...current.map((node) => ({ ...node, selected: false })),
      { id, type: "step", selected: true, position: lowest ? { x: lowest.position.x, y: lowest.position.y + 170 } : { x: 0, y: 0 }, data: { bot, root: false, join: false } },
    ]);
    changed();
    fit();
  }

  function connect(connection: Connection) {
    if (connection.source === connection.target) return;
    const data = { report_back: true, instruction: "", timeout_minutes: 0 };
    setEdges((current) => addEdge({ ...connection, ...flowEdgeDefaults, id: crypto.randomUUID(), data, label: edgeLabel(data, t.reportsBack) }, current));
    changed();
  }

  function updateEdge(patch: Partial<WorkflowEdge>) {
    if (!selectedEdge) return;
    setEdges((current) =>
      current.map((edge) => {
        if (edge.id !== selectedEdge.id) return edge;
        const data = { ...edge.data!, ...patch };
        return { ...edge, data, label: edgeLabel(data, t.reportsBack) };
      }),
    );
    changed();
  }

  function deselect() {
    setNodes((current) => current.map((node) => ({ ...node, selected: false })));
    setEdges((current) => current.map((edge) => ({ ...edge, selected: false })));
  }

  function autoLayout() {
    const positions = layout(nodes.map((node) => node.id), edges.map((edge) => ({ from: edge.source, to: edge.target })));
    setNodes((current) => current.map((node) => ({ ...node, position: positions.get(node.id) ?? node.position })));
    changed();
    fit();
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

  async function save() {
    try {
      const result = workflow ? await api.patch<Workflow>(`/api/workflows/${workflow.id}`, body) : await api.post<Workflow>("/api/workflows", body);
      setError(undefined);
      setDirty(false);
      setSaved(true);
      window.setTimeout(() => setSaved(false), 1500);
      onSaved(result);
      return true;
    } catch (reason) {
      setError(reason);
      return false;
    }
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    await save();
  }

  // Run uses the saved workflow, so unsaved edits are saved first.
  async function run() {
    if (!dirty || (await save())) setRunning(true);
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

  return (
    <div className="stack">
      <BackLink />
      <form className="flow-workbench" onSubmit={submit} aria-label={workflow ? workflow.name : t.new}>
        <div className="flow-bar">
          <input className="flow-name" aria-label={t.name} placeholder={t.name} autoFocus={!workflow} required value={name} onChange={(event) => (setName(event.target.value), changed())} />
          <div className="flow-bar-actions">
            <Menu
              label={t.addBot}
              button={
                <>
                  <Icon name="plus" /> {t.addBot}
                </>
              }
            >
              {(close) =>
                bots.map((bot) => (
                  <button key={bot.id} type="button" role="menuitem" onClick={() => (close(), addBot(bot))}>
                    <span className="bot-dot" style={{ background: bot.color || "var(--muted)" }} aria-hidden="true" /> {bot.name}
                  </button>
                ))
              }
            </Menu>
            <button type="button" onClick={autoLayout} disabled={nodes.length === 0}>
              {t.layout}
            </button>
            <button type="button" aria-expanded={settingsOpen} onClick={() => setSettingsOpen((open) => !open)}>
              <Icon name="settings" /> {t.settings}
            </button>
            {workflow && <IconButton type="button" className="ghost danger-text" icon="trash" label={t.delete(workflow.name)} onClick={remove} />}
            <button className={workflow ? "" : "primary"} disabled={!dirty || nodes.length === 0 || roots.length !== 1}>
              {saved ? t.saved : workflow ? t.save : t.create}
            </button>
            {workflow && (
              <button type="button" className="primary" disabled={nodes.length === 0 || roots.length !== 1} onClick={run}>
                <Icon name="play" /> {t.run}
              </button>
            )}
          </div>
        </div>
        {settingsOpen && (
          <div className="flow-settings">
            <label className="grow">
              {t.description}
              <input value={description} onChange={(event) => (setDescription(event.target.value), changed())} />
            </label>
            <label title={t.hopBudgetTitle}>
              {t.hopBudget}
              <input type="number" min={1} max={500} required value={hopBudget} onChange={(event) => (setHopBudget(Number(event.target.value)), changed())} />
            </label>
          </div>
        )}
        <div className="flow-stage" aria-label={t.canvas}>
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
            fitViewOptions={{ padding: 0.3, maxZoom: 1 }}
            proOptions={{ hideAttribution: true }}
          >
            <Background gap={20} />
            <Controls showInteractive={false} position="bottom-left" />
          </ReactFlow>
          {nodes.length === 0 && <p className="flow-empty muted">{t.emptyCanvas}</p>}
          {(selectedEdge || selectedNode) && (
            <aside className="flow-inspector" aria-label={selectedEdge ? t.edge : t.node}>
              <div className="flow-inspector-head">
                <h3>{selectedEdge ? t.edge : t.node}</h3>
                <IconButton type="button" className="ghost" icon="x" label={t.closePanel} onClick={deselect} />
              </div>
              {selectedEdge && (
                <>
                  <label className="flow-check">
                    <input type="checkbox" checked={selectedEdge.data!.report_back} onChange={(event) => updateEdge({ report_back: event.target.checked })} /> {t.reportBack}
                  </label>
                  <label>
                    {t.instruction}
                    <textarea rows={3} placeholder={t.instructionPlaceholder} value={selectedEdge.data!.instruction} onChange={(event) => updateEdge({ instruction: event.target.value })} />
                  </label>
                  <label title={t.timeoutTitle}>
                    {t.timeout}
                    <input type="number" min={0} max={1440} value={selectedEdge.data!.timeout_minutes} onChange={(event) => updateEdge({ timeout_minutes: Number(event.target.value) })} />
                  </label>
                  <button type="button" className="ghost danger-text flow-remove" onClick={() => (setEdges((current) => current.filter((edge) => edge.id !== selectedEdge.id)), changed())}>
                    <Icon name="trash" /> {t.removeEdge}
                  </button>
                </>
              )}
              {selectedNode && (
                <>
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
                    className="ghost danger-text flow-remove"
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
        <p className="muted flow-hint">{nodes.length > 0 && roots.length !== 1 ? t.noRoot : t.hint}</p>
        <ErrorText error={error} />
      </form>
      {running && workflow && <RunDialog workflow={workflow} onClose={() => setRunning(false)} />}
      {workflow && <History itemType="workflow" itemId={workflow.id} refresh={refresh} />}
    </div>
  );
}

function RunDialog({ workflow, onClose }: { workflow: Workflow; onClose: () => void }) {
  const t = useText(text);
  const dialog = useRef<HTMLDialogElement>(null);
  const [prompt, setPrompt] = useState("");
  const [error, setError] = useState<unknown>();
  const [starting, setStarting] = useState(false);

  useEffect(() => {
    if (!dialog.current?.open) dialog.current?.showModal();
  }, []);

  async function run(event: FormEvent) {
    event.preventDefault();
    if (starting) return;
    setStarting(true);
    try {
      const result = await api.post<{ chat_id: string }>(`/api/workflows/${workflow.id}/run`, { prompt });
      dialog.current?.close();
      navigate(`/ai/chats/${result.chat_id}`);
    } catch (reason) {
      setError(reason);
      setStarting(false);
    }
  }

  return (
    <dialog ref={dialog} className="create-dialog" aria-labelledby="run-workflow-title" onClose={onClose} onClick={(event) => event.target === dialog.current && dialog.current.close()}>
      <form className="panel create create-modal" onSubmit={run}>
        <div className="page-heading">
          <h2 id="run-workflow-title">{t.runTitle(workflow.name)}</h2>
          <IconButton type="button" className="ghost" icon="x" label={t.closePanel} onClick={() => dialog.current?.close()} />
        </div>
        <label>
          {t.runPrompt}
          <textarea autoFocus rows={4} required placeholder={t.runPlaceholder} value={prompt} onChange={(event) => setPrompt(event.target.value)} />
        </label>
        <ErrorText error={error} />
        <div className="modal-actions">
          <button type="button" onClick={() => dialog.current?.close()}>
            {t.cancel}
          </button>
          <button className="primary" disabled={!prompt.trim() || starting} aria-busy={starting}>
            {starting ? <span className="spinner" aria-hidden="true" /> : <Icon name="play" />} {starting ? t.starting : t.run}
          </button>
        </div>
      </form>
    </dialog>
  );
}
