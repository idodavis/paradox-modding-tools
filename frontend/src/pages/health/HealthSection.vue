<!--
  One section of a finding's detail: a header with its count and its Open button over its content, and a hairline
  under it. A section that folds starts folded, and its header unfolds it.
-->
<script setup lang="ts">
import { shallowRef } from "vue";

import { describeCount } from "../../lib/format";

const {
  title,
  count = undefined,
  fold = false,
  onOpen = undefined,
} = defineProps<{
  title: string;
  /** How many things the section holds, shown after its title. Unset shows none. */
  count?: number;
  /** Whether the section starts folded, with a header that unfolds it. */
  fold?: boolean;
  /** Opens what the section shows in the IDE. Without it, the header has no Open button. */
  onOpen?: () => void;
}>();

/** Whether the content shows. */
const shown = shallowRef(!fold);
</script>

<template>
  <section class="flex flex-col gap-3 border-b border-default ps-5 pe-5.5 pt-4 pb-4.5 last:border-b-0">
    <div class="flex min-h-6 items-center gap-2 text-14 font-semibold whitespace-nowrap text-highlighted">
      <button v-if="fold" type="button" class="flex items-center gap-2" :aria-expanded="shown" @click="shown = !shown">
        <UIcon :name="shown ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'" class="size-3.5 text-dimmed" />
        {{ title }}
      </button>
      <template v-else>{{ title }}</template>
      <span v-if="count !== undefined" class="font-medium text-dimmed tabular-nums">{{ describeCount(count) }}</span>
      <span class="grow" />
      <slot name="actions" />
      <UButton v-if="onOpen" label="Open" icon="i-lucide-code" variant="ghost" size="xs" @click="onOpen" />
    </div>
    <slot v-if="shown" />
  </section>
</template>
