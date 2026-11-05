<!-- The IDE panel's Terminal tab: the active shell's screen with a find box, and the list of shells beside it. -->
<script setup lang="ts">
import { computed, onMounted, shallowRef, useTemplateRef, watch } from "vue";
import { useQuery } from "@pinia/colada";
import type { DropdownMenuItem, SplitterItem } from "@nuxt/ui";
import { useResizeObserver, useThrottleFn } from "@vueuse/core";

import type { Shell } from "@internal/terminal/models";
import { loadQuery, programsQuery, shellsQuery } from "../../../queries";
import { useTerminalStore } from "../../../stores/terminal";
import { useWindowsStore } from "../../../stores/windows";
import { useWorkspaceStore } from "../../../stores/workspace";
import FindBox from "../../../ui/FindBox.vue";

// The shell list collapses to a rail of icons below its minimum width, as the sidebar does.
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
// VS Code's icon for each shell program by name. Any other program gets the plain terminal icon.
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
// The active shell's rows as text, read while the find box is open.
const rows = shallowRef<string[]>([]);

const current = computed(() => terminal.consoles.find((session) => session.shell.id === terminal.active));
const hasFile = computed(() => !!terminal.findFile());
const windows = useWindowsStore();
const { data: programs } = useQuery(programsQuery);
const programItems = computed<DropdownMenuItem[]>(() =>
  (programs.value ?? []).map((program) => ({ label: program.name, onSelect: () => terminal.start(program.path) })),
);
const loadRows = useThrottleFn(() => {
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
  if (current.value) terminal.fitConsole(current.value);
}, 50);
useResizeObserver(host, () => void fitShell());
watch(
  [() => terminal.findOpen, current],
  ([findOpen, session], _, onCleanup) => {
    if (!findOpen || !session) return;
    void loadRows();
    const listener = session.xterm.onWriteParsed(() => void loadRows());
    onCleanup(() => listener.dispose());
  },
  { immediate: true },
);

/** describeShell returns where a shell started: its mod's name, else its folder. */
function describeShell(shell: Shell): string {
  return workspace.originName(shell.origin) || shell.folder;
}

/** showHit scrolls the active shell to the given row and selects the find term in it. */
function showHit(row: number): void {
  const xterm = current.value?.xterm;
  const needle = term.value.trim().toLowerCase();
  if (!xterm) return;
  xterm.scrollToLine(row);
  xterm.select(Math.max((rows.value[row] ?? "").toLowerCase().indexOf(needle), 0), row, needle.length);
}

/** closeFind closes the find box and returns the focus to the shell. */
function closeFind(): void {
  terminal.findOpen = false;
  term.value = "";
  current.value?.xterm.clearSelection();
  current.value?.xterm.focus();
}
</script>

<template>
  <div v-if="!terminal.holding" class="flex flex-col gap-2 px-3 py-2 text-12 text-dimmed">
    The shells are in the terminal's own window.
    <div><UButton label="Show window" size="xs" variant="ghost" @click="windows.showWindow('terminal')" /></div>
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
        <!-- The padding stays outside the terminal's element, which the fit measures. -->
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
          v-for="session in terminal.consoles"
          :key="session.shell.id"
          role="button"
          class="group flex min-h-7 shrink-0 cursor-pointer items-center gap-1.5 rounded-md text-12"
          :class="[
            collapsed ? 'size-7 justify-center' : 'py-1 ps-2.5 pe-1',
            session.shell.id === terminal.active
              ? 'bg-elevated text-highlighted ring ring-accented ring-inset'
              : 'text-dimmed hover:text-toned',
          ]"
          :title="collapsed ? `${session.shell.name}, ${describeShell(session.shell)}` : session.shell.folder"
          @click="terminal.active = session.shell.id"
        >
          <UIcon :name="ICONS[session.shell.name] ?? 'i-codicon-terminal'" class="size-3.5 shrink-0" />
          <template v-if="!collapsed">
            <span class="flex min-w-0 grow flex-col">
              <span class="truncate">{{ session.shell.name }}</span>
              <span class="truncate text-11 text-dimmed">{{ describeShell(session.shell) }}</span>
            </span>
            <UButton
              icon="i-lucide-x"
              variant="ghost"
              size="xs"
              square
              aria-label="Close the shell"
              :class="session.shell.id !== terminal.active && 'invisible group-hover:visible'"
              @click.stop="terminal.close(session.shell.id)"
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
