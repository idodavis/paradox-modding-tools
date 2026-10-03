<script setup lang="ts">
/**
 * How to make the game print its scripting documentation, what it gives PMT, what the last scan
 * read of it, and what the scan saw that nothing explained. The game writes these files only from
 * its own console, so this is the one part of the language engine the user does by hand.
 */
import { computed } from "vue";

const open = defineModel<boolean>("open", { required: true });

const props = defineProps<{
  folders: string[];
  unread: string[];
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
    body: props.folders[0]
      ? `The files land in ${props.folders[0]}; the rescan reads them from there.`
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

// what the scan saw and nothing explained, a few lines of it: a game update changing the language
const unexplained = computed(() => props.unread.slice(0, 5));
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

        <div v-if="unexplained.length" class="space-y-1">
          <p class="font-medium text-highlighted">What the last scan could not explain</p>
          <p v-for="(line, i) in unexplained" :key="i" class="break-all font-mono text-xs text-muted">{{ line }}</p>
          <p v-if="unread.length > unexplained.length" class="text-xs text-muted">
            and {{ (unread.length - unexplained.length).toLocaleString() }} more lines
          </p>
        </div>
      </div>
    </template>
    <template #footer="{ close }">
      <UButton label="Close" color="neutral" variant="outline" @click="close" />
    </template>
  </UModal>
</template>
