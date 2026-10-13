<script setup lang="ts">
/**
 * A GUI file's window drawn from Go's list on the zoomable floor, under a toolbar: the entry it draws (a window, type
 * or template, text icon or format of the file), the resolution and UI scale it is laid out at, the language of its
 * texts and whether every element's bounds are marked. The list is nested as elements the browser lays out
 * (widgets.ts). A click selects the element (and, with the code beside it, puts the caret there); the caret in the
 * code selects the element it is in; the code marks the selected element's lines where this file writes it; the
 * pointer resting on an element shows its card.
 */
import { computed, nextTick, onScopeDispose, shallowRef, useTemplateRef, watch } from "vue";
import { useQuery } from "@pinia/colada";
import { refDebounced, useResizeObserver, useTimeoutFn } from "@vueuse/core";
import type { Element as DrawElement } from "@services/internal/draw";
import type { Range } from "@services/internal/lsp";
import { useDraw } from "../composables/useDraw";
import { pathKey } from "../composables/useIdeRoots";
import { cardSaysSomething } from "../editor/providers";
import { hoverQuery } from "../queries";
import { RESOLUTIONS, SCALES, useDrawStore } from "../stores/draw";
import { useEditorStore } from "../stores/editor";
import HoverCard from "../ui/HoverCard.vue";
import StatusFacts from "../ui/StatusFacts.vue";
import Viewport from "./Viewport.vue";
import { DrawnWindow, quote } from "./widgets";

const { workspaceId, path } = defineProps<{ workspaceId: string; path: string }>();

const store = useDrawStore();
const editor = useEditorStore();
const { entries, entry, selected, list, elements, hidden, renderedMs, error } = useDraw(
  () => workspaceId,
  () => path,
);
const viewport = useTemplateRef("viewport");
const scene = useTemplateRef("scene");

/** Whether a line and byte column lie in a range, its ends included. */
function holds({ start, end }: Range, line: number, col: number): boolean {
  const afterStart = start.line < line || (start.line === line && start.character <= col);
  const beforeEnd = end.line > line || (end.line === line && end.character >= col);
  return afterStart && beforeEnd;
}

// the scene: a window's resolution, laid out at the UI scale; an entry drawn alone is the extent of everything it
// draws, measured once drawn and again when its root changes size (its fonts loading), the root moved so what hangs
// above or left of it is on the scene
const root = useTemplateRef("root");
const extent = shallowRef({ x: 0, y: 0, width: 0, height: 0 });
function measure(): void {
  const origin = scene.value?.querySelector('[data-element="0"]');
  if (!origin || list.value?.width) {
    extent.value = { x: 0, y: 0, width: 0, height: 0 };
    return;
  }
  const zoom = viewport.value?.zoom ?? 1;
  const at = origin.getBoundingClientRect();
  let left = 0;
  let top = 0;
  let right = at.width;
  let bottom = at.height;
  for (const drawn of origin.parentElement?.querySelectorAll("[data-element]") ?? []) {
    const box = drawn.getBoundingClientRect();
    if (!box.width || !box.height) continue;
    left = Math.min(left, box.left - at.left);
    top = Math.min(top, box.top - at.top);
    right = Math.max(right, box.right - at.left);
    bottom = Math.max(bottom, box.bottom - at.top);
  }
  extent.value = { x: -left / zoom, y: -top / zoom, width: (right - left) / zoom, height: (bottom - top) / zoom };
}
watch([list, hidden], () => nextTick(measure), { flush: "post" });
useResizeObserver(root, measure);
const sceneWidth = computed(() => list.value?.width || extent.value.width);
const sceneHeight = computed(() => list.value?.height || extent.value.height);

// the fonts the list's texts use, registered with the page while a list names them
const faces = new Map<string, FontFace>();
watch(
  () => list.value?.fonts,
  (fonts) => {
    const wanted = new Map((fonts ?? []).map((font) => [`${font.name}|${font.style}|${font.url}`, font]));
    for (const [key, face] of faces) {
      if (wanted.has(key)) continue;
      document.fonts.delete(face);
      faces.delete(key);
    }
    for (const [key, font] of wanted) {
      if (faces.has(key)) continue;
      const face = new FontFace(font.name, `url(${quote(font.url)})`, {
        weight: font.style === "bold" ? "700" : "400",
        style: font.style === "italic" ? "italic" : "normal",
      });
      faces.set(key, face);
      document.fonts.add(face);
    }
  },
  { immediate: true },
);
onScopeDispose(() => faces.forEach((face) => document.fonts.delete(face)));

