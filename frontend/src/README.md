# Frontend conventions

How every file under `frontend/src` is written, and what checks each rule. The look is set by the design notes
(`.claude/DESIGN.md`, not in the repo); its tokens are `styles/themes.css`. The product terms are
[GLOSSARY.md](../../GLOSSARY.md)'s.

## What checks what

| check           | runs                                    | covers                                                                                                                                                                                                            |
| --------------- | --------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `npm run check` | vue-tsc over `.ts` and `.vue`           | types, the typed route names, the bindings' payloads                                                                                                                                                              |
| `npm run lint`  | eslint, then `prettier --check`         | the import rules (Layout), `vue/no-mutating-props`, `no-floating-promises`, `no-non-null-assertion`, typescript-eslint's recommended type-checked set (`no-explicit-any`, the `no-unsafe-*` rules) and formatting |
| `npm test`      | vitest                                  | the stores' and the editor's tab logic                                                                                                                                                                            |
| review          | a person or an agent reading the change | every rule below that names no check                                                                                                                                                                              |

`task check:frontend` runs the three commands; `task check` runs them with the backend's.

## Layout

Each page is a folder under `pages/` (`library/`, `wizard/`, `settings/`, `health/`, `graph/`, `notes/`, `publish/`,
`ide/`) holding the page and the components only it uses. The app's frame (top bar, status bar, command palette) is
`app/`, with the router, the events' refreshes and the page catalogue (`pages.ts`); the editor's modules are `editor/`;
a component two or more pages use is in `ui/`. `stores/`, `composables/` and `lib/` stay flat; the root holds `main.ts`
and `queries.ts`.

A page imports from `stores/`, `queries.ts`, `lib/`, `composables/`, `ui/` and itself, never another page's folder and
never the frame's components. `editor/` is imported only by `pages/ide/` and the stores; `ui/`, `composables/` and
`lib/` import no page, frame or editor module; the frame and the stores reach a page only through the router. Two
exceptions: the page catalogue `app/pages.ts` is imported by pages and `ui/` as data, and `app/events.ts` imports
`editor/models.ts` to send the open files again.

_Checked by:_ partly, eslint's `no-restricted-imports` blocks in `frontend/eslint.config.js`: they allow
`app/pages.ts` by name, and no block covers the frame importing `editor/`; the rest is review.

## Server state

Every binding read is a Pinia Colada `useQuery` built from one `defineQueryOptions` per RPC in `queries.ts`, the one
place for keys: each option writes its own, and a key factory exists only for a key that an event or a mutation
refreshes. Stores hold client state only: no `loading`, `error`, `refresh` or copy of server data. A write to
`config.json` refreshes what it feeds through `config:changed`; any other mutation invalidates the keys it changes.
Query functions make no store writes; a read of the open workspace's index is enabled by the store's `isOpen`.

```ts
// queries.ts
export const releaseKeys = { mod: (workspaceId: string, modId: string) => ["release", workspaceId, modId] as const };
export const listingQuery = defineQueryOptions(({ workspaceId, modId }: { workspaceId: string; modId: string }) => ({
  key: [...releaseKeys.mod(workspaceId, modId), "listing"],
  query: () => LoadListing(workspaceId, modId),
}));

// a component, whose props are workspaceId and modId
const { data: listing } = useQuery(() => listingQuery({ workspaceId, modId }));
const { mutate: create } = useMutation({
  mutation: () => CreateDescriptionFile(workspaceId, modId), // the mod's file, not config.json
  onSettled: () => queryCache.invalidateQueries({ key: releaseKeys.mod(workspaceId, modId) }),
});
```

_Checked by:_ review.

## Mutations

Templates and handlers call `mutate`, never `mutateAsync`. Errors reach the one `onError` in `main.ts`'s
`PiniaColada({ mutationOptions })`, which shows a toast: no try/catch and toast per site, no `e instanceof Error ? …`
expression.

```vue
<UButton label="Save order" :loading="saving" @click="saveOrder(order)" />
```

_Checked by:_ review; `no-floating-promises` catches an unhandled promise.

## Routes

Workspace pages are children of one `/workspace/:id` route and take `id` as a prop (`props: true`); nothing reads
`route.params` for it. The top bar is App's, on every page. The router's `beforeEach` alone sets the active workspace.
Route names are typed (`RouteNamedMap`) and come from the page catalogue (`app/pages.ts`); pages load lazily; one
`KeepAlive` with `include`. A page's facts in the status bar go through `StatusFacts`, which leaves with a kept-alive
page. State a link names (a Settings section, a Health card and row) is the route's query, handed to the page by a
`props` function, and written back with `router.replace`.

```ts
{ path: "graph", name: "event-graph", component: () => import("../pages/graph/EventGraphPage.vue"), props: true }
```

```vue
<script setup lang="ts">
const { id } = defineProps<{ id: string }>();
</script>
```

_Checked by:_ vue-tsc for the route names; the rest review.

## Wails events

Subscribed once, in `app/events.ts`, called from `main.ts`; each event refreshes what it changes (its queries, the scan
progress, the git store). The editor's subscriptions stay with the module that draws them: `fs:changed` in
`editor/models.ts`, `search:hits` in `editor/search.ts`; a component's own stay in it: Publish's upload progress
(`steam:progress`) and the status bar's wiki downloads (`wiki:progress`). The windows' events are not `app/events.ts`'s:
`stores/windows.ts` answers them, the ones about windows (`window:opened`, `window:state`, `window:closed`,
`window:closing`, `window:show`, `window:saved`) and the ones about tabs (`window:tabs-handed`, `window:tab-requested`),
but for another window's search results (`window:results`), which `editor/search.ts` answers; the protocol table is at
the head of `stores/windows.ts`. Payloads are typed by Go's `application.RegisterEvent`, which the bindings declare on
`Events.CustomEvents`: no casts.

