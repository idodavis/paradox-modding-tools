<!--
  The IDE panel's Terminal tab: the active shell's screen, with a find box over it, and the list of shells beside it,
  which can be resized and folded away. New shell starts the default shell, and the menu beside it any other. Showing
  the tab with no shell starts one.
-->
<script setup lang="ts">
import { computed, onMounted, shallowRef, useTemplateRef, watch } from "vue";
import { useMutation, useQuery } from "@pinia/colada";
import type { DropdownMenuItem, SplitterItem } from "@nuxt/ui";
import { useResizeObserver, useThrottleFn } from "@vueuse/core";

import type { Shell } from "@internal/terminal/models";
import * as app from "@services/app/service";
import { loadQuery, programsQuery, shellsQuery } from "../../../queries";
import { useTerminalStore } from "../../../stores/terminal";
import { useWorkspaceStore } from "../../../stores/workspace";
import FindBox from "../../../ui/FindBox.vue";

// the screen, and the list of shells in pixels, which folds to a rail of their icons past its least width, as the
// sidebar's does
const PANES: SplitterItem[] = [
  { slot: "screen" },
  {
    slot: "shells",
    sizeUnit: "px",
    defaultSize: 220,
    minSize: 160,
    maxSize: 480,
    collapsible: true,
    collapsedSize: 36,
  },
];
// each shell's icon by its program's name, VS Code's own; any other shell takes the plain terminal
const ICONS: Record<string, string> = {
  pwsh: "i-codicon-terminal-powershell",
  powershell: "i-codicon-terminal-powershell",
  bash: "i-codicon-terminal-bash",
  wsl: "i-codicon-terminal-linux",
  tmux: "i-codicon-terminal-tmux",
  cmd: "i-codicon-terminal-cmd",
};

const terminal = useTerminalStore();
const workspace = useWorkspaceStore();
const host = useTemplateRef("host");
const term = shallowRef("");
// the active shell's rows as text, read while the find box is open
const rows = shallowRef<string[]>([]);

const current = computed(() => terminal.consoles.find((open) => open.shell.id === terminal.active));
const hasFile = computed(() => !!terminal.findFile());
const { mutate: showPanel } = useMutation({ mutation: () => app.ShowWindow("panel") });
const { data: programs } = useQuery(programsQuery);
// the menu beside New shell: a shell of each program this system has
const programItems = computed<DropdownMenuItem[]>(() =>
  (programs.value ?? []).map((program) => ({ label: program.name, onSelect: () => terminal.start(program.path) })),
);
const readRows = useThrottleFn(() => {
  const buffer = current.value?.xterm.buffer.active;
  rows.value = buffer
    ? Array.from({ length: buffer.length }, (_, row) => buffer.getLine(row)?.translateToString(true) ?? "")
    : [];
}, 200);

onMounted(async () => {
  if (terminal.holding && !(await loadQuery(shellsQuery))?.length) terminal.start("");
});
watch(
  [() => terminal.active, host, () => terminal.consoles.length],
  () => {
    if (host.value && terminal.active) terminal.show(terminal.active, host.value);
  },
  { flush: "post" },
);
const fitShell = useThrottleFn(() => {
  if (current.value?.xterm.element) current.value.fit.fit();
}, 50);
useResizeObserver(host, () => void fitShell());
// while the find box is open, its rows follow the shell's output
watch(
  [() => terminal.findOpen, current],
  ([findOpen, open], _, onCleanup) => {
    if (!findOpen || !open) return;
    void readRows();
    const listener = open.xterm.onWriteParsed(() => void readRows());
    onCleanup(() => listener.dispose());
  },
  { immediate: true },
);

/** describeShell names where a shell started: its mod, else its folder. */
function describeShell(shell: Shell): string {
  return workspace.originName(shell.origin) || shell.folder;
}

/** showHit scrolls the active shell to a row the find box hit, and selects the term in it. */
function showHit(row: number): void {
  const xterm = current.value?.xterm;
  const needle = term.value.trim().toLowerCase();
  if (!xterm) return;
  xterm.scrollToLine(row);
  xterm.select(Math.max((rows.value[row] ?? "").toLowerCase().indexOf(needle), 0), row, needle.length);
}