/** The element's div an event happened on, or in; null off the elements. */
function divOf(event: Event): HTMLElement | null {
  return event.target instanceof Element ? event.target.closest<HTMLElement>("[data-element]") : null;
}
/** The element a div draws, -1 for none. */
const elementOf = (div: HTMLElement | null) => (div ? Number(div.dataset.element) : -1);

/** Draws another entry of the file, with no element selected and nothing hidden. */
function pickEntry(next: string): void {
  store.picked.set(path, next);
  store.hidden.delete(path);
  selected.value = -1;
}

/** Selects the element clicked; with the code beside it, the caret goes to where the element is written in this
 * file. */
function pick(event: MouseEvent): void {
  selected.value = elementOf(divOf(event));
  const place = elements.value[selected.value]?.location;
  if (place && editor.viewOf(path) === "split" && pathKey(place.path) === pathKey(path))
    editor.open(path, { line: place.range.start.line, col: place.range.start.character });
}

// the caret in the code selects the deepest element it is in: the last the list names, which lists parents first
const caret = refDebounced(
  computed(() =>
    editor.viewOf(path) === "split" && pathKey(editor.activeFile) === pathKey(path) ? editor.caret : null,
  ),
  300,
);
/** Where an element is written, as text, to tell elements apart that are written in the same place. */
const placeOf = (element?: DrawElement) => JSON.stringify([element?.location.path, element?.location.range]);
watch(caret, (position) => {
  if (!position) return;
  const deepest = elements.value.findLastIndex(
    ({ location }) => pathKey(location.path) === pathKey(path) && holds(location.range, position.line, position.col),
  );
  // an element written in the same place (a template used twice) keeps the selection it has
  if (deepest >= 0 && placeOf(elements.value[deepest]) !== placeOf(elements.value[selected.value]))
    selected.value = deepest;
});

// an element selected elsewhere (the panel, the code) comes into view, and the code marks it where this file writes it
watch(selected, async (element) => {
  const place = elements.value[element]?.location;
  editor.marked = place && pathKey(place.path) === pathKey(path) ? { path, range: place.range } : null;
  await nextTick();
  scene.value?.querySelector(`[data-element="${element}"]`)?.scrollIntoView({ block: "nearest", inline: "nearest" });
});
onScopeDispose(() => (editor.marked = null));

// the pointer resting on an element half a second shows its card, which goes a moment after the pointer leaves the
// element unless it is on the card
const hovered = shallowRef<HTMLElement | null>(null);
const resting = shallowRef<{ element: number; anchor: DOMRect } | null>(null);
let held = false;
const rest = useTimeoutFn(
  (element: number, anchor: DOMRect) => {
    resting.value = { element, anchor };
  },
  500,
  { immediate: false },
);
const dismiss = useTimeoutFn(
  () => {
    if (!held) resting.value = null;
  },
  300,
  { immediate: false },
);

/** Follows the pointer over the elements: the one under it, outlined, and its card once it rests there. */
function track(event: PointerEvent): void {
  const target = divOf(event);
  if (target === hovered.value) return;
  hovered.value?.classList.remove("pmt-hovered");
  target?.classList.add("pmt-hovered");
  hovered.value = target;
  rest.stop();
  dismiss.start();
  if (target) rest.start(elementOf(target), target.getBoundingClientRect());
}

/** Forgets the element under the pointer when it leaves the scene. */
function untrack(): void {
  hovered.value?.classList.remove("pmt-hovered");
  hovered.value = null;
  rest.stop();
  dismiss.start();
}

/** Keeps the card while the pointer is on it, and lets it go a moment after the pointer leaves it. */
function holdCard(hold: boolean): void {
  held = hold;
  if (!hold) dismiss.start();
}

const restingElement = computed(() => (resting.value ? elements.value[resting.value.element] : undefined));
const { data: card } = useQuery(() => ({
  ...hoverQuery({
    id: workspaceId,
    path: restingElement.value?.location.path ?? "",
    line: restingElement.value?.location.range.start.line ?? 0,
    col: restingElement.value?.location.range.start.character ?? 0,
  }),
  enabled: !!restingElement.value,
}));
const cardShown = computed(() => !!resting.value && !!card.value && cardSaysSomething(card.value));
const reference = computed(() => {
  const anchor = resting.value?.anchor;
  return anchor ? { getBoundingClientRect: () => anchor } : undefined;
});
const keepFocus = {
  onOpenAutoFocus: (e: Event) => e.preventDefault(),
  onCloseAutoFocus: (e: Event) => e.preventDefault(),
};

