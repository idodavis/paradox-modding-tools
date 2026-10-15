<script setup lang="ts">
/**
 * A file's entry drawn on the zoomable floor between its toolbar (what is drawn, and how) and its status bar (how the
 * render went, what is missing or hidden): the picture the browser lays out (picture.ts), each widget's rect read
 * back from it for the marks over it (WidgetMarks.vue), the selection by click, caret or keys, and the widget's menu.
 */
import { computed, nextTick, onScopeDispose, shallowRef, useTemplateRef, watch } from "vue";
import { useEventListener } from "@vueuse/core";
import { useQuery } from "@pinia/colada";
import { Clipboard } from "@wailsio/runtime";
import type { ContextMenuItem, SelectMenuItem } from "@nuxt/ui";
import { WidgetRole } from "@services/internal/game";
import { Definition } from "@services/ideservice";
import { pathKey } from "../composables/useIdeRoots";
import { symbolsQuery } from "../queries";
import { RESOLUTIONS, useDraw, useDrawStore } from "../stores/draw";
import { useEditorStore } from "../stores/editor";
import WidgetMarks from "./WidgetMarks.vue";
import { Picture, quote } from "./picture";
import Viewport from "./Viewport.vue";
import { choicesOf, hiddenOf, pageGroups, sideGroups, stateOf, typeIcon } from "./widgets";

const { workspaceId, path } = defineProps<{ workspaceId: string; path: string }>();

/** Each database whose entries the picker offers: its heading, icon and place among the groups. */
const ENTRY_GROUPS: Record<string, { label: string; icon: string; order: number }> = {
  "gui.widget": { label: "Windows", icon: "i-lucide-app-window", order: 0 },
  "gui.type": { label: "Types", icon: "i-lucide-shapes", order: 1 },
  "gui.local_type": { label: "Types", icon: "i-lucide-shapes", order: 1 },
  "gui.template": { label: "Templates", icon: "i-lucide-layers", order: 2 },
  "gui.local_template": { label: "Templates", icon: "i-lucide-layers", order: 2 },
  "gui.texticon": { label: "Text icons", icon: "i-lucide-image", order: 3 },
  "gui.format": { label: "Formats", icon: "i-lucide-type", order: 4 },
};

/** The roles whose element is its widget's to the pointer: a skin, and the label a button's text is drawn in. */
const PARTS_OF_WIDGET = new Set<string>([WidgetRole.RoleSkin, WidgetRole.RoleLabel]);

const store = useDrawStore();
const editor = useEditorStore();
const { file, list, elements, drawing, renderedMs, error, refetch } = useDraw(
  () => workspaceId,
  () => path,
);
const viewport = useTemplateRef("viewport");
const scene = useTemplateRef("scene");
const root = useTemplateRef("root");
const zoom = computed(() => viewport.value?.zoom ?? 1);
const selection = computed({ get: () => file.value.selection, set: (one: number) => (file.value.selection = one) });
const hover = computed({ get: () => file.value.hover, set: (one: number) => (file.value.hover = one) });

// the entries the file holds, the ids it defines in the picker's databases; it opens on the one the caret is in, else
// its first window, else its first entry, and keeps the one picked while the file holds it
const { data: symbols } = useQuery(() => symbolsQuery({ id: workspaceId, path }));
const entrySymbols = computed(() => (symbols.value ?? []).filter((one) => one.database in ENTRY_GROUPS));
const entry = computed(() => file.value.entry);
/** An entry as the picker keys it: its database and its id. */
const entryKey = ({ database, name }: { database: string; name: string }) => `${database}|${name}`;
watch(
  entrySymbols,
  (entries) => {
    if (!entries.length || entries.some((one) => entryKey(one) === entryKey({ ...file.value, name: entry.value })))
      return;
    const caret = editor.viewOf(path) === "split" && pathKey(editor.activeFile) === pathKey(path) ? editor.caret : null;
    const caretEntry = caret
      ? entries.findLast(({ location: { range } }) => range.start.line <= caret.line)
      : undefined;
    const first = caretEntry ?? entries.find((one) => one.database === "gui.widget") ?? entries[0];
    Object.assign(file.value, { entry: first.name, database: first.database });
  },
  { immediate: true },
);

