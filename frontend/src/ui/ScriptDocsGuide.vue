<!-- A dialog with the steps that make the given game print its script_docs, and what PMT gains from them. -->
<script setup lang="ts">
import { computed } from "vue";
import { useQuery } from "@pinia/colada";

import { gamesQuery } from "../queries";

const open = defineModel<boolean>("open", { default: false });
const { game } = defineProps<{ game: string }>();

const STEPS = [
  {
    title: "Add the -debug_mode launch option",
    body: "In Steam: right-click the game, Properties, Launch Options. In the Paradox launcher there is no equivalent, so use Steam.",
  },
  {
    title: "Start the game and open a save or a new game",
    body: "The console is only available in-game, not from the main menu.",
  },
  {
    title: "Open the console with ` or ~",
    body: "The key depends on your keyboard layout; both are worth trying.",
  },
  {
    title: "Run script_docs",
    body: "It runs instantly and prints nothing. That is expected.",
  },
  {
    title: "Come back and reopen the workspace",
    body: "The files are read from the game's logs or docs folder when the workspace next opens.",
  },
];

const COLUMNS = [
  {
    title: "With script_docs",
    icon: "i-lucide-check",
    color: "text-success",
    lines: [
      "Scope-aware completion: only the effects and triggers valid in the block you are editing",
      "Typed hover: what an object is to the engine, and what a token runs against",
      "Wrong-scope diagnostics",
      "The citable prefix table (culture:, title:, c:, …)",
    ],
  },
  {
    title: "Works without it",
    icon: "i-lucide-dot",
    color: "text-dimmed",
    lines: [
      "Go to definition, find references, and rename",
      "Workspace Health: conflicts, load order, dangling references",
      "Localization coverage",
      "The Event Graph",
    ],
  },
];

const { data: games } = useQuery(gamesQuery);
const name = computed(() => games.value?.find((row) => row?.id === game)?.name ?? "the game");
</script>

<template>
  <UModal v-model:open="open" title="Generate script_docs" :ui="{ content: 'sm:max-w-2xl' }">
    <template #body>
      <div class="flex flex-col gap-5 text-13">
        <p class="text-dimmed">
          {{ name }} can print its own scripting documentation. PMT reads it as the type system, and never guesses it.
        </p>
        <ol class="space-y-3">
          <li v-for="(step, i) in STEPS" :key="step.title" class="flex gap-3">
            <span
              class="mt-0.5 flex size-5 shrink-0 items-center justify-center rounded-full bg-accented text-11 font-medium"
            >
              {{ i + 1 }}
            </span>
            <div class="min-w-0">
              <p class="font-medium text-highlighted">{{ step.title }}</p>
              <p class="break-words text-toned">{{ step.body }}</p>
            </div>
          </li>
        </ol>
        <div class="grid gap-4 sm:grid-cols-2">
          <div v-for="column in COLUMNS" :key="column.title">
            <p class="mb-1 font-medium text-highlighted">{{ column.title }}</p>
            <ul class="space-y-1 text-toned">
              <li v-for="line in column.lines" :key="line" class="flex gap-2">
                <UIcon :name="column.icon" class="mt-1 size-3.5 shrink-0" :class="column.color" />
                <span>{{ line }}</span>
              </li>
            </ul>
          </div>
        </div>
      </div>
    </template>
    <template #footer="{ close }">
      <span class="grow" />
      <UButton label="Close" @click="close" />
    </template>
  </UModal>
</template>