// what Go did not find: the first sentence it gives and the count of the rest
const missing = computed(() => {
  const [first, ...rest] = list.value?.missing ?? [];
  return first ? `${first}${rest.length ? `, ${rest.length} more` : ""}` : "";
});

// what the floor says while there is no list to draw, or why the entry drawn shows nothing
const empty = computed(() => {
  if (list.value) return list.value.missing?.find((sentence) => sentence.startsWith("Nothing to draw")) ?? "";
  if (!entry.value) return "No window";
  return error.value ? error.value.message : "Drawing";
});
</script>

<template>
  <div class="flex min-h-0 min-w-0 flex-1 flex-col">
    <div class="flex min-h-10 shrink-0 flex-wrap items-center gap-2 border-b border-default bg-muted px-2.5 py-1.5">
      <USelectMenu
        :model-value="entry"
        :items="entries"
        size="xs"
        class="w-44"
        placeholder="No window"
        @update:model-value="pickEntry"
      />
      <USelect v-model="store.width" :items="RESOLUTIONS" size="xs" class="w-32" />
      <USelect v-model="store.scale" :items="SCALES" size="xs" class="w-20" />
      <USelect v-model="store.language" :items="store.languages" size="xs" class="w-32" />
      <span class="grow" />
      <USwitch v-model="store.bounds" label="Bounds" />
    </div>
    <div class="relative flex min-h-0 flex-1 flex-col">
      <Viewport ref="viewport" :width="sceneWidth" :height="sceneHeight">
        <div
          ref="scene"
          class="pmt-scene relative grid bg-default"
          :class="[store.bounds && 'pmt-bounds', list?.width && 'size-full']"
          :style="{
            '--zoom': viewport?.zoom ?? 1,
            gridTemplate: '100% / 100%',
            ...(list?.width ? {} : { width: `${extent.width}px`, height: `${extent.height}px` }),
          }"
          @click="pick"
          @pointermove="track"
          @pointerleave="untrack"
        >
          <div
            ref="root"
            class="grid"
            :style="{ gridArea: '1 / 1', gridTemplate: '100% / 100%', translate: `${extent.x}px ${extent.y}px` }"
          >
            <DrawnWindow
              v-if="list"
              :elements="elements"
              :hidden="hidden"
              :selected="selected"
              :scale="list.width ? store.scale : 1"
            />
          </div>
        </div>
      </Viewport>
      <p v-if="empty" class="pointer-events-none absolute inset-0 flex items-center justify-center text-12 text-dimmed">
        {{ empty }}
      </p>
    </div>
    <UPopover
      :open="cardShown"
      :reference="reference"
      :content="{ side: 'bottom', align: 'start', ...keepFocus }"
      @update:open="(open) => !open && (resting = null)"
    >
      <template #content>
        <div
          class="max-h-(--reka-popover-content-available-height) overflow-y-auto"
          @mouseenter="holdCard(true)"
          @mouseleave="holdCard(false)"
        >
          <HoverCard :card="card ?? null" />
        </div>
      </template>
    </UPopover>
    <StatusFacts v-if="list">
      <span>Rendered in {{ renderedMs }} ms</span>
      <span v-if="missing">{{ missing }}</span>
    </StatusFacts>
  </div>
</template>

<style>
/* the elements the browser lays out: their textures as layers under them, a text icon a line high, and the marks
   around them at the width they have on the screen at this zoom */
.pmt-element {
  box-sizing: border-box;
}
.pmt-layer {
  position: absolute;
  inset: 0;
  pointer-events: none;
}
.pmt-icon {
  display: inline-block;
  height: 1em;
  vertical-align: middle;
}
.pmt-bounds .pmt-element {
  outline: calc(1px / var(--zoom)) dashed color-mix(in srgb, var(--link) 45%, transparent);
}
.pmt-element.pmt-hovered {
  outline: calc(1.5px / var(--zoom)) dashed var(--link);
}
.pmt-element.pmt-selected {
  outline: calc(1.5px / var(--zoom)) solid var(--accent);
  outline-offset: calc(2px / var(--zoom));
}
</style>
