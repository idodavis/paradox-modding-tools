<!--
  The Visual view's pane: the picture on the viewport, the marks over it, and the panel, with the code beside it in
  Split. A coat's instance dragged on the picture moves there.
-->
<script setup lang="ts">
import type { ContextMenuItem, SplitterItem } from "@nuxt/ui";
import { useMutation, useQuery } from "@pinia/colada";
import { refDebounced, useDebounceFn, useEventListener, useLocalStorage, useResizeObserver } from "@vueuse/core";
import { Clipboard } from "@wailsio/runtime";
import { Range } from "monaco-editor";
import type { IPosition } from "monaco-editor";
import { storeToRefs } from "pinia";
import { computed, nextTick, shallowRef, useTemplateRef, watch, watchEffect, watchPostEffect } from "vue";

import { Format } from "@internal/files/models";
import * as draw from "@services/draw/service";
import * as lsp from "@services/lsp/service";
import type { Instance, Widget, Window } from "@services/draw/models";
import type { Symbol } from "@services/lsp/models";
import type { Place } from "@services/workspace/models";

import {
  coatQuery,
  drawablesQuery,
  fileUrl,
  framesQuery,
  settingsCatalogQuery,
  stateQuery,
  statesQuery,
  windowQuery,
} from "../../../queries";
import { useDrawStore } from "../../../stores/draw";
import { useSettingsStore } from "../../../stores/settings";
import { useWorkspaceStore } from "../../../stores/workspace";
import CodeEditor from "../CodeEditor.vue";
import { applyEdits } from "../editor/models";
import type { Model } from "../editor/models";
import CodeLink from "./CodeLink.vue";
import DrawDetail from "./DrawDetail.vue";
import PictureViewport, { DRAG } from "./PictureViewport.vue";
import WidgetMarks from "./WidgetMarks.vue";
import { drawCoat, findBoxes, isOwn } from "./coat";
import { buildPicture, fitPlaceholders, fitTexts, listProperties, placeOnScreen, readRects } from "./picture";
import type { PictureNode } from "./picture";

const { model, split } = defineProps<{ model: Model; split: boolean }>();

/**
 * An instance being dragged across the coat: its layer, its place among the layer's instances, the key its position is
 * written by, where the press began and how far it has moved, in the picture's pixels. Once dropped, it is kept until
 * the coat is drawn again with the instance where it was dropped.
 */
type Drag = {
  layer: number;
  copy: number;
  instance: Instance;
  key: string;
  from: DOMPoint;
  by: DOMPoint;
  isDropped: boolean;
};

const NO_WINDOW: Window = {
  name: "",
  widgets: [],
  holders: [],
  datacontext: "",
  places: [],
  fonts: [],
  rows: {},
  conditions: {},
  properties: [],
  overlays: [],
  groups: [],
  textFont: "",
  textSize: "",
};
// the keys an instance's position is written by: an emblem's position, a sub's offset
const POSITION_KEYS = ["position", "offset"];

const store = useDrawStore();
const settings = useSettingsStore();
const workspace = useWorkspaceStore();
const file = computed(() => store.file(model.place.path));
const isCoat = computed(() => model.place.format !== Format.FormatGUI);
const isBounded = shallowRef(false);
const hasGrid = useLocalStorage("pmt.draw.grid", false);
const { isPanelOpen } = storeToRefs(store);

const splitter = useTemplateRef("splitter");
const viewport = useTemplateRef("viewport");
const floor = useTemplateRef<HTMLElement>("floor");
const canvas = useTemplateRef<HTMLCanvasElement>("canvas");
const nodes = shallowRef<PictureNode[]>([]);
const windowRects = shallowRef(new Map<number, DOMRect[]>());
// the window lay wholly off the screen and is drawn at the screen's edge it passed
const isPlaced = shallowRef(false);
const renderedMs = shallowRef(0);
const drag = shallowRef<Drag | null>(null);

const { data: state } = useQuery(() => ({ ...stateQuery, enabled: workspace.isOpen() }));
const { data: catalog } = useQuery(() => ({ ...settingsCatalogQuery, enabled: workspace.isOpen() }));
const { data: frames } = useQuery(() => ({ ...framesQuery, enabled: workspace.isOpen() }));
const { data: drawables } = useQuery(() => ({
  ...drawablesQuery(model.place.path),
  enabled: workspace.isOpen(),
}));

const resolutionKey = computed(() => `draw.resolution.${state.value?.game?.id ?? ""}`);
const resolution = computed(() => settings.value(resolutionKey.value));
const resolutions = computed(() => catalog.value?.find(({ key }) => key === resolutionKey.value)?.choices ?? []);
const screen = computed(() => {
  const [width = 0, height = 0] = resolution.value.split("x").map(Number);
  return { width, height };
});

