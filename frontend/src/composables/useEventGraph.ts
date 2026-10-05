/**
 * The Event Graph page's state: what the graph is drawn from and how far, the stubs opened, the
 * selected node, the graph, detail and root-search queries, and where dagre puts each node.
 */
import { computed, ref, shallowRef, toValue, type MaybeRefOrGetter } from "vue";
import { useQuery } from "@pinia/colada";
import { sessionKeys } from "../queries";
import { refDebounced } from "@vueuse/core";
import { Graph, layout } from "@dagrejs/dagre";
import { GetEventDetail, GetEventGraph, SearchRoots } from "@services/eventgraphservice";

/** Every node's box on the canvas; dagre lays them out by it. */
export const NODE_WIDTH = 260;
export const NODE_HEIGHT = 112;

// a kind's name picks its color, so a kind no list here knows still gets one of its own
const PALETTE = ["info", "success", "warning", "secondary", "primary", "error"];

/** A kind's color, as a tint of one of the theme's colors. */
export function kindColor(kind: string, strength = 22): string {
  let hash = 0;
  for (const char of kind) hash = (hash * 31 + char.charCodeAt(0)) >>> 0;
  return `color-mix(in oklab, var(--ui-${PALETTE[hash % PALETTE.length]}) ${strength}%, transparent)`;
}

export function useEventGraph(workspaceId: MaybeRefOrGetter<string>, live: MaybeRefOrGetter<boolean>) {
  const params = ref({
    root: "",
    depth: 3,
    fanout: 12,
    maxNodes: 40,
    expand: [] as string[],
    origins: [] as string[],
    scripted: false,
  });
  const selected = shallowRef("");
  const searchTerm = shallowRef("");
  const rootKind = shallowRef("");
  const search = refDebounced(searchTerm, 200);

  const { data: graph, error } = useQuery({
    key: () => sessionKeys.graph(toValue(workspaceId), { ...params.value }),
    query: () => GetEventGraph(toValue(workspaceId), params.value),
    enabled: () => toValue(live),
    placeholderData: (previous) => previous,
  });
  const { data: detail } = useQuery({
    key: () => sessionKeys.eventDetail(toValue(workspaceId), selected.value),
    query: () => GetEventDetail(toValue(workspaceId), selected.value),
    enabled: () => toValue(live) && !!selected.value,
  });
  const { data: roots, isLoading: searching } = useQuery({
    key: () => sessionKeys.roots(toValue(workspaceId), search.value, rootKind.value),
    query: () => SearchRoots(toValue(workspaceId), search.value, rootKind.value),
    enabled: () => toValue(live),
    placeholderData: (previous) => previous,
  });

  // dagre's top-left corner for each node, the graph read left to right
  const positions = computed(() => {
    const dag = new Graph();
    dag.setGraph({ rankdir: "LR", nodesep: 56, ranksep: 180, marginx: 32, marginy: 32 });
    dag.setDefaultEdgeLabel(() => ({}));
    for (const node of graph.value?.nodes ?? []) dag.setNode(node.id, { width: NODE_WIDTH, height: NODE_HEIGHT });
    for (const edge of graph.value?.edges ?? []) dag.setEdge(edge.from, edge.to);
    layout(dag);
    return new Map(
      dag.nodes().map((id) => [id, { x: dag.node(id).x - NODE_WIDTH / 2, y: dag.node(id).y - NODE_HEIGHT / 2 }]),
    );
  });

  /** A click: a stub opens what it hides; anything else is selected. */
  function select(id: string): void {
    if (id.startsWith("more:") || id.startsWith("more-in:")) {
      params.value = { ...params.value, expand: [...params.value.expand, id] };
      return;
    }
    selected.value = id;
  }

  /** Draw the graph around another definition, and select it. */
  function setRoot(id: string): void {
    params.value = { ...params.value, root: id, expand: [] };
    selected.value = id;
  }

  return { params, selected, searchTerm, rootKind, graph, error, detail, roots, searching, positions, select, setRoot };
}
