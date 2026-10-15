<script setup lang="ts">
/**
 * The marks over a drawn picture, from each widget's rect: a window's screen framed with its size, the selected and
 * hovered widgets outlined with a chip, a badge on what is missing, and for the property on the caret's line a
 * margin's bands or an anchor's two points. Strokes and chips keep one size on the screen at any zoom.
 */
import { computed } from "vue";
import type { Absent, Element as DrawElement } from "@services/internal/draw";
import type { DrawFile } from "../stores/draw";
import { anchorOf } from "./picture";
import { typeIcon } from "./widgets";

const { file, elements, missing, screen, zoom, host } = defineProps<{
  file: DrawFile;
  elements: DrawElement[];
  missing: Absent[];
  /** A window's screen, framed with its size above its corner; null for an entry drawn alone. */
  screen: { width: number; height: number; label: string } | null;
  /** How big a pixel of the picture is on the screen. */
  zoom: number;
  /** The picture's root, whose nodes the margin guide reads its padding from. */
  host: HTMLElement | null;
}>();

/** The properties the caret's line draws a guide for: a margin's bands, or an anchor's two points. */
const GUIDES: Record<string, "margin" | "anchor"> = {
  margin: "margin",
  margin_left: "margin",
  margin_right: "margin",
  margin_top: "margin",
  margin_bottom: "margin",
  parentanchor: "anchor",
  widgetanchor: "anchor",
  position: "anchor",
  position_x: "anchor",
  position_y: "anchor",
};

/** Each mark's look: its outline and its chip. */
const LOOKS = {
  selection: { outline: "solid var(--accent)", out: 2, chip: "bg-(--accent) text-(--accent-ink)" },
  hover: { outline: "dashed var(--link)", out: 0, chip: "bg-(--link) text-(--bg-0)" },
};

/** An element's first rect; undefined for none drawn. */
const rectOf = (element: number): DOMRect | undefined => file.rects.get(element)?.[0];

// the selected and hovered widgets' outlines and chips
const marks = computed(() =>
  (
    [
      ["hover", file.hover !== file.selection ? file.hover : -1],
      ["selection", file.selection],
    ] as const
  ).flatMap(([look, index]) => {
    const rect = rectOf(index);
    const element = elements[index];
    return rect && element ? [{ look, rect, label: element.label, icon: typeIcon(element.type) }] : [];
  }),
);
// a badge on each widget that asked for something the game's files did not give
const badges = computed(() =>
  [...new Set(missing.map(({ element }) => element))].flatMap((index) => {
    const rect = rectOf(index);
    return rect ? [{ index, rect }] : [];
  }),
);

/** The guide the property on the caret's line draws: a margin's bands with their widths, from the widget's rect and
 * its computed padding, or an anchor's two points from its rect, its holder's and the words. */
const guide = computed(() => {
  const caret = file.caret;
  const key = caret?.key.toLowerCase() ?? "";
  const element = caret ? elements[caret.element] : undefined;
  const rect = caret ? rectOf(caret.element) : undefined;
  if (!caret || !element || !rect || !GUIDES[key]) return null;
  if (GUIDES[key] === "margin") {
    const node = host?.querySelector<HTMLElement>(`[data-element="${caret.element}"]`);
    const style = node ? getComputedStyle(node) : null;
    const widths = (["paddingTop", "paddingRight", "paddingBottom", "paddingLeft"] as const).map((side) =>
      parseFloat(style?.[side] ?? "0"),
    );
    return { shape: "margin" as const, rect, widths };
  }
  const holder = rectOf(element.parent) ?? rect;
  const properties = element.properties ?? {};
  const own = properties.widgetanchor ?? properties.parentanchor;
  return {
    shape: "anchor" as const,
    from: [
      holder.x + (anchorOf(properties.parentanchor, 0) ?? 0) * holder.width,
      holder.y + (anchorOf(properties.parentanchor, 1) ?? 0) * holder.height,
    ],
    to: [rect.x + (anchorOf(own, 0) ?? 0) * rect.width, rect.y + (anchorOf(own, 1) ?? 0) * rect.height],
    word: properties[key] ?? "",
  };
});
</script>

