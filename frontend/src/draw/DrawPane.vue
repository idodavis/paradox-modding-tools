<script setup lang="ts">
/**
 * A GUI file's window drawn from Go's list on the zoomable floor, under a toolbar: the entry it draws (a window, type
 * or template, text icon or format of the file), the resolution and UI scale it is laid out at, the language of its
 * texts and whether every element's bounds are marked. The list is drawn as it comes: each element a box with its
 * texture or its lines of text. A click selects the part (and, with the code beside it, puts the caret there); the
 * caret in the code selects the part it is in; the code marks the selected part's lines where this file writes it;
 * the pointer resting on a part shows its card.
 */
import { computed, nextTick, onScopeDispose, shallowRef, useTemplateRef, watch, type CSSProperties } from "vue";
import { useQuery } from "@pinia/colada";
import { refDebounced, useTimeoutFn } from "@vueuse/core";
import type { Box, Element as DrawElement, Part, Texture } from "@services/internal/draw";
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

const { workspaceId, path } = defineProps<{ workspaceId: string; path: string }>();

const store = useDrawStore();
const editor = useEditorStore();
const { entries, entry, selected, list, parts, hidden, renderedMs, error } = useDraw(
  () => workspaceId,
  () => path,
);
const viewport = useTemplateRef("viewport");
const scene = useTemplateRef("scene");

// how a texture with no frame is drawn in its box, by its fittype; fill, and a fittype none of these, stretch it
const FILL = { size: "100% 100%", position: "0 0" };
const FITS: Record<string, { size: string; position: string }> = {
  centercrop: { size: "cover", position: "center" },
  start: { size: "contain", position: "left top" },
  center: { size: "contain", position: "center" },
  end: { size: "contain", position: "right bottom" },
};
// how a mark outlines an element, in screen pixels: the selected part's, the one under the pointer and the bounds
const MARKS = {
  selected: { style: "solid", color: "var(--accent)", width: 1.5, offset: 2 },
  hovered: { style: "dashed", color: "var(--link)", width: 1.5, offset: 0 },
  bounds: { style: "dashed", color: "color-mix(in srgb, var(--link) 45%, transparent)", width: 1, offset: 0 },
} as const;

/** An element outlined by a kind of mark. */
type Mark = { element: DrawElement; kind: keyof typeof MARKS };

/** A string as a CSS string: quoted, its backslashes and quotes escaped. */
const quote = (text: string) => `"${text.replaceAll("\\", "\\\\").replaceAll('"', '\\"')}"`;
/** Lengths as CSS pixels, one after another. */
const px = (...values: number[]) => values.map((value) => `${value}px`).join(" ");

/** Whether a line and byte column lie in a range, its ends included. */
function holds({ start, end }: Range, line: number, col: number): boolean {
  const afterStart = start.line < line || (start.line === line && start.character <= col);
  const beforeEnd = end.line > line || (end.line === line && end.character >= col);
  return afterStart && beforeEnd;
}

/** The CSS of a box in the scene. */
function boxStyle(box: Box): CSSProperties {
  return { left: `${box.x}px`, top: `${box.y}px`, width: `${box.width}px`, height: `${box.height}px` };
}

/** A clip as `inset()`: how far each side of the element's box is cut, none where the clip holds it. */
function clipOf(box: Box, clip: Box): string {
  const top = clip.y - box.y;
  const right = box.x + box.width - (clip.x + clip.width);
  const bottom = box.y + box.height - (clip.y + clip.height);
  const left = clip.x - box.x;
  return `inset(${px(...[top, right, bottom, left].map((side) => Math.max(0, side)))})`;
}

/** How a texture's image lies in a box: a frame fills the box, the whole image scaled as the frame is and moved so
 * the frame sits at the origin; else its fit. */
function layerOf(texture: Texture, box: Box): { size: string; position: string } {
  const { frame } = texture;
  if (frame.width <= 0 || frame.height <= 0) return FITS[texture.fit] ?? FILL;
  const scaleX = box.width / frame.width;
  const scaleY = box.height / frame.height;
  return {
    size: px(texture.width * scaleX, texture.height * scaleY),
    position: px(-frame.x * scaleX, -frame.y * scaleY),
  };
}

/** The CSS of a texture in a box: its frame scaled to the box or its fit, its slices, its mirror, tint, blend and
 * the mask that keeps it to another texture's shape. */
