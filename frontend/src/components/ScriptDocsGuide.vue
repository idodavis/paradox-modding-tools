<script setup lang="ts">
/**
 * How to make the game print its scripting documentation and what it gives PMT. The game writes
 * these files only from its own console, so this is the one part of the language engine the user
 * does by hand. Opened through useOverlay.
 */
const { folders } = defineProps<{ folders: string[] }>();
const emit = defineEmits<{ close: [] }>();

const steps = [
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
    body: folders[0]
      ? `The files land in ${folders[0]}; they are read when the workspace next opens.`
      : "The files are read from the game's logs or docs folder when the workspace next opens.",
  },
];

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
</script>

<template>
  <UModal
    title="Generate script_docs"
    description="The game can print its own scripting documentation. PMT reads it as the type system — it is never guessed."
    :ui="{ content: 'sm:max-w-2xl' }"
  >
    <template #body>
      <div class="flex flex-col gap-5 text-13">
        <ol class="space-y-3">
          <li v-for="(s, i) in steps" :key="s.title" class="flex gap-3">
            <span
              class="mt-0.5 flex size-5 shrink-0 items-center justify-center rounded-full bg-accented text-11 font-medium"
            >
              {{ i + 1 }}
            </span>
            <div class="min-w-0">
              <p class="font-medium text-highlighted">{{ s.title }}</p>
              <p class="text-toned break-words">{{ s.body }}</p>
            </div>
          </li>
        </ol>

        <div class="grid gap-4 sm:grid-cols-2">
          <div>
            <p class="mb-1 font-medium text-highlighted">With script_docs</p>
            <ul class="space-y-1 text-toned">
              <li v-for="g in gained" :key="g" class="flex gap-2">
                <UIcon name="i-lucide-check" class="mt-1 size-3.5 shrink-0 text-success" />
                <span>{{ g }}</span>
              </li>
            </ul>
          </div>
          <div>
            <p class="mb-1 font-medium text-highlighted">Works without it</p>
            <ul class="space-y-1 text-toned">
              <li v-for="k in kept" :key="k" class="flex gap-2">
                <UIcon name="i-lucide-dot" class="mt-1 size-3.5 shrink-0 text-dimmed" />
                <span>{{ k }}</span>
              </li>
            </ul>
          </div>
        </div>
      </div>
    </template>
    <template #footer>
      <span class="grow" />
      <UButton label="Close" @click="emit('close')" />
    </template>
  </UModal>
</template>