// the fonts the list's texts use, registered with the page while a list names them; the browser lays the picture
// out again as each loads
const faces = new Map<string, FontFace>();
watch(
  () => list.value?.fonts,
  (fonts) => {
    const listFonts = new Map((fonts ?? []).map((font) => [`${font.name}|${font.style}|${font.url}`, font]));
    for (const [key, face] of faces) {
      if (listFonts.has(key)) continue;
      document.fonts.delete(face);
      faces.delete(key);
    }
    for (const [key, font] of listFonts) {
      if (faces.has(key)) continue;
      const face = new FontFace(font.name, `url(${quote(font.url)})`, {
        weight: font.style === "bold" ? "700" : "400",
        style: font.style === "italic" ? "italic" : "normal",
      });
      faces.set(key, face);
      document.fonts.add(face);
      void face.load().catch(() => undefined);
    }
  },
  { immediate: true },
);
onScopeDispose(() => faces.forEach((face) => document.fonts.delete(face)));

// the widgets hidden, each by itself: by the session, and by the state each group of conditions is drawn at
const hidden = computed(() => (list.value ? hiddenOf(list.value, file.value) : new Set<number>()));
// the screen the entry is laid out on, in the game's pixels: the resolution
const screen = computed<[number, number]>(() => [store.width, store.height]);

// a new list or screen is drawn with the column pages after the first closed; once measured, the groups that show
// side widgets only are closed too
let isSidesPending = false;
watch(
  [list, screen],
  ([next]) => {
    if (!next) return;
    file.value.closedGroups = pageGroups(next);
    isSidesPending = true;
  },
  { immediate: true },
);

/** Reads every drawn widget's rect from the picture, in the picture's pixels, one per drawn copy. */
function measureRects(): void {
  const origin = root.value?.getBoundingClientRect();
  if (!origin || !list.value) return;
  const scale = zoom.value;
  const rects = new Map<number, DOMRect[]>();
  for (const node of root.value?.querySelectorAll<HTMLElement>("[data-element]") ?? []) {
    const index = Number(node.dataset.element);
    const drawn = node.getBoundingClientRect();
    const rect = new DOMRect(
      (drawn.left - origin.left) / scale,
      (drawn.top - origin.top) / scale,
      drawn.width / scale,
      drawn.height / scale,
    );
    rects.set(index, [...(rects.get(index) ?? []), rect]);
  }
  file.value.rects = rects;
  if (!isSidesPending) return;
  isSidesPending = false;
  const closed = pageGroups(list.value).union(sideGroups(list.value, rects));
  if (closed.size !== file.value.closedGroups.size) file.value.closedGroups = closed;
}
watch([list, hidden, screen, () => store.rows], () => void nextTick(measureRects), { flush: "post" });
useEventListener(document.fonts, "loadingdone", measureRects);

/** An element's first rect; undefined for none drawn. */
const rectOf = (element: number): DOMRect | undefined => file.value.rects.get(element)?.[0];

// an entry drawn alone is the extent of everything it draws, moved so what hangs above or left of it is on the
// scene; a window is drawn on its screen
const extent = computed<DOMRect>(() => {
  const rects = [...file.value.rects.values()].flat().filter((rect) => rect.width && rect.height);
  if (!list.value?.isAlone || !rects.length) return new DOMRect(0, 0, 0, 0);
  const left = Math.min(...rects.map((rect) => rect.left));
  const top = Math.min(...rects.map((rect) => rect.top));
  const right = Math.max(...rects.map((rect) => rect.right));
  const bottom = Math.max(...rects.map((rect) => rect.bottom));
  return new DOMRect(left, top, right - left, bottom - top);
});
// an entry drawn alone that draws nothing: no size, texture, text or child of its own
const isEmpty = computed(() => !!list.value?.isAlone && file.value.rects.size > 0 && !extent.value.width);
const sceneWidth = computed(() => (list.value?.isAlone ? extent.value.width : store.width));
const sceneHeight = computed(() => (list.value?.isAlone ? extent.value.height : store.height));

