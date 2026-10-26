<!-- Shows one problem as a row with its level's mark, its message and a second line. -->
<script setup lang="ts">
import { computed } from "vue";

import { LEVELS } from "../lib/format";

const {
  level = "",
  message,
  detail = "",
  check = "",
  mono = false,
  dense = false,
  selected = false,
  plain = false,
} = defineProps<{
  /** The problem's level. "" shows no mark. */
  level?: string;
  message: string;
  /** The second line, after the message when dense. */
  detail?: string;
  /** The name of the check that found it, shown as a chip. */
  check?: string;
  /** The second line is a path, in the mono font. */
  mono?: boolean;
  dense?: boolean;
  selected?: boolean;
  /** The row is plain text, not a button. */
  plain?: boolean;
}>();
const emit = defineEmits<{ open: [] }>();

const mark = computed(() => LEVELS[level]);
// a path shortens in its folders so its file name stays whole; any other second line is all "folder"
const folder = computed(() => (mono ? detail.slice(0, Math.max(detail.lastIndexOf("/"), 0)) : detail));
const file = computed(() => detail.slice(folder.value.length));
</script>

<template>
  <div
    :role="plain ? undefined : 'button'"
    :tabindex="plain ? undefined : 0"
    class="flex w-full text-start text-toned"
    :class="[
      dense ? 'h-6 items-center gap-2 px-3 text-12 whitespace-nowrap' : 'items-start gap-2.5 px-3 py-2',
      plain ? 'cursor-default' : 'cursor-pointer hover:bg-accented',
      { 'bg-(--accent-soft) shadow-[inset_2px_0_0_var(--accent)]': selected },
    ]"
    @click="emit('open')"
    @keydown.enter.self="emit('open')"
    @keydown.space.self.prevent="emit('open')"
  >
    <slot name="leading" />
    <UIcon
      v-if="mark"
      :name="mark.icon"
      class="size-3.5 shrink-0"
      :class="{ 'mt-0.75': !dense }"
      :style="{ color: `var(--ui-${mark.color})` }"
    />
    <span class="flex min-w-0 grow" :class="dense ? 'items-center gap-2' : 'flex-col'">
      <span :title="message" :class="dense ? 'min-w-0 truncate' : 'line-clamp-2 text-13 break-words text-highlighted'">
        {{ message }}
      </span>
      <span v-if="detail || check" class="flex min-w-0 items-center gap-1.5">
        <UBadge v-if="check" :label="check" color="neutral" variant="subtle" size="sm" class="shrink-0" />
        <span
          v-if="detail"
          :title="detail"
          class="flex min-w-0 text-dimmed"
          :class="mono ? 'font-mono text-11' : 'text-12'"
        >
          <span class="truncate">{{ folder }}</span>
          <span class="max-w-full shrink-0 truncate">{{ file }}</span>
        </span>
      </span>
    </span>
    <slot name="trailing" />
  </div>
</template>