// the caret settles before the entry it is in is drawn, so moving through the file does not draw each entry passed
const caret = refDebounced(
  computed(() => ({ lineNumber: model.caret.line, column: model.caret.column })),
  300,
);
const drawable = computed(() => {
  const list = drawables.value ?? [];
  const shown = list.find(({ name }) => name === file.value.drawable);
  return (shown ?? findEntry(caret.value) ?? list[0])?.name ?? "";
});
watchEffect(() => {
  if (drawable.value && file.value.drawable !== drawable.value) store.draw(model.place.path, drawable.value);
});
// the entry the caret moves into is drawn, as is the entry at the caret once the file's entries are known
watch(
  [caret, () => !!drawables.value],
  () => {
    const entry = findEntry(caret.value);
    if (entry) store.draw(model.place.path, entry.name);
  },
  { immediate: true },
);
// a newly drawn entry is fitted, wherever the camera was left
watch(drawable, () => viewport.value?.fit());

const {
  data: drawnWindow,
  error: windowError,
  asyncStatus: windowStatus,
  refetch: refetchWindow,
} = useQuery(() => ({
  ...windowQuery({ path: model.place.path, name: drawable.value }),
  enabled: !isCoat.value && !!drawable.value && !!resolution.value,
}));
const {
  data: drawnCoat,
  error: coatError,
  asyncStatus: coatStatus,
  refetch: refetchCoat,
} = useQuery(() => ({
  ...coatQuery({ path: model.place.path, name: drawable.value, picks: store.picks }),
  enabled: isCoat.value && !!drawable.value && !!resolution.value,
}));
const { data: states } = useQuery(() => ({
  ...statesQuery({ path: model.place.path, name: drawnWindow.value?.name ?? "", picks: store.picks }),
  enabled: workspace.isOpen() && !!drawnWindow.value,
}));
const { mutate: writePosition } = useMutation({
  mutation: async ({ moving, text }: { moving: Drag; text: string }) =>
    applyEdits(model, (await draw.EditProperty(model.place.path, moving.instance.range, moving.key, text)) ?? []),
  onError: () => (drag.value = null),
});
const asyncStatus = computed(() => (isCoat.value ? coatStatus : windowStatus).value);
const error = computed(() => (isCoat.value ? coatError : windowError).value);
const refetch = () => (isCoat.value ? refetchCoat : refetchWindow)();

