# Frontend conventions

How every file under `frontend/src` is written. The look is `.claude/DESIGN.md`'s; the product terms are
`GLOSSARY.md`'s. A file that breaks a rule here is fixed by the stage that rewrites it
(`.claude/plans/frontend-revamp/frontend-revamp.md`, O-R30).

## Server state

Every binding read is a Pinia Colada `useQuery` built from one `defineQueryOptions` per RPC in `queries.ts`,
the one place for keys (a key factory per service). Stores hold client state only: no `loading`, `error`,
`refresh` or copy of server data. After a mutation, invalidate the keys it changes. Query functions are
pure: no store writes, no `EnsureSession`.

```ts
// queries.ts
export const workspaceKeys = { one: (id: string) => ["workspace", id] as const };
export const workspaceQuery = defineQueryOptions((id: string) => ({
  key: workspaceKeys.one(id),
  query: () => GetWorkspace(id),
}));

// a page, whose prop is id
const { data: workspace } = useQuery(() => workspaceQuery(id));
const { mutate: saveOrder, isLoading: saving } = useMutation({
  mutation: (ids: string[]) => ReorderWorkspaceMods(id, ids),
  onSettled: () => queryCache.invalidateQueries({ key: workspaceKeys.one(id) }),
});
```

## Mutations

Templates and handlers call `mutate`, never `mutateAsync`. Errors reach the one `onError` in `main.ts`'s
`PiniaColada({ mutationOptions })`, which shows a toast: no try/catch and toast per site, no
`e instanceof Error ? …` expression.

```vue
<UButton label="Save order" :loading="saving" @click="saveOrder(order)" />
```

## Routes

Workspace pages are children of one `/workspace/:id` route and take `id` as a prop (`props: true`);
nothing reads `route.params` for it. The top bar is App's, on every page. The router's `beforeEach` alone
sets the active workspace. Route names are typed (`RouteNamedMap`) and come from the page catalogue
(`workspaceTools.ts`); pages load lazily; one `KeepAlive` with `include`. A page's facts in the status bar
go through `StatusFacts`, which leaves with a kept-alive page. State a link names (a Settings section, a
Health card and row) is the route's query, handed to the page by a `props` function, and written back with
`router.replace`.

```ts
{ path: "graph", name: "event-graph", component: () => import("./pages/EventGraphPage.vue"), props: true }
```

```vue
<script setup lang="ts">
const { id } = defineProps<{ id: string }>();
</script>
```

## Wails events

Subscribed once, in `events.ts`, called from `main.ts`; each event invalidates its queries. Payloads are
typed by Go's `application.RegisterEvent`, which the bindings declare on `Events.CustomEvents`: no casts.

```ts
Events.On("lang:ready", ({ data: id }) => void queryCache.invalidateQueries({ key: sessionKeys.live(id) }));
```

## Composables

`useX`, one purpose; inputs are `MaybeRefOrGetter` read with `toValue`. Listeners and timers through VueUse
(`useEventListener`, `useIntervalFn`) or `onScopeDispose`. An async watcher is a query, or uses `onCleanup`.

```ts
export function useLanguageHealth(workspaceId: MaybeRefOrGetter<string>) {
  const { data: health } = useQuery(() => languageQuery(toValue(workspaceId)));
```

## Components

`<script setup lang="ts">`; type-based `defineProps` and `defineEmits`; `defineModel` for a v-model pair;
destructured prop defaults (no `withDefaults`); `useTemplateRef`; no `defineOptions({ name })`; `shallowRef`
for primitives and for values replaced whole. No `any`, no `!`, no `as unknown as`. Templates call named
functions; a derived value is a `computed`, never a watcher that assigns.

```vue
<script setup lang="ts">
const { size = "md" } = defineProps<{ size?: "sm" | "md" }>();
const open = defineModel<boolean>("open", { default: false });
const list = useTemplateRef("list");
</script>
```

## Nuxt UI

Overlays through `useOverlay`, toasts through `useToast`. Sections, steps and segmented choices are `UTabs`,
`UStepper` or `URadioGroup` with `:items` and item slots; lists are `UListbox` or `UCollapsible`; a link
action is `UButton variant="link"`; a resizable pane is `ResizeGrip`. Nuxt UI's components and composables are
auto-imported (no deep `@nuxt/ui/…` import), and so are this folder's: a template uses a component under
`components/` by its file name and never imports it; only a script that needs the component itself (for
`useOverlay().create`) imports it. Colors by aliases (`primary`, `neutral`, …) and the tokens; theme
keys in `vite.config.ts`'s `ui()`, never `ui.base` overrides.

```ts
const remove = useOverlay().create(RemoveWorkspaceModal);
const confirmed = await remove.open({ name: workspace.value.name });
```

## Display data

From the payload (AGENTS.md): no slug, rel, basename or MIME derivation the payload could carry. Origins
through the workspace store's `originName` and `originColor`; external links through `Browser.OpenURL`
only; counts, lists, dates and sizes through `lib/format.ts` (`plural`, `list`, `day`, `bytes`), and
`useTimeAgo` for "4 min ago".

```vue
<OriginDot :color="store.originColor(site.origin)" /> {{ store.originName(site.origin) }} {{ site.rel }}
```

## Settings and storage

Settings keys are read and written only through `stores/settings.ts`'s typed accessors. Browser storage keys
share the prefix `pmt.`.

```ts
const layout = useLocalStorage<"drawer" | "panel">("pmt.graph.layout", "drawer");
```

## Styles

Screens are composed of the building blocks (`.claude/DESIGN.md`); a page adds no style a block covers. Token
utilities only: no hex or `rgb()` color outside `styles/themes.css`, no Tailwind palette color
(`text-gray-500`), no arbitrary value (`w-[420px]`), no `uppercase`, no weight but 400, 500 and 600. A block
is a component only for structure or logic it repeats; classes alone stay classes, as Nuxt UI's own components
take their look from `vite.config.ts`'s `ui()`.

```vue
<div class="rounded-lg border border-default bg-muted px-4 py-3">
  <p class="text-13 text-toned">Mods load top to bottom; the last one wins.</p>
</div>
```

## Smallest

No forwarder computeds or wrappers. A file has one purpose, stated in its header comment.
