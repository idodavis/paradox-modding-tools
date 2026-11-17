<!--
  The game's keys of one lexicon class, with the class tabs, a filter and a
  letter rail. A row opens to the key's card, and says what the patches of a
  range of versions did to it.
-->
<script setup lang="ts">
import { computed, shallowRef, useTemplateRef, watch } from "vue";
import { useQuery } from "@pinia/colada";

import { Status } from "@internal/schema/models";
import type { Class } from "@services/lsp/models";

import { describeCount, MARKS, matchQuery } from "../lib/format";
import { lexiconKeyQuery, lexiconQuery } from "../queries";
import { useWorkspaceStore } from "../stores/workspace";
import HoverCard from "./HoverCard.vue";
import ScriptSnippet from "./ScriptSnippet.vue";
import SourceLink from "./SourceLink.vue";

const {
  classes,
  start = "",
  end = "",
} = defineProps<{
  classes: Class[];
  /**
   * The versions the list is of, so that it holds only the rows they changed.
   * With neither, it holds every row, unmarked.
   */
  start?: string;
  end?: string;
}>();
const kind = defineModel<string>("kind", { required: true });
const filter = defineModel<string>("filter", { default: "" });

const workspace = useWorkspaceStore();
const open = shallowRef("");
watch(kind, () => (open.value = ""));

const { data: rows, isPending } = useQuery(() => ({
  ...lexiconQuery({ kind: kind.value, start, end }),
  enabled: workspace.isOpen(),
}));
const visible = computed(() => (rows.value ?? []).filter((row) => matchQuery(filter.value, row.name, row.blurb)));
const label = computed(() => classes.find((own) => own.id === kind.value)?.label.toLowerCase() ?? "");
const opened = computed(() => visible.value.find((row) => row.name === open.value));
// A scripted effect or trigger is no key of the dumps, so it has no card.
const { data: card } = useQuery(() => ({
  ...lexiconKeyQuery({ kind: kind.value, name: open.value }),
  enabled: workspace.isOpen() && !!opened.value && !opened.value.defined,
}));

// The letter rail: each first letter with the first row it begins.
type LetterStart = { letter: string; index: number };
const list = useTemplateRef("list");
const letters = computed(() =>
  visible.value.reduce<LetterStart[]>((starts, row, index) => {
    const letter = initial(row.name);
    return starts.at(-1)?.letter === letter ? starts : [...starts, { letter, index }];
  }, []),
);
const topLetter = shallowRef("");

function initial(text: string): string {
  return text.charAt(0).toUpperCase();
}

/**
 * markLetter marks the first letter of the first row in view. At the end of
 * the list the letter last jumped to stays marked while its first row is in
 * view, since the list cannot bring it to the top.
 */
function markLetter(): void {
  const virtualizer = list.value?.virtualizer;
  const top = virtualizer?.scrollOffset ?? 0;
  const first = virtualizer?.getVirtualItems().find((item) => item.end > top)?.index ?? 0;
  const atEnd = top + (virtualizer?.scrollRect?.height ?? 0) >= (virtualizer?.getTotalSize() ?? 0) - 1;
  const jumped = letters.value.find((entry) => entry.letter === topLetter.value);
  if (atEnd && jumped && jumped.index >= first) return;

  topLetter.value = initial(visible.value[first]?.name ?? "");
}

function jump(entry: LetterStart): void {
  topLetter.value = entry.letter;
  list.value?.virtualizer?.scrollToIndex(entry.index, { align: "start" });
}