const hidden = computed(() => new Set([...file.value.hidden, ...(states.value?.hidden ?? [])]));
const boxes = computed(() =>
  drawnCoat.value ? moveBox(findBoxes(drawnCoat.value, screen.value.height)) : windowRects.value,
);
const rects = computed(() => {
  const first = new Map([...boxes.value].flatMap(([id, list]) => (list[0] ? [[id, list[0]] as const] : [])));
  const { selected, instance, hovered, hoveredInstance } = file.value;
  for (const [id, index] of [
    [hovered, hoveredInstance],
    [selected, instance],
  ] as const) {
    const box = id === null || index === null ? undefined : boxes.value.get(id)?.[index];
    if (id !== null && box) first.set(id, box);
  }
  return first;
});
const hasPicture = computed(() => !!(drawnWindow.value || drawnCoat.value));
const isEmpty = computed(() => !!drawnWindow.value && windowRects.value.size > 0 && !rects.value.get(0)?.width);
const markBoxes = computed(() =>
  drawnCoat.value ? new Map([...rects.value].map(([id, rect]) => [id, [rect]])) : boxes.value,
);
const blocks = computed(() => {
  const window = drawnWindow.value;
  if (window) {
    const places = window.places ?? [];
    return (window.widgets ?? []).map(({ id, parent, place, range }) => ({ id, parent, place: places[place], range }));
  }
  return drawnCoat.value?.layers?.map(({ place, range }, id) => ({ id, parent: -1, place, range })) ?? [];
});
const missing = computed(() =>
  (drawnWindow.value?.widgets ?? []).flatMap((widget) =>
    listProperties(drawnWindow.value ?? NO_WINDOW, widget.properties)
      .filter((property) => property.origin === "missing")
      .map((property) => ({
        id: widget.id,
        text: `${property.key} ${property.value}`,
        detail: widget.name || widget.type,
      })),
  ),
);
const lists = computed(() => [
  {
    label: "missing",
    icon: "i-lucide-triangle-alert",
    color: "warning" as const,
    isHidden: false,
    rows: missing.value,
  },
  {
    label: "hidden",
    icon: "i-lucide-eye-off",
    color: "neutral" as const,
    isHidden: true,
    rows: file.value.hidden.map((id) => {
      const widget = drawnWindow.value?.widgets?.[id];
      return {
        id,
        text: widget ? widget.name || widget.type : (drawnCoat.value?.layers?.[id]?.texture ?? ""),
        detail: "",
      };
    }),
  },
]);
// a frame's value is its label, and the field, which no frame is called, draws the coat unframed
const frameOptions = computed(() => [
  { label: "Field", value: "Field" },
  ...(frames.value ?? []).map(({ label, place }) => ({
    label: place.origin === "vanilla" ? label : `${label} · ${workspace.originName(place.origin)}`,
    value: label,
  })),
]);
const framePlace = computed(() => frames.value?.find(({ label }) => label === store.frame)?.place);
const mask = computed(() =>
  framePlace.value ? { maskImage: `url("${fileUrl(framePlace.value.path)}")`, maskSize: "100% 100%" } : {},
);
// the panes side by side: the code in Split, the picture, and the panel, which closes when dragged past its least size
const panes = computed<SplitterItem[]>(() =>
  split
    ? [
        { id: "code", slot: "code", defaultSize: 35, minSize: 15 },
        { id: "picture", slot: "picture", defaultSize: 40, minSize: 20 },
        { id: "panel", slot: "panel", defaultSize: 25, minSize: 15, collapsible: true, collapsedSize: 0 },
      ]
    : [
        { id: "picture", slot: "picture", defaultSize: 70, minSize: 30 },
        { id: "panel", slot: "panel", defaultSize: 30, minSize: 18, collapsible: true, collapsedSize: 0 },
      ],
);
// the switches over the picture: a window's widget bounds, and the grid over a window or a coat
const toggles = computed(() => [
  ...(isCoat.value
    ? []
    : [{ label: "Bounds", tooltip: "Outline every widget's bounds", icon: "i-lucide-square-dashed", on: isBounded }]),
  { label: "Grid", tooltip: "Draw a grid over the picture", icon: "i-lucide-grid-3x3", on: hasGrid },
]);
// the panel's one button, which opens it on a window's States or a coat's Layers, each with its count
const panelButton = computed(() =>
  isCoat.value
    ? {
        label: `Layers ${drawnCoat.value?.layers?.length ?? 0}`,
        tooltip: "The layers of the coat",
        icon: "i-lucide-layers",
        tab: "layers" as const,
      }
    : {
        label: `States ${Object.keys(states.value?.applies ?? {}).length}`,
        tooltip: "The states that change what is drawn",
        icon: "i-lucide-braces",
        tab: "states" as const,
      },
);
// a cell of the grid is ten of a window's pixels, or a tenth of a coat each way
const gridCell = computed(() => (isCoat.value ? "10%" : "10px"));
const caretKey = computed(() => {
  const caret = { lineNumber: model.caret.line, column: model.caret.column };
  const widget = drawnWindow.value?.widgets?.[file.value.selected ?? -1];
  const places = drawnWindow.value?.places ?? [];
  const property = listProperties(drawnWindow.value ?? NO_WINDOW, widget?.properties ?? []).findLast(
    ({ place, range }) => places[place]?.key === model.place.key && Range.containsPosition(range, caret),
  );
  return split ? (property?.key.toLowerCase() ?? "") : "";
});
const menu = computed<ContextMenuItem[][]>(() => {
  const widgets = drawnWindow.value?.widgets ?? [];
  const widget = widgets[file.value.selected ?? -1];
  const place = widget && drawnWindow.value?.places?.[widget.place];
  const path = model.place.path;
  if (!widget || !place) return [];
  const parent = widget.id ? widgets[widget.parent] : undefined;
  return [
    [
      {
        label: parent ? `Select the parent, ${parent.name || parent.type}` : "Select the parent",
        icon: "i-lucide-corner-left-up",
        kbds: ["arrowup"],
        disabled: !parent,
        onSelect: () => store.select(path, widget.parent),
      },
      { label: "Hide", icon: "i-lucide-eye-off", kbds: ["H"], onSelect: () => store.hide(path, widget.id, true) },
      {
        label: "Show everything hidden",
        icon: "i-lucide-eye",
        kbds: ["shift", "H"],
        disabled: !file.value.hidden.length,
        onSelect: showHidden,
      },
    ],
    [
      {
        label: "Go to code",
        icon: "i-lucide-code",
        kbds: ["enter"],
        onSelect: () => store.showCode(place, widget.range),
      },
      ...(widget.typeDefined
        ? [{ label: "Open its type", icon: "i-lucide-layers", onSelect: () => void openType(place, widget.range) }]
        : []),
    ],
    [
      {
        label: "Copy its name",
        icon: "i-lucide-copy",
        onSelect: () => void Clipboard.SetText(widget.name || widget.type),
      },
      { label: "Copy its path", icon: "i-lucide-copy", onSelect: () => void Clipboard.SetText(place.path) },
    ],
  ];
});

