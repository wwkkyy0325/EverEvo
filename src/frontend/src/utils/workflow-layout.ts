// Directed-graph auto-layout for the workflow canvas, via dagre.

import dagre from 'dagre'

// Approximate the CSS node box: width is min-width 160 / max-width 220,
// height is content-driven (~46-56px). dagre needs fixed sizes for spacing.
export const NODE_W = 200
export const NODE_H = 56
const GRID = 20 // matches Vue Flow snap-grid [20,20]

export interface LayoutNode {
  id: string
  position?: { x: number; y: number }
}
export interface LayoutEdge {
  source: string
  target: string
}

/**
 * Full directed-graph layout. Returns a deterministic top-left pixel coordinate
 * per node id, snapped to the 20px grid.
 *
 * Call this whenever one or more nodes lack a position (first load, or the LLM
 * added nodes). When every node already has a position, skip it to preserve the
 * user's hand arrangement.
 */
export function autoLayout(nodes: LayoutNode[], edges: LayoutEdge[]): Record<string, { x: number; y: number }> {
  const g = new dagre.graphlib.Graph()
  // rankdir TB matches the default handles: target=Top, source=Bottom.
  g.setGraph({ rankdir: 'TB', nodesep: 40, ranksep: 60, marginx: 20, marginy: 20 })
  g.setDefaultEdgeLabel(() => ({}))
  for (const n of nodes) {
    g.setNode(n.id, { width: NODE_W, height: NODE_H })
  }
  for (const e of edges) {
    // Only connect nodes present in the graph; a stale edge (pointing at a
    // just-removed node) would otherwise make dagre throw.
    if (g.hasNode(e.source) && g.hasNode(e.target)) {
      g.setEdge(e.source, e.target)
    }
  }
  dagre.layout(g)

  const snap = (v: number) => Math.round(v / GRID) * GRID
  const out: Record<string, { x: number; y: number }> = {}
  for (const id of g.nodes()) {
    const node: any = g.node(id)
    // dagre yields the node center; Vue Flow positions by top-left corner.
    out[id] = {
      x: snap(node.x - node.width / 2),
      y: snap(node.y - node.height / 2),
    }
  }
  return out
}