function textureStyle(texture: Texture, box: Box): CSSProperties {
  const image = `url(${quote(texture.url)})`;
  const style: CSSProperties = {};
  if (texture.mirror) style.transform = "scaleX(-1)";
  if (texture.blend && texture.blend !== "normal") style.mixBlendMode = texture.blend as CSSProperties["mixBlendMode"];
  if (texture.border.some((inset) => inset > 0)) {
    const slices = px(...texture.border);
    return {
      ...style,
      borderStyle: "solid",
      borderWidth: slices,
      borderImage: `${image} ${texture.border.join(" ")} fill / ${slices} ${texture.tiled ? "repeat" : "stretch"}`,
    };
  }
  const layer = layerOf(texture, box);
  // a tint multiplies the image under a layer of its color, and the image's own alpha keeps the shape
  const tinted = texture.tint !== "";
  const masks = [
    ...(tinted ? [{ image, ...layer }] : []),
    ...(texture.mask ? [{ image: `url(${quote(texture.mask.url)})`, ...layerOf(texture.mask, box) }] : []),
  ];
  return {
    ...style,
    backgroundImage: tinted ? `linear-gradient(${texture.tint}, ${texture.tint}), ${image}` : image,
    backgroundRepeat: "no-repeat",
    backgroundSize: tinted ? `${FILL.size}, ${layer.size}` : layer.size,
    backgroundPosition: tinted ? `${FILL.position}, ${layer.position}` : layer.position,
    backgroundBlendMode: tinted ? "multiply" : undefined,
    ...(masks.length
      ? {
          maskImage: masks.map((mask) => mask.image).join(", "),
          maskSize: masks.map((mask) => mask.size).join(", "),
          maskPosition: masks.map((mask) => mask.position).join(", "),
          maskRepeat: "no-repeat",
          maskComposite: "intersect",
        }
      : {}),
  };
}

/** The CSS of an element: its box, alpha and clip, then its texture's or its text's. */
function styleOf(element: DrawElement): CSSProperties {
  const { text } = element;
  return {
    ...boxStyle(element.box),
    opacity: element.alpha,
    clipPath: element.clip ? clipOf(element.box, element.clip) : undefined,
    ...(element.texture ? textureStyle(element.texture, element.box) : {}),
    ...(text
      ? { fontFamily: quote(text.font), fontSize: `${text.size}px`, color: text.color, lineHeight: "normal" }
      : {}),
  };
}

// the elements drawn: the list's, less those of the parts hidden for the session
const drawn = computed(() => (list.value?.elements ?? []).filter((element) => !hidden.value.has(element.part)));
// every element's style at once, so the pointer moving over the scene redraws its marks and not the elements
const styles = computed(() => drawn.value.map(styleOf));

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

// the marks over the elements: the selected part's, the one under the pointer's, and every element's bounds
const hovered = shallowRef(-1);
const marks = computed(() =>
  drawn.value.flatMap((element): Mark[] => {
    const kind = element.part === selected.value ? "selected" : element.part === hovered.value ? "hovered" : "bounds";
    return kind === "bounds" && !store.bounds ? [] : [{ element, kind }];
  }),
);

/** The CSS of a mark: the element's box, outlined as its kind is, at the width it has on the screen at this zoom. */
function markStyle({ element, kind }: Mark): CSSProperties {
  const zoom = viewport.value?.zoom ?? 1;
  const { style, color, width, offset } = MARKS[kind];
  return {
    ...boxStyle(element.box),
    outline: `${width / zoom}px ${style} ${color}`,
    outlineOffset: `${offset / zoom}px`,
  };
}

/** The element an event happened on, or in; null off the elements. */
function elementOf(event: Event): HTMLElement | null {
  return event.target instanceof Element ? event.target.closest<HTMLElement>("[data-part]") : null;
}
/** The part an element draws, -1 for none. */
const partOf = (element: HTMLElement | null) => (element ? Number(element.dataset.part) : -1);

/** Draws another entry of the file, with no part selected and nothing hidden. */
function pickEntry(next: string): void {
  store.picked.set(path, next);
  store.hidden.delete(path);
  selected.value = -1;
}

/** Selects the part clicked; with the code beside it, the caret goes to where the part is written in this file. */
function pick(event: MouseEvent): void {
  selected.value = partOf(elementOf(event));
  const place = parts.value[selected.value]?.location;
  if (place && editor.viewOf(path) === "split" && pathKey(place.path) === pathKey(path))
    editor.open(path, { line: place.range.start.line, col: place.range.start.character });
}

