import dagre from "@dagrejs/dagre";

export const nodeWidth = 200;
export const nodeHeight = 100;

// Top-to-bottom positions (top-left corners, as React Flow expects) for a graph.
export function layout(ids: string[], edges: { from: string; to: string }[]): Map<string, { x: number; y: number }> {
  const graph = new dagre.graphlib.Graph();
  graph.setGraph({ rankdir: "TB", nodesep: 60, ranksep: 90 });
  graph.setDefaultEdgeLabel(() => ({}));
  for (const id of ids) graph.setNode(id, { width: nodeWidth, height: nodeHeight });
  for (const edge of edges) if (graph.hasNode(edge.from) && graph.hasNode(edge.to)) graph.setEdge(edge.from, edge.to);
  dagre.layout(graph);
  return new Map(ids.map((id) => {
    const node = graph.node(id);
    return [id, { x: node.x - nodeWidth / 2, y: node.y - nodeHeight / 2 }];
  }));
}