<template>
  <div class="pointer-events-none absolute inset-0">
    <div
      v-if="screen"
      class="absolute top-0 left-0 border border-(--line-2)"
      :style="{ width: `${screen.width}px`, height: `${screen.height}px`, borderWidth: `${1 / zoom}px` }"
    >
      <span
        class="absolute bottom-full left-0 origin-bottom-left font-mono text-11 whitespace-nowrap text-(--fg-3)"
        :style="{ transform: `scale(${1 / zoom})` }"
        >{{ screen.label }}</span
      >
    </div>
    <template v-for="mark in marks" :key="mark.look">
      <div
        class="absolute"
        :style="{
          left: `${mark.rect.x}px`,
          top: `${mark.rect.y}px`,
          width: `${mark.rect.width}px`,
          height: `${mark.rect.height}px`,
          outline: `${1.5 / zoom}px ${LOOKS[mark.look].outline}`,
          outlineOffset: `${LOOKS[mark.look].out / zoom}px`,
        }"
      />
      <span
        class="absolute flex h-4.5 origin-bottom-left items-center gap-1.25 rounded ps-1.25 pe-1.5 font-mono text-11 leading-none whitespace-nowrap"
        :class="LOOKS[mark.look].chip"
        :style="{
          left: `${mark.rect.x}px`,
          top: `${mark.rect.y}px`,
          transform: `translateY(-100%) scale(${1 / zoom})`,
        }"
      >
        <UIcon :name="mark.icon" class="size-3 shrink-0" />{{ mark.label
        }}<span class="opacity-70">{{ Math.round(mark.rect.width) }} × {{ Math.round(mark.rect.height) }}</span>
      </span>
    </template>
    <span
      v-for="badge in badges"
      :key="badge.index"
      class="absolute flex size-3.5 items-center justify-center rounded bg-(--orange) text-(--bg-0)"
      :style="{
        left: `${badge.rect.right}px`,
        top: `${badge.rect.y}px`,
        transform: `translate(-50%, -50%) scale(${1 / zoom})`,
      }"
    >
      <UIcon name="i-lucide-triangle-alert" class="size-2.5" />
    </span>
    <div
      v-if="guide?.shape === 'margin'"
      class="absolute border border-dashed border-(--link) bg-(--green)/20"
      :style="{
        left: `${guide.rect.x}px`,
        top: `${guide.rect.y}px`,
        width: `${guide.rect.width}px`,
        height: `${guide.rect.height}px`,
        borderWidth: `${1 / zoom}px`,
      }"
    >
      <div
        class="absolute border border-dashed border-(--link) bg-(--bg-0)/60"
        :style="{ inset: guide.widths.map((width) => `${width}px`).join(' '), borderWidth: `${1 / zoom}px` }"
      />
      <span
        v-for="(width, side) in guide.widths"
        :key="side"
        class="absolute font-mono text-10 text-(--green)"
        :style="{
          left: side === 1 ? 'auto' : side === 3 ? `${width / 2}px` : '50%',
          right: side === 1 ? `${width / 2}px` : 'auto',
          top: side === 2 ? 'auto' : side === 0 ? `${width / 2}px` : '50%',
          bottom: side === 2 ? `${width / 2}px` : 'auto',
          transform: `translate(-50%, -50%) scale(${1 / zoom})`,
          display: width ? undefined : 'none',
        }"
        >{{ Math.round(width) }}</span
      >
    </div>
    <svg v-if="guide?.shape === 'anchor'" class="absolute inset-0 size-full overflow-visible">
      <line
        :x1="guide.from[0]"
        :y1="guide.from[1]"
        :x2="guide.to[0]"
        :y2="guide.to[1]"
        stroke="var(--link)"
        stroke-width="1.5"
        vector-effect="non-scaling-stroke"
      />
      <circle
        v-for="(point, i) in [guide.from, guide.to]"
        :key="i"
        :cx="point[0]"
        :cy="point[1]"
        :r="4 / zoom"
        :fill="i ? 'var(--accent)' : 'var(--link)'"
        stroke="var(--bg-0)"
        stroke-width="2"
        vector-effect="non-scaling-stroke"
      />
    </svg>
    <span
      v-if="guide?.shape === 'anchor' && guide.word"
      class="absolute flex h-4.5 origin-top-left items-center rounded bg-(--link) px-1.5 font-mono text-11 leading-none whitespace-nowrap text-(--bg-0)"
      :style="{
        left: `${guide.to[0]}px`,
        top: `${guide.to[1]}px`,
        transform: `translate(${8 / zoom}px, ${8 / zoom}px) scale(${1 / zoom})`,
      }"
    >
      {{ guide.word }}
    </span>
  </div>
</template>