// the caret in the code selects the deepest part it is in: the last the list names, which lists parents first
const caret = refDebounced(
  computed(() =>
    editor.viewOf(path) === "split" && pathKey(editor.activeFile) === pathKey(path) ? editor.caret : null,
  ),
  300,
);
/** Where a part is written, as text, to tell parts apart that are written in the same place. */
const placeOf = (part?: Part) => JSON.stringify([part?.location.path, part?.location.range]);
watch(caret, (position) => {
  if (!position) return;
  const deepest = parts.value.findLastIndex(
    ({ location }) => pathKey(location.path) === pathKey(path) && holds(location.range, position.line, position.col),
  );
  // a part written in the same place (a template used twice) keeps the selection it has
  if (deepest >= 0 && placeOf(parts.value[deepest]) !== placeOf(parts.value[selected.value])) selected.value = deepest;
});

// a part selected elsewhere (the panel, the code) comes into view, and the code marks it where this file writes it
watch(selected, async (part) => {
  const place = parts.value[part]?.location;
  editor.marked = place && pathKey(place.path) === pathKey(path) ? { path, range: place.range } : null;
  await nextTick();
  scene.value?.querySelector(`[data-part="${part}"]`)?.scrollIntoView({ block: "nearest", inline: "nearest" });
});
onScopeDispose(() => (editor.marked = null));

// the pointer resting on a part half a second shows its card, which goes a moment after the pointer leaves the part
// unless it is on the card
const resting = shallowRef<{ part: number; anchor: DOMRect } | null>(null);
let held = false;
const rest = useTimeoutFn(
  (part: number, anchor: DOMRect) => {
    resting.value = { part, anchor };
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

/** Follows the pointer over the elements: the part under it, and its card once it rests there. */
function track(event: PointerEvent): void {
  const target = elementOf(event);
  const part = partOf(target);
  if (part === hovered.value) return;
  hovered.value = part;
  rest.stop();
  dismiss.start();
  if (target) rest.start(part, target.getBoundingClientRect());
}

/** Forgets the part under the pointer when it leaves the scene. */
function untrack(): void {
  hovered.value = -1;
  rest.stop();
  dismiss.start();
}

/** Keeps the card while the pointer is on it, and lets it go a moment after the pointer leaves it. */
function holdCard(hold: boolean): void {
  held = hold;
  if (!hold) dismiss.start();
}

const restingPart = computed(() => (resting.value ? parts.value[resting.value.part] : undefined));
const { data: card } = useQuery(() => ({
  ...hoverQuery({
    id: workspaceId,
    path: restingPart.value?.location.path ?? "",
    line: restingPart.value?.location.range.start.line ?? 0,
    col: restingPart.value?.location.range.start.character ?? 0,
  }),
  enabled: !!restingPart.value,
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

// what the floor says while there is no list to draw
const empty = computed(() => {
  if (list.value) return "";
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
      <Viewport ref="viewport" :width="list?.width ?? store.width" :height="list?.height ?? store.height">
        <div
          ref="scene"
          class="relative size-full bg-default"
          @click="pick"
          @pointermove="track"
          @pointerleave="untrack"
        >
          <div
            v-for="(element, i) in drawn"
            :key="i"
            :data-part="element.part"
            class="absolute box-border"
            :style="styles[i]"
          >
            <template v-if="element.text">
              <div
                v-for="(line, n) in element.text.lines ?? []"
                :key="n"
                class="absolute left-0 whitespace-pre"
                :style="{ top: `${line.y}px` }"
              >
                <template v-for="(run, k) in line.runs ?? []" :key="k">
                  <img
                    v-if="run.icon"
                    :src="run.icon"
                    alt=""
                    class="absolute max-w-none"
                    :style="{ left: `${run.x}px`, height: `${element.text.size}px` }"
                  />
                  <span
                    v-else
                    class="absolute"
                    :style="{
                      left: `${run.x}px`,
                      color: run.color || undefined,
                      fontWeight: run.bold ? 700 : undefined,
                      fontStyle: run.italic ? 'italic' : undefined,
                    }"
                    >{{ run.text }}</span
                  >
                </template>
              </div>
            </template>
          </div>
          <div
            v-for="(mark, i) in marks"
            :key="`mark-${i}`"
            class="pointer-events-none absolute box-border"
            :style="markStyle(mark)"
          />
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