/** closeFind closes the find box and gives the shell back the keys. */
function closeFind(): void {
  terminal.findOpen = false;
  term.value = "";
  current.value?.xterm.clearSelection();
  current.value?.xterm.focus();
}
</script>

<template>
  <div v-if="!terminal.holding" class="flex flex-col gap-2 px-3 py-2 text-12 text-dimmed">
    The shells are in the panel's own window.
    <div><UButton label="Show window" size="xs" variant="ghost" @click="showPanel()" /></div>
  </div>
  <USplitter v-else :items="PANES" auto-save-id="pmt.terminal.shells" class="min-h-0 min-w-0 flex-1">
    <template #screen>
      <Teleport defer to="#panel-actions">
        <UButton
          label="Send the file to the shell"
          icon="i-lucide-at-sign"
          variant="ghost"
          size="xs"
          :disabled="!current || !hasFile"
          @click="terminal.sendFile()"
        />
      </Teleport>
      <div class="relative size-full min-h-0 min-w-0 bg-(--bg-0)">
        <!-- the padding sits outside the terminal's element, which the fit measures -->
        <div class="absolute inset-0 py-2 ps-3.5">
          <div ref="host" class="size-full" />
        </div>
        <FindBox
          v-if="terminal.findOpen"
          v-model:term="term"
          :texts="rows"
          autofocus
          class="absolute end-3 top-2 z-10 w-72"
          @hit="showHit"
          @close="closeFind"
        />
        <p v-if="!terminal.consoles.length" class="absolute inset-0 px-3.5 py-2 text-12 text-dimmed">
          No shell is running.
        </p>
      </div>
    </template>
    <template #shells="{ collapsed }">
      <div
        class="flex size-full min-w-0 flex-col gap-1 overflow-y-auto border-s border-default"
        :class="collapsed ? 'items-center px-1 py-2' : 'p-2'"
      >
        <div
          v-for="open in terminal.consoles"
          :key="open.shell.id"
          role="button"
          class="group flex min-h-7 shrink-0 cursor-pointer items-center gap-1.5 rounded-md text-12"
          :class="[
            collapsed ? 'size-7 justify-center' : 'py-1 ps-2.5 pe-1',
            open.shell.id === terminal.active
              ? 'bg-elevated text-highlighted ring ring-accented ring-inset'
              : 'text-dimmed hover:text-toned',
          ]"
          :title="collapsed ? `${open.shell.name}, ${describeShell(open.shell)}` : open.shell.folder"
          @click="terminal.active = open.shell.id"
        >
          <UIcon :name="ICONS[open.shell.name] ?? 'i-codicon-terminal'" class="size-3.5 shrink-0" />
          <template v-if="!collapsed">
            <span class="flex min-w-0 grow flex-col">
              <span class="truncate">{{ open.shell.name }}</span>
              <span class="truncate text-11 text-dimmed">{{ describeShell(open.shell) }}</span>
            </span>
            <UButton
              icon="i-lucide-x"
              variant="ghost"
              size="xs"
              square
              aria-label="Close the shell"
              :class="open.shell.id !== terminal.active && 'invisible group-hover:visible'"
              @click.stop="terminal.close(open.shell.id)"
            />
          </template>
        </div>
        <UButton
          v-if="collapsed"
          icon="i-lucide-plus"
          variant="ghost"
          color="neutral"
          size="sm"
          square
          aria-label="New shell"
          @click="terminal.start('')"
        />
        <UFieldGroup v-else size="sm" class="w-full">
          <UButton
            label="New shell"
            icon="i-lucide-plus"
            variant="ghost"
            color="neutral"
            class="grow justify-start"
            @click="terminal.start('')"
          />
          <UDropdownMenu :items="programItems" :content="{ align: 'end' }">
            <UButton icon="i-lucide-chevron-down" variant="ghost" color="neutral" square aria-label="Choose a shell" />
          </UDropdownMenu>
        </UFieldGroup>
      </div>
    </template>
  </USplitter>
</template>