watchPostEffect(() => {
  const element = floor.value;
  // the picture waits for the states, so what they hide never shows first
  if (!element || !drawnWindow.value || !states.value) return;
  const started = performance.now();
  const drawing = { hidden: hidden.value, choices: states.value.choices, rows: Number(settings.value("draw.rows")) };
  nodes.value = buildPicture(drawnWindow.value, drawing, element);
  readPicture(element);
  renderedMs.value = Math.round(performance.now() - started);
});
const readAgain = useDebounceFn(() => {
  if (floor.value) readPicture(floor.value);
}, 50);
useEventListener(floor, "load", () => void readAgain(), { capture: true });
// a pane hidden while the app's scale changed lays out again once shown
useResizeObserver(floor, () => void readAgain());

// the camera's pane takes the pointer and the keys, so a click off the picture selects nothing
const pane = computed(() => viewport.value?.pane);
useEventListener(pane, "click", applyClick);
useEventListener(pane, "contextmenu", openMenu);
useEventListener(pane, "keydown", applyKey);
useEventListener(pane, "pointermove", applyPointer);
useEventListener(pane, "pointerleave", leave);
useEventListener(canvas, "pointerdown", startDrag);
useEventListener(canvas, "pointermove", moveDrag);
useEventListener(canvas, "pointerup", dropDrag);
useEventListener(canvas, "pointercancel", () => (drag.value = null));
// the coat drawn again ends a drag, a dropped instance now drawn where it was dropped
watch(drawnCoat, () => (drag.value = null));
useEventListener(
  () => document.fonts,
  "loadingdone",
  () => void readAgain(),
);

watchPostEffect(() => {
  const layers = drawnCoat.value?.layers?.filter((_, index) => !hidden.value.has(index)) ?? null;
  if (!canvas.value || !drawnCoat.value) return;
  const started = performance.now();
  void drawCoat(canvas.value, { ...drawnCoat.value, layers }, screen.value.height).then(() => {
    renderedMs.value = Math.round(performance.now() - started);
  });
});

// the panel is open or closed as the session chose, in a splitter made again for Split too, once its panes are laid out
watch([isPanelOpen, splitter], ([isOpen]) =>
  nextTick(() => {
    const panel = splitter.value?.panelsRef.at(-1);
    if (isOpen) panel?.expand();
    else panel?.collapse();
  }),
);

watch(
  () => file.value.selected,
  async (id) => {
    await nextTick();
    const rect = id === null ? undefined : rects.value.get(id);
    if (rect) viewport.value?.reveal(rect);
  },
);

/**
 * Reads the rects of the drawn widgets after layout. The texts that their boxes cut are first fitted to them, each
 * placeholder to the part of its box inside its holder, and a window that lies wholly off the screen is moved to the
 * screen's edge it passed.
 */
function readPicture(element: HTMLElement): void {
  fitTexts(nodes.value);
  fitPlaceholders(nodes.value);
  isPlaced.value = placeOnScreen(nodes.value, element);
  windowRects.value = readRects(nodes.value, element);
}

/** Finds how much the camera scales the picture. */
function findScale(): number {
  const element = isCoat.value ? canvas.value : floor.value;
  return element?.offsetWidth ? element.getBoundingClientRect().width / element.offsetWidth : 1;
}

/** Finds a point of the event in the pixels of the picture. */
function findPoint(event: MouseEvent): DOMPoint {
  const origin = (isCoat.value ? canvas.value : floor.value)?.getBoundingClientRect() ?? new DOMRect();
  const scale = findScale();
  return new DOMPoint((event.clientX - origin.left) / scale, (event.clientY - origin.top) / scale);
}

/** Finds the widgets or layers drawn under the pointer. A skin or a label stands for the widget it is part of. */
function findHits(event: MouseEvent): number[] {
  if (isCoat.value) {
    const point = findPoint(event);
    return [...boxes.value].filter(([, list]) => list.some((box) => contains(box, point))).map(([id]) => id);
  }
  const hits = new Set<number>();
  for (const element of document.elementsFromPoint(event.clientX, event.clientY)) {
    const id = element instanceof HTMLElement && floor.value?.contains(element) ? element.dataset.element : undefined;
    const widget = id === undefined ? undefined : drawnWindow.value?.widgets?.[Number(id)];
    if (widget) hits.add(widget.role === "skin" || widget.role === "label" ? widget.parent : widget.id);
  }
  return [...hits];
}

