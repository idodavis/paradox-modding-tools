<!--
  The marks over the picture: the selected and hovered widgets, the widgets a state decides, what is missing, and the
  caret's property guide.
-->
<script setup lang="ts">
import { computed } from "vue";

import type { Window } from "@services/draw/models";

import { applyProperties, listProperties, TYPE_ICONS } from "./picture";

const props = defineProps<{
  rects: Map<number, DOMRect>;
  selected: number | null;
  hovered: number | null;
  /** The widgets whose condition the state under the pointer decides. */
  outlined: number[];
  window: Window;
  /** The key of the property on the caret's line, or "". */
  property: string;
  /** Whether the window lay wholly off the screen and is drawn at the screen's edge it passed. */
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
    const widget = id === null ? undefined : props.window.widgets?.[id];
    return rect ? [{ look, rect, widget }] : [];
  });
});

const outlines = computed(() => props.outlined.flatMap((id) => props.rects.get(id) ?? []));

const badges = computed(() =>
  (props.window.widgets ?? []).flatMap(({ id, properties }) => {
    const rect = props.rects.get(id);
    return rect && listProperties(props.window, properties).some(({ origin }) => origin === "missing") ? [rect] : [];
  }),
);

const guide = computed(() => {
  const widget = props.selected === null ? undefined : props.window.widgets?.[props.selected];
  const rect = props.selected === null ? undefined : props.rects.get(props.selected);
  const shape = GUIDES[props.property];
  if (!widget || !rect || !shape) return null;
  const style = document.createElement("div").style;
  const properties = listProperties(props.window, widget.properties);
  applyProperties(properties, style);
  if (shape === "margin") {
    return { shape, rect, widths: SIDES.map((side) => parseFloat(style.getPropertyValue(`--margin-${side}`)) || 0) };
  }
  const holder = props.rects.get(widget.parent) ?? rect;
  const at = (name: string, axis: string) => Number(style.getPropertyValue(`--${name}-${axis}`) || 0);
  const own = (axis: string) => Number(style.getPropertyValue(`--own-${axis}`) || at("parent", axis));
  return {
    shape,
    from: [holder.x + at("parent", "x") * holder.width, holder.y + at("parent", "y") * holder.height],
    to: [rect.x + own("x") * rect.width, rect.y + own("y") * rect.height],
    word: properties.findLast(({ key }) => key.toLowerCase() === props.property)?.value ?? "",
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
          <UIcon :name="TYPE_ICONS[mark.widget.type] ?? 'i-lucide-square-dashed'" class="size-3 shrink-0" />
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
        :x1="guide.from[0]"
        :y1="guide.from[1]"
        :x2="guide.to[0]"
        :y2="guide.to[1]"
        stroke="var(--link)"
        stroke-width="1.5"
        vector-effect="non-scaling-stroke"
      />
      <circle
        v-for="(point, ordinal) in [guide.from, guide.to]"
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
      v-if="guide?.shape === 'anchor' && guide.word"
      class="absolute flex h-4.5 origin-top-left items-center rounded bg-(--link) px-1.5 font-mono text-11 leading-none whitespace-nowrap text-(--bg-0)"
      :style="{
        left: `${guide.to[0]}px`,
        top: `${guide.to[1]}px`,
        transform: 'translate(calc(8px / var(--zoom, 1)), calc(8px / var(--zoom, 1))) scale(calc(1 / var(--zoom, 1)))',
      }"
    >
      {{ guide.word }}
    </span>
  </div>
</template>