/** A rect of the picture on the scene, which the camera fits. */
const sceneRect = (rect: DOMRect) =>
  new DOMRect(rect.x - extent.value.x, rect.y - extent.value.y, rect.width, rect.height);
// what Fit shows: a window's rect, else the whole scene
const windowRect = computed(() => {
  const rect = rectOf(0);
  return list.value?.isAlone || !rect ? null : sceneRect(rect);
});

/** A rect of the picture where it is on the page. */
function pageRect(rect: DOMRect): DOMRect {
  const origin = root.value?.getBoundingClientRect() ?? new DOMRect();
  const scale = zoom.value;
  return new DOMRect(
    origin.left + rect.x * scale,
    origin.top + rect.y * scale,
    rect.width * scale,
    rect.height * scale,
  );
}

/** The widgets drawn under the pointer, topmost first, each once: the click, the pointer and the menu read it. */
function hitsUnder(event: MouseEvent): number[] {
  const hits = new Set<number>();
  for (const node of document.elementsFromPoint(event.clientX, event.clientY)) {
    const index = Number((node as HTMLElement).dataset.element ?? -1);
    const part = elements.value[index];
    if (part) hits.add(PARTS_OF_WIDGET.has(part.role) ? part.parent : index);
  }
  return [...hits];
}

/** The widget a click selects among those hit: the smallest, the topmost of equal ones; -1 for none. */
function target(hits: number[]): number {
  const area = (element: number) => {
    const rect = rectOf(element);
    return rect ? rect.width * rect.height : Infinity;
  };
  return hits.reduce((smallest, one) => (area(one) < area(smallest) ? one : smallest), hits[0] ?? -1);
}

// the entries as the picker groups them, each with its icon and the line it starts on
const items = computed<SelectMenuItem[][]>(() => {
  const entries = [...Map.groupBy(entrySymbols.value, entryKey).values()].map((same) => same[0]);
  const groups = Map.groupBy(entries, (symbol) => ENTRY_GROUPS[symbol.database].order);
  return [...groups]
    .sort(([a], [b]) => a - b)
    .map(([, group]) => [
      { type: "label" as const, label: ENTRY_GROUPS[group[0].database].label },
      ...group.map((symbol) => ({
        label: symbol.name,
        value: entryKey(symbol),
        icon: ENTRY_GROUPS[symbol.database].icon,
        line: symbol.location.range.start.line + 1,
      })),
    ]);
});
/** A picker row as an object: the rows are, but the menu's type allows a bare value. */
const rowOf = (item: SelectMenuItem) => (typeof item === "object" && item ? item : { label: String(item) });
const entryIcon = computed(() => ENTRY_GROUPS[file.value.database]?.icon);
// the status bar's lists, a row selecting its widget: what the game's files did not give, and what the session hides
const lists = computed(() => {
  const missing = list.value?.missing ?? [];
  const hiddenRows = [...file.value.hidden].sort((a, b) => a - b);
  return [
    {
      label: `${missing.length} missing`,
      icon: "i-lucide-triangle-alert",
      color: "warning" as const,
      rows: missing.map(({ text, element }) => ({
        element,
        text,
        detail: elements.value[element]?.label ?? "",
        hasEye: false,
      })),
    },
    {
      label: `${hiddenRows.length} hidden`,
      icon: "i-lucide-eye-off",
      color: "neutral" as const,
      rows: hiddenRows.map((element) => ({
        element,
        text: elements.value[element]?.label ?? "",
        detail: "",
        hasEye: true,
      })),
    },
  ].filter(({ rows }) => rows.length);
});
// what the floor says with no picture on it: that the entry draws nothing, or that the file holds none
const notice = computed(() => {
  if (isEmpty.value)
    return {
      icon: "i-lucide-square-dashed",
      title: `Nothing to draw: ${entry.value}`,
      description: "It has no size, texture, text or children of its own.",
    };
  if (!entry.value)
    return { icon: "i-lucide-app-window", title: "No window", description: "This file holds nothing to draw." };
  return null;
});

