<script setup lang="ts">
/**
 * The head of a detail column: what the thing is as a kind badge in its color, the origin that holds it, its name in
 * mono and, where it is written, a link to the place. The column's own buttons end the first row, before a close
 * button that shows while a close is listened to.
 */
import { useWorkspaceStore } from "../stores/workspace";
import OriginChip from "./OriginChip.vue";
import SourceLink from "./SourceLink.vue";

const {
  kind,
  kindColor = "",
  kindIcon = "",
  origin,
  name,
  place = undefined,
  line = 0,
  onClose = undefined,
} = defineProps<{
  kind: string;
  /** A CSS color; the badge is a tint of it. */
  kindColor?: string;
  kindIcon?: string;
  /** An origin's id. */
  origin: string;
  name: string;
  place?: { path: string; rel: string };
  line?: number;
  onClose?: () => void;
}>();

const store = useWorkspaceStore();
</script>

<template>
  <div class="flex flex-col gap-1.5 border-b border-default px-4 pt-3.5 pb-3">
    <div class="flex flex-wrap items-center gap-2">
      <UBadge
        v-if="kind"
        :label="kind"
        :icon="kindIcon"
        size="sm"
        class="bg-current/16"
        :style="{ color: kindColor }"
      />
      <OriginChip :label="store.originName(origin)" :color="store.originColor(origin)" small />
      <span class="grow" />
      <slot name="actions" />
      <UButton
        v-if="onClose"
        icon="i-lucide-x"
        variant="ghost"
        size="sm"
        square
        aria-label="Close detail"
        @click="onClose"
      />
    </div>
    <span class="font-mono text-16 leading-5.5 font-semibold break-all text-highlighted">{{ name }}</span>
    <SourceLink v-if="place" :place="place" :line="line" class="self-start" />
  </div>
</template>
