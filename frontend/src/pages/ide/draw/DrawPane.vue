<!--
  The pane of Visual: the picture on the viewport, the marks over it, the code beside it and the panel, with the tools
  above them in the group's top row. A coat's instance dragged on the picture moves there.
-->
<script setup lang="ts">
import type { ContextMenuItem, SplitterItem } from "@nuxt/ui";
import { useMutation, useQuery } from "@pinia/colada";
import { refDebounced, useDebounceFn, useEventListener, useLocalStorage, useResizeObserver } from "@vueuse/core";
import { Clipboard } from "@wailsio/runtime";
import { Range } from "monaco-editor/editor/editor.api";
import type { IPosition } from "monaco-editor";
import { storeToRefs } from "pinia";
import { computed, nextTick, shallowRef, useTemplateRef, watch, watchEffect, watchPostEffect } from "vue";

import { Format } from "@internal/files/models";
import * as draw from "@services/draw/service";
import * as lsp from "@services/lsp/service";
import type { Instance, Widget } from "@services/draw/models";
import type { Symbol } from "@services/lsp/models";
import type { Place } from "@services/workspace/models";

import { findCommand, findKbds } from "../../../app/commands";
import {
  coatQuery,
  drawablesQuery,
  fileUrl,
  framesQuery,
  settingsCatalogQuery,
  statesQuery,
  windowQuery,
} from "../../../queries";
import { useDrawStore } from "../../../stores/draw";
import { useSettingsStore } from "../../../stores/settings";
import { useWorkspaceStore, VANILLA } from "../../../stores/workspace";
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

const { model } = defineProps<{ model: Model }>();

/**
 * A Drag is a coat's instance being dragged across the picture. The start is where the press began and the offset is
 * how far it has moved, in the picture's pixels. A dropped drag is kept until the coat is drawn again with the
 * instance in its new place.
 */
type Drag = {
  layer: number;
  copy: number;
  instance: Instance;
  key: string;
  start: DOMPoint;
  offset: DOMPoint;
  isDropped: boolean;
};

// the keys an instance's position is written by: an emblem's position, a sub's offset
const POSITION_KEYS = ["position", "offset"];
// the panes side by side: the code and the panel close when dragged past their least size
const PANES: SplitterItem[] = [
  { id: "code", slot: "code", defaultSize: 35, minSize: 15, collapsible: true, collapsedSize: 0 },
  { id: "picture", slot: "picture", defaultSize: 40, minSize: 20 },
  { id: "panel", slot: "panel", defaultSize: 25, minSize: 15, collapsible: true, collapsedSize: 0 },
];
// the command that saves the file in the group's active tab, which this pane draws
const SAVE = findCommand("editor.save");

const store = useDrawStore();
const settings = useSettingsStore();
const workspace = useWorkspaceStore();
const file = computed(() => store.file(model.place.path));
const isCoat = computed(() => model.place.format !== Format.FormatGUI);
const isBounded = shallowRef(false);
const hasGrid = useLocalStorage("pmt.draw.grid", false);
const { isCodeOpen, isPanelOpen } = storeToRefs(store);
// the buttons that open and close the code and the panel
const PANE_BUTTONS = [
  { pane: "code", icon: "i-lucide-panel-left", tooltip: "The code beside the picture", on: isCodeOpen },
  { pane: "panel", icon: "i-lucide-panel-right", tooltip: "The panel", on: isPanelOpen },
];

const splitter = useTemplateRef("splitter");
const code = useTemplateRef("code");
const viewport = useTemplateRef("viewport");
const floor = useTemplateRef<HTMLElement>("floor");
const canvas = useTemplateRef<HTMLCanvasElement>("canvas");
const nodes = shallowRef<PictureNode[]>([]);
const windowRects = shallowRef(new Map<number, DOMRect[]>());
// isPlaced is true when the window lay wholly off the screen and was moved to the edge it passed.
const isPlaced = shallowRef(false);
const renderedMs = shallowRef(0);
const drag = shallowRef<Drag | null>(null);

const { data: catalog } = useQuery(() => ({ ...settingsCatalogQuery, enabled: workspace.isOpen() }));
const { data: frames } = useQuery(() => ({ ...framesQuery, enabled: workspace.isOpen() }));
const { data: drawables } = useQuery(() => ({
  ...drawablesQuery(model.place.path),
  enabled: workspace.isOpen(),
}));