// the window's groups of conditions as the States list shows them: the call, the value drawn, and its choices
const states = computed(() =>
  (list.value?.groups ?? []).map((group) => ({
    group: group.call,
    value: stateOf(file.value, group),
    items: choicesOf(group),
  })),
);

/** Draws another entry of the file, its picker key, with no element selected and nothing hidden. */
function pickEntry(key: string): void {
  const [database, id] = [key.slice(0, key.indexOf("|")), key.slice(key.indexOf("|") + 1)];
  Object.assign(file.value, {
    entry: id,
    database,
    hidden: new Set<number>(),
    states: new Map<string, string>(),
    selection: -1,
  });
}

/** Selects the element clicked, taking the keys; with the code beside it, the caret goes to where the element is
 * written in this file. */
function pick(event: MouseEvent): void {
  scene.value?.focus({ preventScroll: true });
  selection.value = target(hitsUnder(event));
  const location = elements.value[selection.value]?.location;
  if (location && editor.viewOf(path) === "split" && pathKey(location.path) === pathKey(path))
    editor.open(path, { line: location.range.start.line, col: location.range.start.character });
}

// an element selected elsewhere (the panel, the code) comes into view
watch(selection, async (element) => {
  await nextTick();
  const rect = rectOf(element);
  if (rect) viewport.value?.reveal(pageRect(rect));
});

/** Selects nothing, as Escape or a click on the floor around the picture does, and gives the picture the keys. */
function deselect(): void {
  selection.value = -1;
  scene.value?.focus({ preventScroll: true });
}

/** Moves the selection to its parent, its first child, or its sibling before or after, where there is one. */
function move(way: "parent" | "child" | "before" | "after"): void {
  const index = selection.value;
  const element = elements.value[index];
  if (!element) return;
  const siblings = elements.value.flatMap((one, i) => (one.parent === element.parent ? [i] : []));
  const next = {
    parent: element.parent,
    child: elements.value.findIndex((one) => one.parent === index),
    before: siblings[siblings.indexOf(index) - 1] ?? -1,
    after: siblings[siblings.indexOf(index) + 1] ?? -1,
  }[way];
  if (next >= 0) selection.value = next;
}

// the keys while the picture has them: each key's action
const KEYS: Record<string, () => void> = {
  Escape: deselect,
  ArrowUp: () => move("parent"),
  ArrowDown: () => move("child"),
  ArrowLeft: () => move("before"),
  ArrowRight: () => move("after"),
  h: () => {
    if (selection.value >= 0) store.toggleHidden(path, [selection.value]);
  },
  H: () => (file.value.hidden = new Set()),
  Enter: () => goToElement(selection.value),
  f: () => {
    const rect = rectOf(selection.value);
    if (rect) viewport.value?.fitTo(sceneRect(rect));
  },
};
function onKey(event: KeyboardEvent): void {
  const action = event.ctrlKey || event.altKey || event.metaKey ? undefined : KEYS[event.key];
  if (!action) return;
  event.preventDefault();
  action();
}

// the widgets drawn where the menu was opened, which it lists so one under another can be selected
const menuHits = shallowRef<number[]>([]);

/** Selects the element right-clicked, whose menu opens with every widget drawn there. */
function openMenu(event: MouseEvent): void {
  const hits = hitsUnder(event);
  menuHits.value = hits;
  if (hits.length) selection.value = target(hits);
}
/** Puts the caret where an element is written. */
function goToElement(index: number): void {
  const element = elements.value[index];
  if (element) store.goTo(path, element.location);
}

