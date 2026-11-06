<!-- A dialog with the steps that make the given game write its own documentation, and what PMT gains from it. -->
<script setup lang="ts">
import { computed } from "vue";
import { useMutation, useQuery } from "@pinia/colada";

import { gamesQuery } from "../queries";
import * as workspace from "@services/workspace/service";

const open = defineModel<boolean>("open", { default: false });
const { game } = defineProps<{ game: string }>();

// A step's title writes what the user types between backticks.
const STEPS = [
  {
    title: "Launch the game from PMT",
    body: "PMT starts it with the debug switch the console needs.",
    launches: true,
  },
  {
    title: "Start or load a game",
    body: "The console opens in a game, not in the main menu.",
  },
  {
    title: "Open the console and type `script_docs`, then `dump_data_types`",
    body: "The console key is ` or ~, by keyboard layout. Each prints nothing; that is expected.",
  },
  {
    title: "Quit the game",
    body: "PMT sees the new files and offers to read them.",
  },
];

const COLUMNS = [
  {
    title: "With the game's documentation",
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
const { mutate: launch } = useMutation({ mutation: () => workspace.Launch() });
const name = computed(() => games.value?.find((row) => row.id === game)?.name ?? "the game");
</script>

<template>
  <UModal v-model:open="open" title="Get the game's own knowledge" :ui="{ content: 'sm:max-w-2xl' }">
    <template #body>
      <div class="flex flex-col gap-5 text-13">
        <p class="text-dimmed">
          {{ name }} writes a list of every effect, trigger and scope it has when asked. PMT reads it for completion,
          hover and the checks.
        </p>
        <ol class="space-y-3">
          <li v-for="(step, i) in STEPS" :key="step.title" class="flex gap-3">
            <span
              class="mt-0.5 flex size-5 shrink-0 items-center justify-center rounded-full bg-accented text-11 font-medium"
            >
              {{ i + 1 }}
            </span>
            <div class="min-w-0 grow">
              <p class="font-medium text-highlighted">
                <template v-for="(piece, at) in step.title.split('`')" :key="at">
                  <code
                    v-if="at % 2"
                    class="rounded-md border border-default bg-(--bg-0) px-1.5 py-0.5 font-mono text-[0.9em] text-(--syn-command)"
                    >{{ piece }}</code
                  >
                  <template v-else>{{ piece }}</template>
                </template>
              </p>
              <p class="break-words text-toned">{{ step.body }}</p>
            </div>
            <UButton
              v-if="step.launches"
              label="Launch"
              icon="i-lucide-play"
              color="primary"
              variant="solid"
              @click="launch()"
            />
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