/** Whether a box holds a point. */
const contains = (box: DOMRect, point: DOMPoint) =>
  point.x >= box.left && point.x <= box.right && point.y >= box.top && point.y <= box.bottom;

/**
 * Finds the copy of a widget in a list's rows, or the instance of a layer, that the pointer is on: its place among the
 * boxes it covers. It is null for none.
 */
function findCopy(id: number | null, event: MouseEvent): number | null {
  const point = findPoint(event);
  const copy = id === null ? -1 : (boxes.value.get(id)?.findIndex((box) => contains(box, point)) ?? -1);
  return copy < 0 ? null : copy;
}

/** Sorts the hits at a point in the order clicks there go through them: smallest first, the topmost of equal ones. */
function sortHits(hits: number[]): number[] {
  const area = (id: number) => Math.min(...(boxes.value.get(id) ?? []).map((box) => box.width * box.height));
  return hits.toSorted((first, second) => area(first) - area(second));
}

/**
 * Finds the one hit that a click at a point selects: the one after the selected one in the order clicks go through
 * them, else the first, so clicks on one spot reach every widget drawn there. It is null for none.
 */
function findTarget(hits: number[]): number | null {
  const order = sortHits(hits);
  return order.length ? order[(order.indexOf(file.value.selected ?? -1) + 1) % order.length] : null;
}

/** Opens the definition of the type of the widget written in a file at the given range. */
async function openType(place: Place, range: Widget["range"]): Promise<void> {
  const [type] = (await lsp.Definition(place.path, range.startLineNumber, range.startColumn)) ?? [];
  if (type) store.showCode(type.place, type.range);
}

/** Makes a PNG of the coat as drawn, cut to the frame where one is chosen, its transparent pixels kept. */
async function snapshotCoat(): Promise<Blob | null> {
  const source = canvas.value;
  const sheet = document.createElement("canvas");
  const context = sheet.getContext("2d");
  if (!source || !context) return null;
  Object.assign(sheet, { width: source.width, height: source.height });
  context.drawImage(source, 0, 0);
  if (framePlace.value) {
    const frame = new Image();
    frame.src = fileUrl(framePlace.value.path);
    await frame.decode();
    context.globalCompositeOperation = "destination-in";
    context.drawImage(frame, 0, 0, sheet.width, sheet.height);
  }
  return new Promise((resolve) => sheet.toBlob(resolve, "image/png"));
}

/** Finds the entry of the file written at a position, or undefined where none is. */
function findEntry(position: IPosition): Symbol | undefined {
  return drawables.value?.findLast(({ range }) => Range.containsPosition(range, position));
}

/** Opens the code of the drawn drawable. */
function openDrawable(): void {
  const found = drawables.value?.find(({ name }) => name === drawable.value);
  if (found) store.showCode(model.place, found.range);
}

/** Shows every widget the session hides. */
function showHidden(): void {
  file.value.hidden.forEach((id) => store.hide(model.place.path, id, false));
}

/**
 * Selects what is clicked, the next widget under the pointer with each click on one spot, or nothing for a click off
 * the picture. A widget or a layer selected opens a closed panel on its Properties tab, which is Layers for a coat.
 */
function applyClick(event: MouseEvent): void {
  if (event.currentTarget instanceof HTMLElement) event.currentTarget.focus({ preventScroll: true });
  // the click that ends a drag selects nothing more
  if (drag.value) return;
  const target = findTarget(findHits(event));
  store.select(model.place.path, target, findCopy(target, event));
  if (target === null || isPanelOpen.value) return;
  file.value.tab = "properties";
  isPanelOpen.value = true;
}

/** Opens the panel on a window's States or a coat's Layers, or closes it when it is open on any tab. */
function togglePanel(): void {
  if (!isPanelOpen.value) file.value.tab = panelButton.value.tab;
  isPanelOpen.value = !isPanelOpen.value;
}

/**
 * Keeps what the user does to the panel's edge, dragged or moved by its keys: the panel at no size is closed. A pane's
 * first size, read from the splitter's saved layout, is left alone, since the session decides whether the panel opens.
 */
function followPanel(pane: number, size: number, previous: number | undefined): void {
  if (panes.value[pane]?.id === "panel" && previous !== undefined) isPanelOpen.value = size > 0;
}