/** Opens the type an element is made from, where it is defined. */
async function openType(index: number): Promise<void> {
  const location = elements.value[index]?.location;
  if (!location) return;
  store.goTo(
    path,
    (await Definition(workspaceId, location.path, location.range.start.line, location.range.start.character))?.[0],
  );
}
// the selected element's menu, under the widgets drawn where it opened when there are several, the selected one marked
const menu = computed<ContextMenuItem[][]>(() => {
  const index = selection.value;
  const element = elements.value[index];
  const hitItems: ContextMenuItem[][] =
    menuHits.value.length > 1
      ? [
          [
            { type: "label", label: "Widgets here" },
            ...menuHits.value.map((one) => ({
              label: elements.value[one]?.label ?? "",
              icon: typeIcon(elements.value[one]?.type ?? ""),
              color: one === index ? ("primary" as const) : undefined,
              onSelect: () => (selection.value = one),
            })),
          ],
        ]
      : [];
  if (!element) return hitItems;
  const parent = elements.value[element.parent];
  return [
    ...hitItems,
    [
      {
        label: parent ? `Select the parent, ${parent.label}` : "Select the parent",
        icon: "i-lucide-corner-left-up",
        kbds: ["arrowup"],
        disabled: !parent,
        onSelect: () => (selection.value = element.parent),
      },
      { label: "Hide", icon: "i-lucide-eye-off", kbds: ["H"], onSelect: () => store.toggleHidden(path, [index]) },
      {
        label: "Show everything hidden",
        icon: "i-lucide-eye",
        kbds: ["shift", "H"],
        disabled: !file.value.hidden.size,
        onSelect: () => (file.value.hidden = new Set()),
      },
    ],
    [
      { label: "Go to code", icon: "i-lucide-code", kbds: ["enter"], onSelect: () => goToElement(index) },
      ...(element.key !== element.type
        ? [{ label: `Open its type, ${element.key}`, icon: "i-lucide-layers", onSelect: () => void openType(index) }]
        : []),
    ],
    [
      {
        label: "Copy its name",
        icon: "i-lucide-copy",
        onSelect: () => void Clipboard.SetText(element.name || element.key),
      },
      { label: "Copy its path", icon: "i-lucide-copy", onSelect: () => void Clipboard.SetText(element.path) },
    ],
  ];
});

// how many widgets are drawn under the pointer: the menu opens wherever there is one
const hitCount = shallowRef(0);

/** Follows the pointer over the elements: the one a click there would select, which the marks outline. */
function track(event: PointerEvent): void {
  const hits = hitsUnder(event);
  hitCount.value = hits.length;
  hover.value = target(hits);
}
// a pane that closes forgets the element under the pointer, which its file's next picture would outline
onScopeDispose(() => (file.value.hover = -1));
</script>

