/**
 * The graph's layout: Go's nodes and edges laid out left to right by step, as the canvas draws them. Nothing else
 * about the graph is computed here; the reach, the steps and the splicing are Go's.
 */
import type { MaybeRefOrGetter } from "vue";
import type { Edge as FlowEdge, Node as FlowNode } from "@vue-flow/core";

import type { Graph } from "@services/graph/models";

/** Lays a graph out. The result follows the graph when it changes. */
export function useEventGraph(graph: MaybeRefOrGetter<Graph | undefined>): { nodes: FlowNode[]; edges: FlowEdge[] } {
  throw new Error("stub", { cause: graph });
}