/**
 * Selects what is right-clicked, whose menu then opens: the selected widget where it is under the pointer, else the
 * first.
 */
function openMenu(event: MouseEvent): void {
  const hits = sortHits(findHits(event));
  const target = hits.find((id) => id === file.value.selected) ?? hits.at(0);
  if (target !== undefined) store.select(model.place.path, target, findCopy(target, event));
}

/** Follows the pointer: it hovers what a click there would select. */
function applyPointer(event: PointerEvent): void {
  const target = findTarget(findHits(event));
  store.hover(model.place.path, target, findCopy(target, event));
}

/** Leaves the picture: nothing is under the pointer. */
function leave(): void {
  store.hover(model.place.path, null);
}

/**
 * Starts a drag of the coat's instance under a press, where this file writes it: the selected layer's where it is
 * under the pointer, else the one a click there would select. The camera then does not pan. A default instance, which
 * no block writes, is not dragged.
 */
function startDrag(event: PointerEvent): void {
  const coat = drawnCoat.value;
  const hits = sortHits(findHits(event));
  const layer = hits.find((id) => id === file.value.selected) ?? findTarget(hits);
  const copy = findCopy(layer, event);
  const found = layer === null ? undefined : coat?.layers?.[layer];
  const instance = copy === null ? undefined : found?.instances?.[copy];
  const key = POSITION_KEYS.find((one) => found?.inputs?.[one]);
  if (event.button !== 0 || layer === null || copy === null || !coat || !found || !instance || !key) return;
  if (!instance.range.startLineNumber || !isOwn(coat, found, model.place.key)) return;
  event.stopPropagation();
  canvas.value?.setPointerCapture(event.pointerId);
  drag.value = { layer, copy, instance, key, from: findPoint(event), by: new DOMPoint(), isDropped: false };
}

/** Moves the dragged instance's box with the pointer. Once the press is a drag, the instance is selected. */
function moveDrag(event: PointerEvent): void {
  const moving = drag.value;
  if (!moving || moving.isDropped) return;
  const point = findPoint(event);
  drag.value = { ...moving, by: new DOMPoint(point.x - moving.from.x, point.y - moving.from.y) };
  if (isDragged(drag.value)) store.select(model.place.path, moving.layer, moving.copy);
}

/**
 * Ends a press on the coat. After a drag, the instance's new position is written in the file as one edit; a press
 * that never became a drag is a click.
 */
function dropDrag(): void {
  const moving = drag.value;
  if (!moving || moving.isDropped) return;
  if (!isDragged(moving) || !canvas.value) {
    drag.value = null;
    return;
  }
  drag.value = { ...moving, isDropped: true };
  const [across, down] = moving.instance.position;
  const position = [across + moving.by.x / canvas.value.width, down + moving.by.y / canvas.value.height];
  writePosition({ moving, text: `{ ${position.map((part) => Number(part.toFixed(3))).join(" ")} }` });
}

/** Whether a press has moved far enough on the screen to be a drag. */
function isDragged(moving: Drag): boolean {
  return Math.hypot(moving.by.x, moving.by.y) * findScale() >= DRAG;
}

/** Moves the box of the instance being dragged by the drag, so its mark follows the pointer. */
function moveBox(found: Map<number, DOMRect[]>): Map<number, DOMRect[]> {
  const moving = drag.value;
  const list = moving ? found.get(moving.layer) : undefined;
  const box = moving && list?.[moving.copy];
  if (moving && list && box) {
    list[moving.copy] = new DOMRect(box.x + moving.by.x, box.y + moving.by.y, box.width, box.height);
  }
  return found;
}

/** Acts on a key: Escape, the arrows, Enter, h, H or f. A key with Ctrl, Alt or Meta is left alone. */
function applyKey(event: KeyboardEvent): void {
  if (event.ctrlKey || event.altKey || event.metaKey) return;
  const path = model.place.path;
  const widgets = drawnWindow.value?.widgets ?? [];
  const places = drawnWindow.value?.places ?? [];
  const current = widgets[file.value.selected ?? -1];
  const siblings = widgets.filter(({ id, parent }) => id !== 0 && parent === current?.parent);
  const select = (id: number | undefined) => () => id !== undefined && store.select(path, id);
  const actions: Record<string, (() => void) | undefined> = {
    Escape: () => store.select(path, null),
    H: showHidden,
    ...(current && {
      Enter: () => store.showCode(places[current.place], current.range),
      h: () => store.hide(path, current.id, true),
      f: () => viewport.value?.fit(rects.value.get(current.id)),
      ArrowUp: select(current.id ? current.parent : undefined),
      ArrowDown: select(widgets.find(({ id, parent }) => id !== 0 && parent === current.id)?.id),
      ArrowLeft: select(siblings[siblings.indexOf(current) - 1]?.id),
      ArrowRight: select(siblings[siblings.indexOf(current) + 1]?.id),
    }),
  };
  const action = actions[event.key];
  if (!action) return;
  event.preventDefault();
  action();
}
</script>