<template>
  <div class="flex min-h-0 min-w-0 flex-1 flex-col">
    <div class="flex h-10 shrink-0 items-center gap-1.5 overflow-x-auto border-b border-default bg-muted px-2">
      <!-- the entry's full id is its tooltip, on the picker and on each row; the menu is as wide as its ids -->
      <span :title="entry" class="contents">
        <USelectMenu
          :model-value="entryKey({ database: file.database, name: entry })"
          :items="items"
          value-key="value"
          size="sm"
          :icon="entryIcon"
          class="w-60 shrink-0"
          :search-input="{ placeholder: 'Find an entry of this file' }"
          :ui="{ content: 'w-max min-w-(--reka-combobox-trigger-width) max-w-xl', value: 'font-mono' }"
          placeholder="No window"
          @update:model-value="(key) => pickEntry(String(key))"
        >
          <template #item-label="{ item }">
            <span :title="rowOf(item).label" class="font-mono">{{ rowOf(item).label }}</span>
          </template>
          <template #item-trailing="{ item }">
            <span v-if="rowOf(item).line" class="font-mono text-11 text-dimmed">L{{ rowOf(item).line }}</span>
          </template>
        </USelectMenu>
      </span>
      <USelect v-model="store.width" :items="RESOLUTIONS" icon="i-lucide-monitor" size="sm" class="w-36 shrink-0" />
      <USeparator orientation="vertical" class="mx-1 h-4" />
      <UTooltip text="Outline every widget's bounds">
        <UButton
          icon="i-lucide-square-dashed"
          square
          size="sm"
          :color="store.bounds ? 'primary' : 'neutral'"
          :variant="store.bounds ? 'soft' : 'ghost'"
          aria-label="Bounds"
          :aria-pressed="store.bounds"
          @click="store.bounds = !store.bounds"
        />
      </UTooltip>
      <!-- the window's conditions: each group drawn at one of its values, its first unless another is chosen -->
      <UPopover v-if="states.length" :content="{ side: 'bottom', align: 'start' }">
        <UButton
          :label="`States ${states.length}`"
          icon="i-lucide-braces"
          trailing-icon="i-lucide-chevron-down"
          color="neutral"
          variant="ghost"
          size="sm"
          class="shrink-0"
        />
        <template #content>
          <div class="flex max-h-96 w-120 flex-col gap-1 overflow-auto p-2 text-12">
            <p class="px-1 pb-1 text-dimmed">
              Widgets shown on the same call are one group; each group is drawn at one value, its first unless you pick
              another.
            </p>
            <div v-for="row in states" :key="row.group" class="flex items-center gap-2 rounded px-1 hover:bg-elevated">
              <span class="min-w-0 grow truncate font-mono text-(--syn-ref)" :title="row.group">{{ row.group }}</span>
              <USelect
                :model-value="row.value"
                :items="row.items"
                size="xs"
                class="w-40 shrink-0"
                @update:model-value="(value) => store.setState(path, row.group, String(value))"
              />
            </div>
          </div>
        </template>
      </UPopover>
    </div>
    <div class="relative flex min-h-0 flex-1 flex-col">
      <Viewport ref="viewport" :width="sceneWidth" :height="sceneHeight" :focus="windowRect" @floor="deselect">
        <UContextMenu :items="menu" :disabled="!hitCount" :ui="{ content: 'w-80' }">
          <div
            ref="scene"
            tabindex="-1"
            class="relative size-full bg-default outline-none"
            @click="pick"
            @contextmenu="openMenu"
            @keydown="onKey"
            @pointermove="track"
            @pointerleave="
              hitCount = 0;
              hover = -1;
            "
          >
            <!-- the picture, and the marks over it, in the game's pixels; Bounds outlines every widget by a rule -->
            <div
              ref="root"
              class="absolute top-0 left-0"
              :class="{ 'pmt-bounds': store.bounds }"
              :style="{
                width: `${sceneWidth}px`,
                height: `${sceneHeight}px`,
                translate: `${-extent.x}px ${-extent.y}px`,
                '--zoom': zoom,
              }"
            >
              <Picture
                v-if="list"
                :list="list"
                :hidden="hidden"
                :rows="store.rows"
                :width="screen[0]"
                :height="screen[1]"
              />
              <WidgetMarks
                :file="file"
                :elements="elements"
                :missing="list?.missing ?? []"
                :screen="
                  list && !list.isAlone
                    ? { width: screen[0], height: screen[1], label: `${store.width} × ${store.height}` }
                    : null
                "
                :zoom="zoom"
                :host="root"
              />
            </div>
          </div>
        </UContextMenu>
        <template #toolbar>
          <span v-if="drawing" class="flex items-center gap-1.5 px-1">
            <UIcon name="i-lucide-loader-circle" class="size-3.5 animate-spin" />Drawing
          </span>
          <span v-else-if="list" class="flex items-center gap-1.5 px-1">
            <UIcon name="i-lucide-circle-check" class="size-3.5 text-success" />Rendered in {{ renderedMs }} ms
          </span>
          <!-- what the game's files did not give, and what the session hides: a row selects its widget -->
          <UPopover v-for="one in lists" :key="one.icon" :content="{ side: 'top', align: 'start' }">
            <UButton
              :label="one.label"
              :icon="one.icon"
              trailing-icon="i-lucide-chevron-up"
              :color="one.color"
              variant="ghost"
              size="xs"
            />
            <template #content>
              <ul class="flex max-h-80 w-100 flex-col overflow-auto p-1 text-12">
                <li
                  v-for="row in one.rows"
                  :key="`${row.text}|${row.element}`"
                  class="flex items-center rounded hover:bg-elevated"
                >
                  <button
                    type="button"
                    class="flex min-w-0 grow flex-col px-1.5 py-1 text-start"
                    :disabled="row.element < 0"
                    :title="elements[row.element]?.path"
                    @click="selection = row.element"
                  >
                    <span class="truncate text-toned" :class="row.hasEye && 'font-mono'">{{ row.text }}</span>
                    <span v-if="row.detail" class="truncate font-mono text-11 text-dimmed">{{ row.detail }}</span>
                  </button>
                  <UButton
                    v-if="row.hasEye"
                    icon="i-lucide-eye"
                    color="neutral"
                    variant="ghost"
                    size="xs"
                    square
                    aria-label="Show"
                    @click="store.toggleHidden(path, [row.element])"
                  />
                </li>
              </ul>
            </template>
          </UPopover>
        </template>
      </Viewport>
      <div
        v-if="!list || isEmpty"
        class="pointer-events-none absolute inset-x-0 top-0 bottom-7 flex items-center justify-center p-6"
      >
        <UAlert
          v-if="error && !list"
          class="pointer-events-auto max-w-120"
          color="error"
          variant="subtle"
          icon="i-lucide-circle-x"
          :title="`${entry} could not be drawn`"
          :description="error.message"
          :actions="[
            {
              label: 'Open in code',
              icon: 'i-lucide-code',
              color: 'neutral',
              variant: 'outline',
              onClick: () =>
                store.goTo(
                  path,
                  entrySymbols.find((one) => entryKey(one) === entryKey({ ...file, name: entry }))?.location,
                ),
            },
            {
              label: 'Try again',
              icon: 'i-lucide-rotate-cw',
              color: 'neutral',
              variant: 'ghost',
              onClick: () => void refetch(),
            },
          ]"
        />
        <UEmpty v-else-if="notice" v-bind="notice" size="sm" />
        <span v-else class="flex items-center gap-2 text-12 text-dimmed">
          <UIcon name="i-lucide-loader-circle" class="size-4 animate-spin" />Drawing
        </span>
      </div>
    </div>
  </div>
</template>

<style>
/* every widget's bounds, a dashed line one screen pixel wide at any zoom */
.pmt-bounds [data-element] {
  outline: calc(1px / var(--zoom)) dashed color-mix(in srgb, var(--link) 45%, transparent);
  outline-offset: calc(-1px / var(--zoom));
}
/* a texture the game supplies through a binding: an image glyph in the link color in the middle of its box, 16 pixels
   in a small box, a quarter of a large one, at most 48 */
.pmt-placeholder::after {
  content: "";
  width: min(100%, 48px, max(16px, 25%));
  height: min(100%, 48px, max(16px, 25%));
  background: color-mix(in srgb, var(--link) 80%, transparent);
  --glyph: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='black' stroke-width='2' stroke-linecap='round' stroke-linejoin='round'%3E%3Crect width='18' height='18' x='3' y='3' rx='2'/%3E%3Ccircle cx='9' cy='9' r='2'/%3E%3Cpath d='m21 15-3.086-3.086a2 2 0 0 0-2.828 0L6 21'/%3E%3C/svg%3E");
  mask: var(--glyph) center / contain no-repeat;
}
</style>
