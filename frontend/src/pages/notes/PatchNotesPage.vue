<!-- The Patch Notes page: the versions, with a dot where a note has a Modding section, the note itself, and find. -->
<script setup lang="ts">
import { computed, shallowRef, watch } from "vue";
import { useQuery } from "@pinia/colada";
import { Browser } from "@wailsio/runtime";
import { day } from "../../lib/format";
import { noteQuery, notesQuery, stateQuery } from "../../queries";
import { makeChip, useChip } from "../../stores/agent";
import { useWorkspaceStore } from "../../stores/workspace";
import StatusFacts from "../../ui/StatusFacts.vue";
import WikiArticle from "../../ui/WikiArticle.vue";

const {
  id,
  patch = "",
  find = "",
} = defineProps<{
  id: string;
  /** The version whose notes are shown, as the route names it; the newest when it names none. */
  patch?: string;
  /** Text to find in the notes shown. */
  find?: string;
}>();

const PANES = [
  { slot: "versions", defaultSize: 248, minSize: 180, maxSize: 400, sizeUnit: "px" as const },
  { slot: "notes" },
];

const workspace = useWorkspaceStore();
const { data: state } = useQuery(stateQuery);
const { data: notes } = useQuery(() => ({ ...notesQuery, enabled: workspace.isOpen() }));

// the version shown is the one chosen or the route names, else the newest, which Go lists first
const choice = shallowRef<string>();
watch(
  () => id,
  () => (choice.value = undefined),
);
watch(
  () => patch,
  (value) => (choice.value = value || choice.value),
  { immediate: true },
);
const version = computed({
  get: () => choice.value ?? notes.value?.[0]?.version ?? "",
  set: (next: string | undefined) => (choice.value = next),
});
const note = computed(() => notes.value?.find((one) => one.version === version.value));
const { data: page, isLoading } = useQuery(() => ({
  ...noteQuery(version.value),
  enabled: workspace.isOpen() && !!version.value,
}));
const facts = computed(() => [
  { label: "Patch notes from the", value: `${state.value?.game?.name ?? ""} wiki` },
  ...(page.value ? [{ label: "Cached", value: day(page.value.fetchedAt) }] : []),
]);
useChip(() =>
  version.value
    ? makeChip(version.value, `The ${state.value?.game?.name ?? ""} patch notes of ${version.value}`)
    : null,
);
</script>

<template>
  <div class="flex h-full min-h-0 overflow-hidden">
    <p v-if="!notes?.length" class="p-6 text-13 text-dimmed">
      No patch notes yet. PMT reads them from the {{ state?.game?.name }} wiki when it starts, the status bar showing
      how far it is, and they show here once read.
    </p>
    <USplitter v-else :items="PANES" auto-save-id="pmt.patchNotes" class="min-w-0 flex-1">
      <template #versions>
        <aside class="flex min-w-0 flex-1 flex-col border-e border-default bg-muted">
          <div class="flex h-11 shrink-0 items-center px-3 text-12 font-semibold">
            Versions<span class="grow" /><span class="text-11 font-normal text-dimmed">{{ notes.length }}</span>
          </div>
          <UListbox
            v-model="version"
            :items="notes.map((one) => ({ ...one, class: one.parent ? 'h-6.5 ps-5.5 text-12' : undefined }))"
            value-key="version"
            :filter="{ placeholder: 'Filter versions', icon: 'i-lucide-search', size: 'sm', variant: 'outline' }"
            class="min-h-0 flex-1"
            :ui="{
              root: 'gap-0 border-0',
              input: 'mx-2.5 mb-1 border-0',
              content: 'max-h-none divide-y-0 px-1.5',
              item: [
                'h-7.5 items-center rounded-md px-2.5 text-13 text-muted before:rounded-md',
                'data-[state=checked]:border-s-2 data-[state=checked]:border-primary data-[state=checked]:before:bg-(--accent-soft)',
              ],
              itemTrailingIcon: 'hidden',
            }"
          >
            <template #item-label="{ item }">
              <span v-if="item.parent" class="font-mono text-11 text-muted">{{ item.version }}</span>
              <span v-else class="flex min-w-0 flex-1 items-center gap-2">
                <span class="shrink-0 font-mono text-12 font-medium text-highlighted">{{ item.version }}</span>
                <span class="ms-auto min-w-0 truncate text-11 text-dimmed">{{ item.title }}</span>
              </span>
            </template>
            <template #item-trailing="{ item }">
              <UBadge v-if="item.version === notes[0]?.version" label="Latest" color="success" size="sm" />
              <span v-if="item.modding" class="size-1.5 shrink-0 rounded-full bg-(--link)" />
            </template>
          </UListbox>
          <div class="flex items-center gap-1.5 border-t border-default px-3 py-2 text-11 text-dimmed">
            <span class="size-1.5 shrink-0 rounded-full bg-(--link)" />Has a modding section
          </div>
        </aside>
      </template>
      <template #notes>
        <section class="flex min-w-0 grow flex-col bg-(--bg-0)">
          <div class="flex h-13 shrink-0 items-center gap-3 border-b border-default ps-8 pe-6">
            <h1 class="truncate text-16">{{ page?.title ?? note?.title ?? version }}</h1>
            <span v-if="note?.released" class="shrink-0 text-12 text-dimmed">released {{ note.released }}</span>
            <span class="grow" />
            <UButton
              v-if="page?.url"
              label="Open on the wiki"
              icon="i-lucide-external-link"
              variant="ghost"
              @click="Browser.OpenURL(page.url)"
            />
          </div>
          <WikiArticle v-if="page" :page="page" :find="find" class="min-h-0 flex-1" />
          <p v-else-if="!isLoading" class="px-8 pt-4 text-13 text-dimmed">No notes for this version.</p>
        </section>
      </template>
    </USplitter>
    <StatusFacts :facts="facts" />
  </div>
</template>