```ts
Events.On("workspace:opened", ({ data: id }) => void queryCache.invalidateQueries({ key: workspaceKeys.open(id) }));
```

_Checked by:_ vue-tsc for the payload types; the rest review.

## Composables

`useX`, one purpose; inputs are `MaybeRefOrGetter` read with `toValue`. Listeners and timers through VueUse
(`useEventListener`, `useIntervalFn`) or `onScopeDispose`. An async watcher is a query, or uses `onCleanup`.

```ts
export function useIdeRoots(workspaceId: MaybeRefOrGetter<string>) {
  const { data } = useQuery(() => ideRootsQuery(toValue(workspaceId)));
```

_Checked by:_ review.

## Components

`<script setup lang="ts">`; type-based `defineProps` and `defineEmits`; `defineModel` for a v-model pair; destructured
prop defaults (no `withDefaults`); `useTemplateRef`; no `defineOptions({ name })`; `shallowRef` for primitives and for
values replaced whole. No `any`, no `!`, no `as unknown as`. Templates call named functions; a derived value is a
`computed`, never a watcher that assigns.

```vue
<script setup lang="ts">
const { size = "md" } = defineProps<{ size?: "sm" | "md" }>();
const open = defineModel<boolean>("open", { default: false });
const list = useTemplateRef("list");
</script>
```

_Checked by:_ `no-explicit-any`, `no-non-null-assertion` and `vue/no-mutating-props`; the rest review.

## Nuxt UI

Overlays through `useOverlay`, toasts through `useToast`. Sections, steps and segmented choices are `UTabs`, `UStepper`
or `URadioGroup` with `:items` and item slots; lists are `UListbox` or `UCollapsible`; a link action is
`UButton variant="link"`; resizable panes are a `USplitter` (its handle themed in `vite.config.ts`, its sizes kept by
`auto-save-id`). Nuxt UI's components and composables are auto-imported (no deep `@nuxt/ui/…` import); our components
are imported explicitly, in the `<script setup>` of the file whose template uses them. Colors by aliases (`primary`,
`neutral`, …) and the tokens; theme keys in `vite.config.ts`'s `ui()`, never `ui.base` overrides.

```ts
const remove = useOverlay().create(RemoveWorkspaceModal);
const confirmed = await remove.open({ name: workspace.value.name });
```

_Checked by:_ review.

## Display data

From the payload (AGENTS.md): no slug, rel, basename or MIME derivation the payload could carry. Origins through the
workspace store's `originName` and `originColor`; external links through `Browser.OpenURL` only; counts, lists, dates
and sizes through `lib/format.ts` (`plural`, `list`, `day`, `bytes`), and `useTimeAgo` for "4 min ago".

```vue
<OriginDot :color="store.originColor(site.origin)" />
{{ store.originName(site.origin) }} {{ site.rel }}
```

_Checked by:_ review.

## Settings and storage

Settings keys are read and written only through `stores/settings.ts`'s typed accessors. Browser storage keys share the
prefix `pmt.`.

```ts
const layout = useLocalStorage<"drawer" | "panel">("pmt.graph.layout", "drawer");
```

_Checked by:_ review.

## Styles

Screens are composed of the shared building blocks (the design notes); a page adds no style a block covers. Token
utilities only: no hex or `rgb()` color outside `styles/themes.css`, no Tailwind palette color (`text-gray-500`), no
arbitrary value (`w-[420px]`), no `uppercase`, no weight but 400, 500 and 600. A block is a component only for
structure or logic it repeats; classes alone stay classes, as Nuxt UI's own components take their look from
`vite.config.ts`'s `ui()`.

```vue
<div class="rounded-lg border border-default bg-muted px-4 py-3">
  <p class="text-13 text-toned">Mods load top to bottom; the last one wins.</p>
</div>
```

_Checked by:_ review.

## Where to change what

| To change                     | Edit                                                                                                                                                   |
| ----------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ |
| a theme token, or add a theme | `styles/themes.css` (a block of tokens per `data-theme`; the family list is in `stores/settings.ts`)                                                   |
| a window event                | `services/windowservice.go` declares it; the table at the head of `stores/windows.ts` says who answers                                                 |
| what a page reads from Go     | its option in `queries.ts`, then the component's `useQuery`                                                                                            |
| what an event refreshes       | `app/events.ts`                                                                                                                                        |
| a command in the palette      | `app/CommandPalette.vue` builds the pages', workspaces' and settings' rows; the IDE's are registered by `editor/commands.ts` into `stores/commands.ts` |
| the hover card's look         | `pages/ide/HoverCard.vue`; its words are the backend's (`services/internal/lsp/wording.go`)                                                            |
| a Health section              | `pages/health/` and `composables/useHealth.ts`; its rows are `services/internal/pages/health.go` and `health_*.go`                                     |
| the event graph               | `pages/graph/` and `composables/useEventGraph.ts`                                                                                                      |
| a setting                     | its typed accessor in `stores/settings.ts`                                                                                                             |

## Smallest

No forwarder computeds or wrappers. A file has one purpose, stated in its header comment.

_Checked by:_ review.
