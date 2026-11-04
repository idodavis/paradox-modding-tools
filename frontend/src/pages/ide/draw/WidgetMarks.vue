<!-- The marks over the picture: selected and hovered widgets, missing properties and the caret's property guide. -->
<script setup lang="ts">
import { computed } from "vue";

import type { Missing, Window } from "@services/draw/models";

import { applyProperties, listProperties } from "./picture";
import { findTypeIcon } from "./properties";

const props = defineProps<{
  rects: Map<number, DOMRect>;
  selected: number | null;
  hovered: number | null;
  /** The widgets whose condition the state under the pointer decides. */
  outlined: number[];
  /** The drawn window, or null for a coat. */
  window: Window | null;
  /** The widgets that lack a property the game's files did not give. */
  missing: Missing[];
  /** The key of the property on the caret's line, or "". */
  property: string;
  /** Whether the window lay wholly off the screen and is drawn at its edge. */
  isPlaced: boolean;
}>();

const SIDES = ["top", "right", "bottom", "left"];

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

const LOOKS = {
  hovered: { outline: "dashed var(--link)", out: 0, chip: "bg-(--link) text-(--bg-0)" },
  selected: { outline: "solid var(--accent)", out: 2, chip: "bg-(--accent) text-(--accent-ink)" },
};

const marks = computed(() => {
  const ids: { look: keyof typeof LOOKS; id: number | null }[] = [
    { look: "hovered", id: props.hovered === props.selected ? null : props.hovered },
    { look: "selected", id: props.selected },
  ];
  return ids.flatMap(({ look, id }) => {
    const rect = id === null ? undefined : props.rects.get(id);
    const widget = id === null ? undefined : props.window?.widgets?.[id];
    return rect ? [{ look, rect, widget }] : [];
  });
});

const outlines = computed(() => props.outlined.flatMap((id) => props.rects.get(id) ?? []));

const badges = computed(() =>
  [...new Set(props.missing.map(({ widget }) => widget))].flatMap((id) => props.rects.get(id) ?? []),
);

const guide = computed(() => {
  const widget = props.selected === null ? undefined : props.window?.widgets?.[props.selected];
  const rect = props.selected === null ? undefined : props.rects.get(props.selected);
  const shape = GUIDES[props.property];
  if (!props.window || !widget || !rect || !shape) return null;
  const style = document.createElement("div").style;
  const properties = listProperties(props.window, widget.properties);
  applyProperties(properties, style);
  if (shape === "margin") {
    return { shape, rect, widths: SIDES.map((side) => parseFloat(style.getPropertyValue(`--margin-${side}`)) || 0) };
  }
  const holder = props.rects.get(widget.parent) ?? rect;
  const findAnchor = (prefix: string, axis: string) => Number(style.getPropertyValue(`--${prefix}-${axis}`) || 0);
  const findOwnAnchor = (axis: string) => Number(style.getPropertyValue(`--own-${axis}`) || findAnchor("parent", axis));
  return {
    shape,
    start: [holder.x + findAnchor("parent", "x") * holder.width, holder.y + findAnchor("parent", "y") * holder.height],
    end: [rect.x + findOwnAnchor("x") * rect.width, rect.y + findOwnAnchor("y") * rect.height],
    text: properties.findLast(({ key }) => key.toLowerCase() === props.property)?.value ?? "",
  };
});
</script>

<template>
  <div class="pointer-events-none absolute inset-0">
    <div
      v-for="(rect, ordinal) in outlines"
      :key="`outline${ordinal}`"
      class="absolute bg-(--orange)/10"
      :style="{
        left: `${rect.x}px`,
        top: `${rect.y}px`,
        width: `${rect.width}px`,
        height: `${rect.height}px`,
        outline: 'calc(1.5px / var(--zoom, 1)) dotted var(--orange)',
      }"
    />
    <template v-for="mark in marks" :key="mark.look">
      <div
        class="absolute"
        :style="{
          left: `${mark.rect.x}px`,
          top: `${mark.rect.y}px`,
          width: `${mark.rect.width}px`,
          height: `${mark.rect.height}px`,
          outline: `calc(1.5px / var(--zoom, 1)) ${LOOKS[mark.look].outline}`,
          outlineOffset: `calc(${LOOKS[mark.look].out}px / var(--zoom, 1))`,
        }"
      />
      <span
        class="absolute flex h-4.5 origin-bottom-left items-center gap-1.25 rounded ps-1.25 pe-1.5 font-mono text-11 leading-none whitespace-nowrap"
        :class="LOOKS[mark.look].chip"
        :style="{
          left: `${mark.rect.x}px`,
          top: `${mark.rect.y}px`,
          transform: 'translateY(-100%) scale(calc(1 / var(--zoom, 1)))',
        }"
      >
        <template v-if="mark.widget">
          <UIcon :name="findTypeIcon(mark.widget.type)" class="size-3 shrink-0" />
          {{ mark.widget.type }} {{ mark.widget.name }}
        </template>
        <span class="opacity-70">{{ Math.round(mark.rect.width) }} × {{ Math.round(mark.rect.height) }}</span>
        <span v-if="isPlaced && mark.widget?.id === 0" class="opacity-70">· placed at the screen's edge</span>
      </span>
    </template>
    <span
      v-for="(rect, ordinal) in badges"
      :key="ordinal"
      class="absolute flex size-3.5 items-center justify-center rounded bg-(--orange) text-(--bg-0)"
      :style="{
        left: `${rect.right}px`,
        top: `${rect.y}px`,
        transform: 'translate(-50%, -50%) scale(calc(1 / var(--zoom, 1)))',
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
        borderWidth: 'calc(1px / var(--zoom, 1))',
      }"
    >
      <div
        class="absolute border border-dashed border-(--link) bg-(--bg-0)/60"
        :style="{
          inset: guide.widths.map((width) => `${width}px`).join(' '),
          borderWidth: 'calc(1px / var(--zoom, 1))',
        }"
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
          transform: 'translate(-50%, -50%) scale(calc(1 / var(--zoom, 1)))',
          display: width ? undefined : 'none',
        }"
        >{{ Math.round(width) }}</span
      >
    </div>
    <svg v-if="guide?.shape === 'anchor'" class="absolute inset-0 size-full overflow-visible">
      <line
        :x1="guide.start[0]"
        :y1="guide.start[1]"
        :x2="guide.end[0]"
        :y2="guide.end[1]"
        stroke="var(--link)"
        stroke-width="1.5"
        vector-effect="non-scaling-stroke"
      />
      <circle
        v-for="(point, ordinal) in [guide.start, guide.end]"
        :key="ordinal"
        :cx="point[0]"
        :cy="point[1]"
        :style="{ r: 'calc(4px / var(--zoom, 1))' }"
        :fill="ordinal ? 'var(--accent)' : 'var(--link)'"
        stroke="var(--bg-0)"
        stroke-width="2"
        vector-effect="non-scaling-stroke"
      />
    </svg>
    <span
      v-if="guide?.shape === 'anchor' && guide.text"
      class="absolute flex h-4.5 origin-top-left items-center rounded bg-(--link) px-1.5 font-mono text-11 leading-none whitespace-nowrap text-(--bg-0)"
      :style="{
        left: `${guide.end[0]}px`,
        top: `${guide.end[1]}px`,
        transform: 'translate(calc(8px / var(--zoom, 1)), calc(8px / var(--zoom, 1))) scale(calc(1 / var(--zoom, 1)))',
      }"
    >
      {{ guide.text }}
    </span>
  </div>
</template>