<template>
  <div class="flex min-h-0 min-w-0 flex-1">
    <USplitter
      ref="splitter"
      :key="String(split)"
      :items="panes"
      :auto-save-id="split ? 'pmt.draw.split' : 'pmt.draw.panes'"
      class="min-h-0 min-w-0 flex-1"
      @resize="followPanel"
    >
      <template #code>
        <CodeEditor :model="model" active />
      </template>
      <template #picture>
        <div class="flex h-full min-h-0 min-w-0 flex-1 flex-col">
          <div class="flex h-10 shrink-0 items-center gap-1.5 overflow-x-auto border-b border-default bg-muted px-2">
            <USelect
              v-if="!isCoat && resolutions.length"
              :model-value="resolution"
              :items="resolutions"
              icon="i-lucide-monitor"
              size="sm"
              class="w-36 shrink-0"
              @update:model-value="(value) => settings.write(resolutionKey, String(value))"
            />
            <USelect
              v-else-if="isCoat && frameOptions.length > 1"
              v-model="store.frame"
              placeholder="Field"
              :items="frameOptions"
              icon="i-lucide-shield"
              size="sm"
              class="w-36 shrink-0"
            />
            <USeparator orientation="vertical" class="mx-1 h-4" />
            <UTooltip v-for="toggle in toggles" :key="toggle.label" :text="toggle.tooltip">
              <UButton
                :icon="toggle.icon"
                square
                size="sm"
                :color="toggle.on.value ? 'primary' : 'neutral'"
                :variant="toggle.on.value ? 'soft' : 'ghost'"
                :aria-label="toggle.label"
                :aria-pressed="toggle.on.value"
                @click="toggle.on.value = !toggle.on.value"
              />
            </UTooltip>
            <UTooltip :text="panelButton.tooltip">
              <UButton
                :icon="panelButton.icon"
                :label="panelButton.label"
                size="sm"
                :color="isPanelOpen ? 'primary' : 'neutral'"
                :variant="isPanelOpen ? 'soft' : 'ghost'"
                :aria-pressed="isPanelOpen"
                @click="togglePanel"
              />
            </UTooltip>
          </div>
          <div class="relative flex min-h-0 flex-1 flex-col">
            <PictureViewport
              ref="viewport"
              :backdrop="isCoat"
              :snapshot="isCoat ? snapshotCoat : null"
              :title="drawable"
            >
              <UContextMenu :items="menu" :disabled="isCoat || file.hovered === null" :ui="{ content: 'w-80' }">
                <div
                  class="relative"
                  :class="!isCoat && 'bg-default'"
                  :style="isCoat ? undefined : { width: `${screen.width}px`, height: `${screen.height}px` }"
                >
                  <canvas v-if="isCoat" ref="canvas" class="block" :style="mask" />
                  <template v-else>
                    <div ref="floor" class="absolute inset-0 overflow-hidden" :class="{ 'pmt-bounds': isBounded }" />
                    <div
                      class="pointer-events-none absolute inset-0 border border-(--line-2)"
                      :style="{ borderWidth: 'calc(1px / var(--zoom, 1))' }"
                    >
                      <span
                        class="absolute bottom-full left-0 origin-bottom-left font-mono text-11 whitespace-nowrap text-(--fg-3)"
                        :style="{ transform: 'scale(calc(1 / var(--zoom, 1)))' }"
                      >
                        {{ screen.width }} × {{ screen.height }}
                      </span>
                    </div>
                  </template>
                  <div
                    v-if="hasGrid"
                    class="pmt-grid pointer-events-none absolute inset-0"
                    :style="{ '--cell': gridCell }"
                  />
                  <WidgetMarks
                    :rects="rects"
                    :selected="file.selected"
                    :hovered="file.hovered"
                    :outlined="file.outlined"
                    :window="drawnWindow ?? NO_WINDOW"
                    :property="caretKey"
                    :is-placed="isPlaced"
                  />
                </div>
              </UContextMenu>
              <template #toolbar>
                <span v-if="asyncStatus === 'loading'" class="flex items-center gap-1.5 px-1">
                  <UIcon name="i-lucide-loader-circle" class="size-3.5 animate-spin" />Drawing
                </span>
                <span v-else-if="hasPicture" class="flex items-center gap-1.5 px-1">
                  <UIcon name="i-lucide-circle-check" class="size-3.5 text-success" />Rendered in {{ renderedMs }} ms
                </span>
                <UPopover
                  v-for="list in lists.filter(({ rows }) => rows.length)"
                  :key="list.label"
                  :content="{ side: 'top', align: 'start' }"
                >
                  <UButton
                    :label="`${list.rows.length} ${list.label}`"
                    :icon="list.icon"
                    trailing-icon="i-lucide-chevron-up"
                    :color="list.color"
                    variant="ghost"
                    size="xs"
                  />
                  <template #content>
                    <ul class="flex max-h-80 w-100 flex-col overflow-auto p-1 text-12">
                      <li
                        v-for="(row, ordinal) in list.rows"
                        :key="ordinal"
                        class="flex items-center rounded hover:bg-elevated"
                      >
                        <button
                          type="button"
                          class="flex min-w-0 grow flex-col px-1.5 py-1 text-start"
                          @click="store.select(model.place.path, row.id)"
                        >
                          <span class="truncate text-toned" :class="list.isHidden && 'font-mono'">{{ row.text }}</span>
                          <span v-if="row.detail" class="truncate font-mono text-11 text-dimmed">{{ row.detail }}</span>
                        </button>
                        <UButton
                          v-if="list.isHidden"
                          icon="i-lucide-eye"
                          color="neutral"
                          variant="ghost"
                          size="xs"
                          square
                          aria-label="Show"
                          @click="store.hide(model.place.path, row.id, false)"
                        />
                      </li>
                    </ul>
                  </template>
                </UPopover>
              </template>
            </PictureViewport>
            <div
              v-if="!hasPicture || isEmpty"
              class="pointer-events-none absolute inset-x-0 top-0 bottom-7 flex items-center justify-center p-6"
            >
              <UAlert
                v-if="error && !hasPicture"
                class="pointer-events-auto max-w-120"
                color="error"
                variant="subtle"
                icon="i-lucide-circle-x"
                :title="`${drawable} could not be drawn`"
                :description="error.message"
                :actions="[
                  {
                    label: 'Open in code',
                    icon: 'i-lucide-code',
                    color: 'neutral',
                    variant: 'outline',
                    onClick: openDrawable,
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
              <UEmpty
                v-else-if="isEmpty"
                icon="i-lucide-square-dashed"
                :title="`Nothing to draw: ${drawable}`"
                description="It has no size, texture, text or children of its own."
                size="sm"
              />
              <UEmpty
                v-else-if="!drawable"
                icon="i-lucide-app-window"
                title="No window"
                description="This file holds nothing to draw."
                size="sm"
              />
              <span v-else class="flex items-center gap-2 text-12 text-dimmed">
                <UIcon name="i-lucide-loader-circle" class="size-4 animate-spin" />Drawing
              </span>
            </div>
          </div>
        </div>
      </template>
      <template #panel="{ collapsed }">
        <DrawDetail v-if="!collapsed" :model="model" :window="drawnWindow ?? null" :coat="drawnCoat ?? null" />
      </template>
    </USplitter>
    <CodeLink
      v-if="split && hasPicture"
      :model="model"
      :blocks="blocks"
      :boxes="markBoxes"
      :hidden="hidden"
      :missing="missing"
      :name="drawable"
    />
  </div>
</template>

<style>
.pmt-bounds [data-element] {
  outline: calc(1px / var(--zoom, 1)) dashed color-mix(in srgb, var(--link) 45%, transparent);
  outline-offset: calc(-1px / var(--zoom, 1));
}
/* each line is a pixel wide on the screen at any zoom; every fifth line is stronger, as are the right and bottom
   edges, which close the grid */
.pmt-grid {
  --stroke: calc(1px / var(--zoom, 1));
  --strong-cell: calc(var(--cell) * 5);
  background-image:
    linear-gradient(to left, var(--line-2) var(--stroke), transparent 0),
    linear-gradient(to top, var(--line-2) var(--stroke), transparent 0),
    linear-gradient(to right, var(--line-2) var(--stroke), transparent 0),
    linear-gradient(to bottom, var(--line-2) var(--stroke), transparent 0),
    linear-gradient(to right, var(--line-1) var(--stroke), transparent 0),
    linear-gradient(to bottom, var(--line-1) var(--stroke), transparent 0);
  background-size:
    100% 100%,
    100% 100%,
    var(--strong-cell) var(--strong-cell),
    var(--strong-cell) var(--strong-cell),
    var(--cell) var(--cell),
    var(--cell) var(--cell);
}
</style>
