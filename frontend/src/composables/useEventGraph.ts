/**
 * The Event Graph page's state: the root and how far the graph reaches, the kinds hidden in the page, the
 * stubs opened, the selected node, and where dagre puts each node, left to right by step.
 */
import { computed, ref, shallowRef, toValue, type MaybeRefOrGetter } from "vue";
import { useQuery } from "@pinia/colada";
import { refDebounced } from "@vueuse/core";
import { Graph, layout } from "@dagrejs/dagre";
import type { Edge, Node, Params } from "@services/internal/pages/models";
import { eventDetailQuery, graphQuery, rootsQuery } from "../queries";
import { useSettingsStore } from "../stores/settings";

/** Each kind's color and icon, on the graph and in the detail; any other kind is quiet. */
const KINDS: Record<string, { color: string; icon: string }> = {
  event: { color: "var(--blue)", icon: "i-lucide-scroll" },
  "on action": { color: "var(--green)", icon: "i-lucide-timer" },
  "scripted effect": { color: "var(--purple)", icon: "i-lucide-zap" },
};
export function kindOf(kind: string): { color: string; icon: string } {
  return KINDS[kind] ?? { color: "var(--fg-3)", icon: "i-lucide-box" };
}

/** What a node is drawn as: events are cards, a stub a dashed group, the rest pills. */
type Shape = "card" | "pill" | "group";
export function shapeOf(node: Node): Shape {
  if (node.stub) return "group";
  return node.kind === "event" ? "card" : "pill";
}
/** Each shape's box at 100%; the interface scale scales it as it scales the nodes. */
const BOX: Record<Shape, { width: number; height: number }> = {
  card: { width: 196, height: 56 },
  pill: { width: 176, height: 26 },
  group: { width: 176, height: 26 },
};

/** Each node a start reaches along the fires, forward or back, by its step from the start. */
export function reach(edges: Edge[], start: string, forward: boolean): Map<string, number> {
  const step = new Map([[start, 0]]);
  for (let frontier = [start], depth = 1; frontier.length; depth++) {
    const next: string[] = [];
    for (const edge of edges) {
      const [source, target] = forward ? [edge.from, edge.to] : [edge.to, edge.from];
      if (frontier.includes(source) && !step.has(target)) {
        step.set(target, depth);
        next.push(target);
      }
    }
    frontier = next;
  }
  return step;
}

/** The page's defaults: three steps out, 120 nodes, fan-outs past six folded, macros followed. */
const DEFAULTS = { depth: 3, maxNodes: 120, fanout: 6, origins: [] as string[], scripted: true, follow: true };