const resolutionKey = "draw.resolution";
const resolution = computed(() => settings.value(resolutionKey));
const resolutions = computed(() => catalog.value?.find(({ key }) => key === resolutionKey)?.choices ?? []);
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
  mutation: async ({ moving, text }: { moving: Drag; text: string }) => {
    const setting = { block: moving.instance.range, key: moving.key, value: text };
    applyEdits(model, (await draw.EditProperties(model.place.path, [setting])) ?? []);
  },
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
const missing = computed(() => drawnWindow.value?.missing ?? []);
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
        widget: id,
        text: widget ? widget.name || widget.type : (drawnCoat.value?.layers?.[id]?.texture ?? ""),
        detail: "",
      };
    }),
  },
]);
// A frame's value is its label. The "Field" option, which no frame is called, draws the coat unframed.
const frameOptions = computed(() => [
  { label: "Field", value: "Field" },
  ...(frames.value ?? []).map(({ label, place }) => ({
    label: place.origin === VANILLA ? label : `${label} · ${workspace.originName(place.origin)}`,
    value: label,
  })),
]);
const framePlace = computed(() => frames.value?.find(({ label }) => label === store.frame)?.place);
const mask = computed(() =>
  framePlace.value ? { maskImage: `url("${fileUrl(framePlace.value.path)}")`, maskSize: "100% 100%" } : {},
);
// the switches over the picture: a window's widget bounds, and the grid over a window or a coat
const toggles = computed(() => [
  ...(isCoat.value
    ? []
    : [{ label: "Bounds", tooltip: "Outline every widget's bounds", icon: "i-lucide-square-dashed", on: isBounded }]),
  { label: "Grid", tooltip: "Draw a grid over the picture", icon: "i-lucide-grid-3x3", on: hasGrid },
]);
// a cell of the grid is ten of a window's pixels, or a tenth of a coat each way
const gridCell = computed(() => (isCoat.value ? "10%" : "10px"));
const caretKey = computed(() => {
  const caret = { lineNumber: model.caret.line, column: model.caret.column };
  const window = drawnWindow.value;
  const widget = window?.widgets?.[file.value.selected ?? -1];
  if (!isCodeOpen.value || !window || !widget) return "";
  const places = window.places ?? [];
  const property = listProperties(window, widget.properties).findLast(
    ({ place, range }) => places[place]?.key === model.place.key && Range.containsPosition(range, caret),
  );
  return property?.key.toLowerCase() ?? "";
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
useEventListener(pane, "pointerleave", () => store.hover(model.place.path, null));
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

// the code and the panel are open or closed as the session chose, once the splitter has laid its panes out
watch([isCodeOpen, isPanelOpen, splitter], () =>
  nextTick(() => {
    for (const button of PANE_BUTTONS) {
      const panel = splitter.value?.panelsRef[PANES.findIndex(({ id }) => id === button.pane)];
      if (button.on.value) panel?.expand();
      else panel?.collapse();
    }
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
 * readPicture fits the picture and then reads the rects of the drawn widgets. It fits the texts their boxes cut and
 * each placeholder to its holder, and moves a window that lies wholly off the screen to the edge it passed.
 */
function readPicture(element: HTMLElement): void {
  fitTexts(nodes.value);
  fitPlaceholders(nodes.value);
  isPlaced.value = placeOnScreen(nodes.value, element);
  windowRects.value = readRects(nodes.value, element);
}

/** findScale finds how much the camera scales the picture. */
function findScale(): number {
  const element = isCoat.value ? canvas.value : floor.value;
  return element?.offsetWidth ? element.getBoundingClientRect().width / element.offsetWidth : 1;
}

/** findPoint finds a point of the given event in the pixels of the picture. */
function findPoint(event: MouseEvent): DOMPoint {
  const origin = (isCoat.value ? canvas.value : floor.value)?.getBoundingClientRect() ?? new DOMRect();
  const scale = findScale();
  return new DOMPoint((event.clientX - origin.left) / scale, (event.clientY - origin.top) / scale);
}

/**
 * findHits finds the widgets or layers drawn under the pointer. A skin or a label stands for the widget it is part of.
 */
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

/** contains reports whether a box holds a point. */
const contains = (box: DOMRect, point: DOMPoint) =>
  point.x >= box.left && point.x <= box.right && point.y >= box.top && point.y <= box.bottom;

/**
 * findCopy returns the index of the copy of a widget (one for each row of a list), or of the instance of a layer, that
 * the pointer is on. It returns null if the pointer is on none.
 */
function findCopy(id: number | null, event: MouseEvent): number | null {
  const point = findPoint(event);
  const copy = id === null ? -1 : (boxes.value.get(id)?.findIndex((box) => contains(box, point)) ?? -1);
  return copy < 0 ? null : copy;
}

/** sortHits sorts the hits at a point in click order: the smallest first, and the topmost first among equal ones. */
function sortHits(hits: number[]): number[] {
  const area = (id: number) => Math.min(...(boxes.value.get(id) ?? []).map((box) => box.width * box.height));
  return hits.toSorted((first, second) => area(first) - area(second));
}

/**
 * findTarget returns the hit a click selects: the one after the selected hit in click order, else the first, so
 * repeated clicks on one spot reach every widget there. It returns null for no hits.
 */
function findTarget(hits: number[]): number | null {
  const order = sortHits(hits);
  return order.length ? order[(order.indexOf(file.value.selected ?? -1) + 1) % order.length] : null;
}

/** openType opens the definition of the type of the widget written in a file at the given range. */
async function openType(place: Place, range: Widget["range"]): Promise<void> {
  const [type] = (await lsp.Definition(place.path, range.startLineNumber, range.startColumn)) ?? [];
  if (type) store.showCode(type.place, type.range);
}

/** snapshotCoat makes a PNG of the coat as drawn, cut to the frame where one is chosen, its transparent pixels kept. */
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

/** findEntry finds the entry of the file written at a position, or undefined where none is. */
function findEntry(position: IPosition): Symbol | undefined {
  return drawables.value?.findLast(({ range }) => Range.containsPosition(range, position));
}

/** openDrawable opens the code of the drawn drawable. */
function openDrawable(): void {
  const entry = drawables.value?.find(({ name }) => name === drawable.value);
  if (entry) store.showCode(model.place, entry.range);
}

/** showHidden shows every widget the session hides. */
function showHidden(): void {
  file.value.hidden.forEach((id) => store.hide(model.place.path, id, false));
}

/**
 * applyClick selects what is clicked: the next widget under the pointer with each click on one spot, or nothing for a
 * click off the picture. Selecting a widget or layer opens a closed panel on its Properties tab, or Layers for a coat.
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

/**
 * togglePane opens the pane of the given button, or closes it if it is open. A panel that opens shows a window's States
 * tab or a coat's Layers.
 */
function togglePane(button: (typeof PANE_BUTTONS)[number]): void {
  if (button.pane === "panel" && !isPanelOpen.value) file.value.tab = isCoat.value ? "layers" : "states";
  button.on.value = !button.on.value;
}

/**
 * followPane keeps the button of a pane in step with the user resizing the pane, by drag or keys: a pane resized to no
 * size is closed. The first size of a pane, read from the splitter's saved layout, is ignored, since the session
 * decides whether the pane opens.
 */
function followPane(index: number, size: number, previous: number | undefined): void {
  const button = PANE_BUTTONS.find(({ pane }) => pane === PANES[index]?.id);
  if (button && previous !== undefined) button.on.value = size > 0;
}

/**
 * openMenu selects the widget that is right-clicked, so that its menu opens: the selected widget if it is under the
 * pointer, else the first hit.
 */
function openMenu(event: MouseEvent): void {
  const hits = sortHits(findHits(event));
  const target = hits.find((id) => id === file.value.selected) ?? hits.at(0);
  if (target !== undefined) store.select(model.place.path, target, findCopy(target, event));
}

/** applyPointer follows the pointer: it hovers what a click there would select. */
function applyPointer(event: PointerEvent): void {
  const target = findTarget(findHits(event));
  store.hover(model.place.path, target, findCopy(target, event));
}

/**
 * startDrag starts dragging the coat's instance under a press, if this file writes it: the selected layer's if it is
 * under the pointer, else the one a click there would select. The camera then does not pan. A default instance, which
 * no block writes, is not dragged.
 */
function startDrag(event: PointerEvent): void {
  const coat = drawnCoat.value;
  const hits = sortHits(findHits(event));
  const layer = hits.find((id) => id === file.value.selected) ?? findTarget(hits);
  const copy = findCopy(layer, event);
  const layerData = layer === null ? undefined : coat?.layers?.[layer];
  const instance = copy === null ? undefined : layerData?.instances?.[copy];
  const key = POSITION_KEYS.find((candidate) => layerData?.inputs?.[candidate]);
  if (event.button !== 0 || layer === null || copy === null || !coat || !layerData || !instance || !key) return;
  if (!instance.range.startLineNumber || !isOwn(coat, layerData, model.place.key)) return;
  event.stopPropagation();
  canvas.value?.setPointerCapture(event.pointerId);
  drag.value = { layer, copy, instance, key, start: findPoint(event), offset: new DOMPoint(), isDropped: false };
}

/** moveDrag moves the dragged instance's box with the pointer. Once the press is a drag, the instance is selected. */
function moveDrag(event: PointerEvent): void {
  const moving = drag.value;
  if (!moving || moving.isDropped) return;
  const point = findPoint(event);
  drag.value = { ...moving, offset: new DOMPoint(point.x - moving.start.x, point.y - moving.start.y) };
  if (isDragged(drag.value)) store.select(model.place.path, moving.layer, moving.copy);
}

/**
 * dropDrag ends a press on the coat. After a drag, the instance's new position is written in the file as one edit; a
 * press that never became a drag is a click.
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
  const position = [across + moving.offset.x / canvas.value.width, down + moving.offset.y / canvas.value.height];
  writePosition({ moving, text: `{ ${position.map((part) => Number(part.toFixed(3))).join(" ")} }` });
}

/** isDragged reports whether a press has moved far enough on the screen to be a drag. */
function isDragged(moving: Drag): boolean {
  return Math.hypot(moving.offset.x, moving.offset.y) * findScale() >= DRAG;
}

/** moveBox moves the box of the instance being dragged by the drag's offset, so its mark follows the pointer. */
function moveBox(layerBoxes: Map<number, DOMRect[]>): Map<number, DOMRect[]> {
  const moving = drag.value;
  const list = moving ? layerBoxes.get(moving.layer) : undefined;
  const box = moving && list?.[moving.copy];
  if (moving && list && box) {
    list[moving.copy] = new DOMRect(box.x + moving.offset.x, box.y + moving.offset.y, box.width, box.height);
  }
  return layerBoxes;
}

/** applyKey acts on a key: Escape, the arrows, Enter, h, H or f. A key with Ctrl, Alt or Meta is left alone. */
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
    <Teleport defer to="#visual-tools">
      <UTooltip text="Save" :kbds="findKbds('editor.save')">
        <UButton
          label="Save"
          icon="i-lucide-save"
          size="sm"
          :disabled="!model.unsaved"
          @click="SAVE?.onSelect?.($event)"
        />
      </UTooltip>
      <USeparator orientation="vertical" class="mx-1 h-4" />
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
      <span class="grow" />
      <UTooltip v-for="button in PANE_BUTTONS" :key="button.pane" :text="button.tooltip">
        <UButton
          :icon="button.icon"
          square
          size="sm"
          :color="button.on.value ? 'primary' : 'neutral'"
          :variant="button.on.value ? 'soft' : 'ghost'"
          :aria-label="button.tooltip"
          :aria-pressed="button.on.value"
          @click="togglePane(button)"
        />
      </UTooltip>
    </Teleport>
    <USplitter
      ref="splitter"
      :items="PANES"
      auto-save-id="pmt.draw.panes"
      class="min-h-0 min-w-0 flex-1"
      @resize="followPane"
    >
      <template #code="{ collapsed }">
        <CodeEditor v-if="!collapsed" ref="code" :model="model" active />
      </template>
      <template #picture>
        <div class="relative flex h-full min-h-0 min-w-0 flex-1 flex-col">
          <PictureViewport ref="viewport" :backdrop="isCoat" :snapshot="isCoat ? snapshotCoat : null" :title="drawable">
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
                  :window="drawnWindow ?? null"
                  :missing="missing"
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
                        @click="store.select(model.place.path, row.widget)"
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
                        @click="store.hide(model.place.path, row.widget, false)"
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
      </template>
      <template #panel="{ collapsed }">
        <DrawDetail v-if="!collapsed" :model="model" :window="drawnWindow ?? null" :coat="drawnCoat ?? null" />
      </template>
    </USplitter>
    <CodeLink
      v-if="isCodeOpen && hasPicture"
      :model="model"
      :code-editor="code?.view"
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