/** estimateSize returns the height a row is drawn at before it is measured. */
function estimateSize(index: number): number {
  const row = visible.value[index];
  if (row?.name === open.value) return 320;
  return row?.blurb ? 48 : 30;
}
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div v-if="classes.length > 1" class="shrink-0 px-3 pt-2">
      <UTabs
        :model-value="kind"
        :items="classes"
        value-key="id"
        :content="false"
        size="xs"
        @update:model-value="kind = String($event)"
      />
    </div>
    <div class="flex shrink-0 items-center gap-2 border-b border-default px-3 py-2">
      <UInput
        v-model="filter"
        placeholder="Filter the lexicon"
        size="xs"
        icon="i-lucide-filter"
        class="min-w-0 flex-1"
      />
      <slot />
    </div>
    <div v-if="visible.length" class="flex min-h-0 flex-1">
      <UScrollArea
        ref="list"
        v-slot="{ item }"
        :items="visible"
        :virtualize="{ estimateSize }"
        class="min-h-0 flex-1"
        @scroll="markLetter"
      >
        <div
          class="flex flex-col border-b border-default"
          :class="item.name === open && 'm-2 rounded-lg border bg-elevated shadow-md'"
        >
          <button
            type="button"
            class="flex w-full flex-col gap-0.5 px-3.5 py-1.75 text-start"
            :aria-expanded="item.name === open"
            @click="open = item.name === open ? '' : item.name"
          >
            <span class="flex min-w-0 items-center gap-2">
              <span
                class="truncate font-mono text-13 font-medium text-highlighted"
                :class="item.status === Status.StatusRemoved && 'text-dimmed line-through decoration-error'"
                >{{ item.name }}</span
              >
              <span
                v-if="item.status"
                class="inline-flex shrink-0 items-center gap-1.5 text-11 font-medium capitalize"
                :class="MARKS[item.status]?.color"
              >
                <span class="size-1.5 rounded-full bg-current" />
                {{ MARKS[item.status]?.count }}
                <span class="font-mono font-normal text-dimmed">{{ item.version }}</span>
              </span>
            </span>
            <span v-if="item.blurb && item.name !== open" class="truncate text-12 text-dimmed">{{ item.blurb }}</span>
          </button>
          <template v-if="item.name === open">
            <HoverCard v-if="card" :card="card" class="border-t border-default" />
            <div v-if="item.defined || item.usage" class="flex flex-col gap-2 border-t border-default px-3.5 py-2.5">
              <SourceLink v-if="item.defined" :place="item.defined.place" :line="item.defined.line" class="text-11" />
              <ScriptSnippet
                v-if="item.usage"
                :text="item.usage"
                class="rounded-md border border-default px-2.5 pb-1.5"
              />
            </div>
          </template>
        </div>
      </UScrollArea>
      <nav class="flex w-6 shrink-0 flex-col items-center overflow-y-auto border-s border-default py-1 text-10">
        <button
          v-for="entry in letters"
          :key="entry.letter"
          type="button"
          class="w-full shrink-0 rounded-sm py-0.5 text-center font-medium"
          :class="entry.letter === topLetter ? 'bg-(--accent-soft) text-highlighted' : 'text-dimmed hover:text-toned'"
          :aria-label="`Rows beginning with ${entry.letter}`"
          @click="jump(entry)"
        >
          {{ entry.letter }}
        </button>
      </nav>
    </div>
    <p v-else-if="isPending" class="flex-1 px-3.5 py-3 text-12 text-dimmed">Reading the game's dumps…</p>
    <UEmpty
      v-else
      icon="i-lucide-book-a"
      :title="start && !rows?.length ? `No ${label} changed` : 'Nothing in the lexicon matches'"
      size="sm"
      class="m-auto"
    />
    <div v-if="start" class="flex shrink-0 items-center gap-3 border-t border-default px-3 py-1.5 text-11 text-dimmed">
      <span>Since {{ start }}, {{ describeCount(rows?.length ?? 0) }} of the {{ label }} changed</span>
      <span class="grow" />
      <span
        v-for="mark in Object.values(MARKS)"
        :key="mark.count"
        class="inline-flex items-center gap-1.5 font-medium capitalize"
        :class="mark.color"
      >
        <span class="size-1.5 rounded-full bg-current" />{{ mark.count }}
      </span>
    </div>
  </div>
</template>
