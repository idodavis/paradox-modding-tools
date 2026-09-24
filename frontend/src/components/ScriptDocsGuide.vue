<script setup lang="ts">
/**
 * How to make the game print its scripting documentation, what it gives PMT, what the last scan
 * read of it, and what the scan saw that nothing explained. The game writes these files only from
 * its own console, so this is the one part of the language engine the user does by hand.
 */
import { computed } from "vue";
import type { DumpHealth } from "@services/models";
import type { Drift } from "@services/internal/lang/schema";

const open = defineModel<boolean>("open", { required: true });

const props = defineProps<{
  dumps: DumpHealth[];
  drift: Drift | undefined;
  gameName?: string;
}>();

const steps = computed(() => [
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
    title: "Come back and rescan",
    body: props.dumps[0]?.folder
      ? `The files land in ${props.dumps[0].folder}; the rescan reads them from there.`
      : "The rescan reads the files from the game's logs or docs folder.",
  },
]);

const gained = [
  "Scope-aware completion — only the effects and triggers valid in the block you are editing",
  "Typed hover — what an object is to the engine, and what a token runs against",
  "Wrong-scope diagnostics",
  "The citable prefix table (culture:, title:, c:, …)",
];

const kept = [
  "Go to definition, find references, and rename",
  "Workspace Health: conflicts, load order, dangling references",
  "Localization coverage",
  "The Event Graph",
];

// what the scan saw and nothing explained, by what it was: a game update changing the language
const unexplained = computed(() =>
  Object.entries(props.drift ?? {})
    .filter(([, tallies]) => tallies?.length)
    .map(([what, tallies]) => ({ what: what.replace(/[A-Z]/g, " $&").toLowerCase(), tallies: tallies!.slice(0, 5) })),
);
</script>

<template>
  <UModal
    v-model:open="open"
    title="Generate script_docs"
    :description="`${gameName || 'The game'} can print its own scripting documentation. PMT reads it as the type system — it is never guessed.`"
    :ui="{ content: 'max-w-2xl' }"
  >
    <template #body>
      <div class="space-y-5 text-sm">
        <ol class="space-y-3">
          <li v-for="(s, i) in steps" :key="s.title" class="flex gap-3">
            <span
              class="mt-0.5 flex size-5 shrink-0 items-center justify-center rounded-full bg-elevated text-xs font-medium"
            >
              {{ i + 1 }}
            </span>
            <div class="min-w-0">
              <p class="font-medium text-highlighted">{{ s.title }}</p>
              <p class="text-muted break-words">{{ s.body }}</p>
            </div>
          </li>
        </ol>

        <div class="grid gap-4 sm:grid-cols-2">
          <div>
            <p class="mb-1 font-medium text-highlighted">With script_docs</p>
            <ul class="space-y-1 text-muted">
              <li v-for="g in gained" :key="g" class="flex gap-2">
                <UIcon name="i-lucide-check" class="mt-1 size-3.5 shrink-0 text-success" />
                <span>{{ g }}</span>
              </li>
            </ul>
          </div>
          <div>
            <p class="mb-1 font-medium text-highlighted">Works without it</p>
            <ul class="space-y-1 text-muted">
              <li v-for="k in kept" :key="k" class="flex gap-2">
                <UIcon name="i-lucide-dot" class="mt-1 size-3.5 shrink-0 text-muted" />
                <span>{{ k }}</span>
              </li>
            </ul>
          </div>
        </div>

        <p v-for="dump in dumps" :key="dump.folder" class="break-all text-xs text-muted">
          {{ dump.folder }}: {{ dump.effects.toLocaleString() }} effects, {{ dump.triggers.toLocaleString() }}
          triggers, {{ dump.links }} links, {{ dump.scopes }} scope types.
        </p>

        <div v-if="unexplained.length" class="space-y-1">
          <p class="font-medium text-highlighted">What the last scan could not explain</p>
          <p v-for="group in unexplained" :key="group.what" class="text-xs text-muted">
            {{ group.what }}:
            <span v-for="tally in group.tallies" :key="tally.name" class="mr-2 font-mono">
              {{ tally.name }} ×{{ tally.count }}
            </span>
          </p>
        </div>
      </div>
    </template>
    <template #footer="{ close }">
      <UButton label="Close" color="neutral" variant="outline" @click="close" />
    </template>
  </UModal>
</template>
