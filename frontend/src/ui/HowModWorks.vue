<!--
  A dialog that shows, with one of the game's own definitions as the example, how the game reads a mod beside its own
  files: three columns, then three rules.
-->
<script setup lang="ts">
import { computed } from "vue";
import { useRouter } from "vue-router";
import { useQuery } from "@pinia/colada";

import { toSentence } from "../lib/format";
import { gamesQuery, precedenceQuery } from "../queries";
import { useWorkspaceStore } from "../stores/workspace";

const open = defineModel<boolean>("open", { required: true });
const { gameId, modTitle } = defineProps<{ gameId: string; modTitle: string }>();

const router = useRouter();
const workspace = useWorkspaceStore();
const { data: games } = useQuery(gamesQuery);
// with no game given, the first one is the example
const game = computed(() => games.value?.find((row) => row.id === gameId) ?? games.value?.[0]);
const example = computed(() => game.value?.example);
const { data: precedence } = useQuery(() => ({
  ...precedenceQuery({
    gameId: game.value?.id ?? "",
    database: example.value?.database ?? "",
    folder: example.value?.folder ?? "",
  }),
  enabled: open.value && !!example.value,
}));

const title = computed(() => modTitle.trim() || "Your mod");
// the mod changes the example's first name and adds one of its own, which the game uses from the mod
const newEntry = computed(() => `${title.value.toLowerCase().replace(/\W+/g, "_")}_new`);
const uses = computed(() => [
  ...(example.value?.names ?? []).map((entry, index) => ({ entry, modCopy: index === 0 })),
  { entry: newEntry.value, modCopy: true },
]);
const rules = computed(() => [
  "Mods load after the game, in the workspace's load order, top to bottom.",
  "A mod file at the same path as one of the game's replaces that whole file. Every entry in it is then the mod's to keep up to date when the game is patched.",
  `When two files define the same name, its kind decides which copy stays. ${precedence.value ?? ""}`,
]);

/** openCompatibility shows Health's compatibility for the open workspace and closes the dialog. */
function openCompatibility(): void {
  open.value = false;
  void router.push({ name: "health", params: { id: workspace.activeId() }, query: { view: "compatibility" } });
}
</script>

<template>
  <UModal
    v-model:open="open"
    title="How the game reads your mod"
    :description="`${game?.name ?? ''}, with your mods. ${toSentence(example?.database ?? '')} as the example.`"
    :ui="{ content: 'sm:max-w-4xl' }"
  >
    <template #body>
      <div class="flex flex-col gap-5">
        <div class="flex items-stretch gap-2.5">
          <div class="flex min-w-0 flex-1 flex-col gap-2.5 rounded-lg border border-default bg-muted px-3.5 py-3">
            <div class="flex items-center gap-1.5">
              <span class="size-2 shrink-0 rounded-full bg-(--game)" />
              <span class="text-13 font-semibold">The game's files</span>
              <span class="grow" />
              <UIcon name="i-lucide-lock" class="size-3.5 text-dimmed" />
            </div>
            <span class="font-mono text-12 text-dimmed">{{ example?.folder }}</span>
            <div class="flex flex-col items-start gap-1">
              <UBadge v-for="entry in example?.names ?? []" :key="entry" :label="entry" class="font-mono" />
            </div>
          </div>
          <UIcon name="i-lucide-arrow-right" class="size-5 shrink-0 self-center text-dimmed" />
          <div class="flex min-w-0 flex-1 flex-col gap-2.5 rounded-lg border border-default bg-muted px-3.5 py-3">
            <div class="flex items-center gap-1.5">
              <span class="size-2 shrink-0 rounded-full bg-(--o1)" />
              <span class="truncate text-13 font-semibold">{{ title }}</span>
              <span class="grow" />
              <span class="shrink-0 text-12 text-dimmed">loads after the game</span>
            </div>
            <span class="font-mono text-12 text-dimmed">{{ example?.folder }}</span>
            <div class="flex flex-col gap-1">
              <span class="flex items-center gap-2">
                <UBadge :label="`${example?.modes ? 'REPLACE:' : ''}${example?.names?.[0] ?? ''}`" class="font-mono" />
                <span class="text-12 text-dimmed">changed</span>
              </span>
              <span class="flex items-center gap-2">
                <UBadge :label="newEntry" class="font-mono" />
                <span class="text-12 text-dimmed">new</span>
              </span>
            </div>
          </div>
          <UIcon name="i-lucide-arrow-right" class="size-5 shrink-0 self-center text-dimmed" />
          <div class="flex min-w-0 flex-1 flex-col gap-2.5 rounded-lg border border-accented bg-elevated px-3.5 py-3">
            <span class="text-13 font-semibold">What the game uses</span>
            <span class="text-12 text-dimmed">One copy of each entry</span>
            <div class="flex flex-col gap-1">
              <span v-for="use in uses" :key="use.entry" class="flex min-w-0 items-center gap-2">
                <span class="w-32 shrink-0 truncate font-mono text-12">{{ use.entry }}</span>
                <UBadge :label="use.modCopy ? title : game?.name" />
              </span>
            </div>
          </div>
        </div>
        <ol class="flex flex-col gap-2.5">
          <li v-for="(rule, index) in rules" :key="index" class="flex items-start gap-2.5">
            <span
              class="mt-0.5 flex size-5 shrink-0 items-center justify-center rounded-full bg-accented text-11 font-medium"
            >
              {{ index + 1 }}
            </span>
            <span class="text-13 text-toned">{{ rule }}</span>
          </li>
        </ol>
      </div>
    </template>
    <template #footer="{ close }">
      <span class="grow text-12 text-dimmed">
        How the game picks a copy, for every kind, is at the top of Health's compatibility.
      </span>
      <UButton v-if="workspace.isOpen()" label="Open Health's compatibility" @click="openCompatibility" />
      <UButton label="Got it" color="primary" variant="solid" @click="close" />
    </template>
  </UModal>
</template>