export function useEventGraph(workspaceId: MaybeRefOrGetter<string>, live: MaybeRefOrGetter<boolean>) {
  const params = ref<Params>({ root: "", expand: [], ...DEFAULTS });
  const hidden = ref<string[]>([]);
  const selected = shallowRef("");
  const searchTerm = shallowRef("");
  const search = refDebounced(searchTerm, 200);
  const settings = useSettingsStore();

  const { data: graph, error } = useQuery(() =>
    graphQuery({ id: toValue(workspaceId), params: params.value, live: toValue(live) }),
  );
  const { data: detail } = useQuery(() =>
    eventDetailQuery({ id: toValue(workspaceId), event: selected.value, live: toValue(live) }),
  );
  const { data: roots, isLoading: searching } = useQuery(() =>
    rootsQuery({ id: toValue(workspaceId), search: search.value, live: toValue(live) }),
  );

  // the graph without the kinds hidden in the page, and the fires among what is left
  const visibleGraph = computed(() => {
    const nodes = (graph.value?.nodes ?? []).filter((node) => !hidden.value.includes(node.kind));
    const ids = new Set(nodes.map((node) => node.id));
    return { nodes, edges: (graph.value?.edges ?? []).filter((edge) => ids.has(edge.from) && ids.has(edge.to)) };
  });

  // each node's step from the root along the fires: what the root reaches after it, what reaches the root
  // before it (a chain of macros into it one step each), and a side branch one step after what fires it
  const steps = computed(() => {
    const { edges } = visibleGraph.value;
    const root = params.value.root;
    const step = reach(edges, root, true);
    for (const [id, back] of reach(edges, root, false)) if (!step.has(id)) step.set(id, -back);
    for (let added = true; added;) {
      added = false;
      for (const edge of edges) {
        const sourceStep = step.get(edge.from);
        if (sourceStep !== undefined && !step.has(edge.to)) {
          step.set(edge.to, sourceStep + 1);
          added = true;
        }
      }
    }
    return step;
  });

  // dagre's top-left corner for each node, laid out by step: dagre is given the fires one step forward, so its
  // columns are the steps, and those back to an earlier step turned around, so it orders the nodes for them too and
  // routes them through the gaps (routes: a fire back's points, and whether it goes back or stays in its column)
  const laid = computed(() => {
    const unit = settings.fontScale / 100;
    const dag = new Graph();
    dag.setGraph({ rankdir: "LR", nodesep: 12 * unit, ranksep: 60 * unit, marginx: 20 * unit, marginy: 40 * unit });
    dag.setDefaultEdgeLabel(() => ({}));
    for (const node of visibleGraph.value.nodes) {
      const box = BOX[shapeOf(node)];
      dag.setNode(node.id, { width: box.width * unit, height: box.height * unit });
    }
    const stepsOf = (edge: Edge) => [steps.value.get(edge.from) ?? 0, steps.value.get(edge.to) ?? 0];
    for (const edge of visibleGraph.value.edges) {
      const [from, to] = stepsOf(edge);
      if (to > from) dag.setEdge(edge.from, edge.to);
      else if (to < from) dag.setEdge(edge.to, edge.from, { minlen: from - to });
    }
    layout(dag);
    const routes = new Map(
      visibleGraph.value.edges.map((edge) => {
        const [from, to] = stepsOf(edge);
        // a fire back leaves its source's left side and meets its target's right, through dagre's bends between
        const [source, target] = [dag.node(edge.from), dag.node(edge.to)];
        const bends = to < from ? (dag.edge(edge.to, edge.from)?.points ?? []).slice(1, -1).reverse() : [];
        const points = bends.length
          ? [{ x: source.x - source.width / 2, y: source.y }, ...bends, { x: target.x + target.width / 2, y: target.y }]
          : [];
        return [`${edge.from}->${edge.to}`, { points, back: to <= from }];
      }),
    );
    const centers = [...new Set(dag.nodes().map((id) => Math.round(dag.node(id).x)))].sort((a, b) => a - b);
    const rootColumn = centers.indexOf(Math.round(dag.node(params.value.root)?.x ?? NaN));
    const top = Math.min(...dag.nodes().map((id) => dag.node(id).y - dag.node(id).height / 2));
    return {
      routes,
      positions: new Map(
        dag
          .nodes()
          .map((id) => [
            id,
            { x: dag.node(id).x - dag.node(id).width / 2, y: dag.node(id).y - dag.node(id).height / 2 },
          ]),
      ),
      // a column's head is its step from the root's; the callers' columns stand before it
      heads:
        rootColumn < 0
          ? []
          : centers.map((x, i) => ({ x: x - (BOX.card.width * unit) / 2, y: top - 28 * unit, step: i - rootColumn })),
    };
  });

  /** A click: a stub opens what it hides; anything else is selected. */
  function select(node: Node): void {
    if (node.stub) params.value = { ...params.value, expand: [...(params.value.expand ?? []), node.id] };
    else selected.value = node.id;
  }

  /** Draw the graph around another definition, and select it. */
  function setRoot(id: string): void {
    params.value = { ...params.value, root: id, expand: [] };
    selected.value = id;
  }

  /** The filters back as the page starts: every kind, every origin, three steps, 120 nodes. */
  function resetFilters(): void {
    params.value = { ...params.value, ...DEFAULTS };
    hidden.value = [];
  }
  // how many of the four filters differ from their defaults
  const filtersOn = computed(
    () =>
      [
        hidden.value.length > 0 || params.value.scripted !== DEFAULTS.scripted,
        !!params.value.origins?.length,
        params.value.depth !== DEFAULTS.depth,
        params.value.maxNodes !== DEFAULTS.maxNodes,
      ].filter(Boolean).length,
  );

  return {
    params,
    hidden,
    selected,
    searchTerm,
    graph,
    error,
    detail,
    roots,
    searching,
    visibleGraph,
    laid,
    filtersOn,
    select,
    setRoot,
    resetFilters,
  };
}
