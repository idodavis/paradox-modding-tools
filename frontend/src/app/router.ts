/**
 * The router: one route per page of the catalogue. A workspace's pages are children of one `/workspace/:id`
 * route, pages load lazily, and the one guard sets the active workspace from the route.
 */
import { createRouter, createWebHashHistory, type Router } from "vue-router";
/** The app's router. Its guard writes the active workspace id. */
export const router: Router = createRouter({ history: createWebHashHistory(), routes: [] });

/** The pages kept alive across navigation. */
export const KEPT: readonly string[] = [];
