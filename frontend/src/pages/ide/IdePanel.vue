<script setup lang="ts">
/**
 * The IDE's panel (the Ide board's Problems, the IdeDiff board's Git output): the open files' problems by file,
 * filtered as typed, with Open in Health; and every git command source control ran, with what it wrote.
 */
import { computed, shallowRef, useTemplateRef, watch } from "vue";
import { useIdeRoots } from "../../composables/useIdeRoots";
import { useGitStore } from "../../stores/git";
import { useEditorStore } from "../../stores/editor";
import { useWorkspaceStore } from "../../stores/workspace";
import { Close } from "@services/windowservice";
import { isMain, useWindowsStore, windowName } from "../../stores/windows";
import OriginDot from "../../ui/OriginDot.vue";

const { workspaceId } = defineProps<{ workspaceId: string }>();

const editor = useEditorStore();
const windows = useWindowsStore();
const git = useGitStore();
const store = useWorkspaceStore();
const { locate } = useIdeRoots(() => workspaceId);

const filter = shallowRef("");
const files = computed(() => {
  const needle = filter.value.trim().toLowerCase();
  return [...editor.problems.entries()]
    .map(([path, found]) => {
      const at = locate(path);
      const rel = at?.rel ?? path;
      const shown = (found ?? []).filter(
        (one) => !needle || `${one.message} ${one.code} ${rel}`.toLowerCase().includes(needle),
      );
      return { path, rel, origin: at?.root.origin ?? "", found: shown };
    })
    .filter((file) => file.found.length);
});

const TABS = [
  { label: "Problems", value: "problems" },
  { label: "Git output", value: "git" },
];
const isTab = (value: string | number) => (value === "git" ? value : "problems");

// the Git output keeps its newest command in view
const gitLog = useTemplateRef("gitLog");
watch(
  () => [git.output.length, editor.panel],
  () => gitLog.value?.scrollTo({ top: gitLog.value.scrollHeight }),
  { flush: "post" },
);

function open(at: { path: string; line: number; col: number }): void {
  editor.open(at.path, { line: at.line, col: at.col });
}
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col bg-muted text-(length:--ide-ui-font)">
    <div class="flex h-8 shrink-0 items-center gap-0.5 border-b border-default ps-2 pe-1.5">
      <UTabs
        :model-value="isTab(editor.panel)"
        :items="TABS"
        :content="false"
        variant="link"
        size="sm"
        @update:model-value="editor.panel = isTab($event)"
      >
        <template #trailing="{ item }">
          <template v-if="item.value === 'problems'">
            <UBadge
              v-if="editor.problemCounts.errors"
              :label="String(editor.problemCounts.errors)"
              color="error"
              size="sm"
            />
            <UBadge
              v-if="editor.problemCounts.warnings"
              :label="String(editor.problemCounts.warnings)"
              color="warning"
              size="sm"
            />
          </template>
        </template>
      </UTabs>
      <span class="grow" />
      <template v-if="editor.panel === 'problems'">
        <UInput v-model="filter" placeholder="Filter problems" size="xs" class="w-50" icon="i-lucide-filter" />
        <UButton
          label="Open in Health"
          variant="ghost"
          size="xs"
          :to="{ name: 'health', params: { id: workspaceId }, query: { card: 'script' } }"
        />
      </template>
      <template v-if="isMain">
        <UTooltip text="Open in its own window">
          <UButton
            icon="i-lucide-app-window"
            variant="ghost"
            size="sm"
            square
            aria-label="Open in its own window"
            @click="windows.openWindow(`/workspace/${workspaceId}`, 'panel', 'Panel')"
          />
        </UTooltip>
        <UButton
          icon="i-lucide-x"
          variant="ghost"
          size="sm"
          square
          aria-label="Close the panel"
          @click="editor.panel = ''"
        />
      </template>
      <UTooltip v-else text="Back into the main window">
        <UButton
          icon="i-lucide-x"
          variant="ghost"
          size="sm"
          square
          aria-label="Back into the main window"
          @click="Close(windowName)"
        />
      </UTooltip>
    </div>
    <div
      v-if="editor.panel === 'git'"
      ref="gitLog"
      class="min-h-0 flex-1 overflow-y-auto px-3 py-1.5 font-mono text-11"
    >
      <div v-for="(line, i) in git.output" :key="i" class="pb-1.5">
        <div :class="line.failed ? 'text-error' : 'text-dimmed'">&gt; {{ line.command }}</div>
        <pre v-if="line.output" class="whitespace-pre-wrap text-toned">{{ line.output }}</pre>
      </div>
      <p v-if="!git.output.length" class="font-sans text-12 text-dimmed">
        The git commands source control runs show here.
      </p>
    </div>
    <div v-else class="min-h-0 flex-1 overflow-y-auto py-1">
      <template v-for="file in files" :key="file.path">
        <div class="flex h-6 items-center gap-1.5 px-3 text-12 font-medium whitespace-nowrap text-highlighted">
          <OriginDot :color="store.originColor(file.origin)" small />
          <span class="truncate">{{ file.rel }}</span>
          <span class="text-11 font-normal text-dimmed">{{ file.found.length }}</span>
        </div>
        <button
          v-for="(one, i) in file.found"
          :key="i"
          class="flex h-6 w-full items-center gap-2 ps-7.5 pe-3 text-start text-12 whitespace-nowrap text-toned hover:bg-accented"
          @click="open({ path: file.path, line: one.range.start.line, col: one.range.start.character })"
        >
          <UIcon
            :name="one.severity === 1 ? 'i-lucide-circle-x' : 'i-lucide-triangle-alert'"
            class="size-3.5 shrink-0"
            :class="one.severity === 1 ? 'text-error' : 'text-warning'"
          />
          <span class="min-w-0 grow truncate">{{ one.message }}</span>
          <span class="shrink-0 font-mono text-11 text-dimmed">{{ one.code }}</span>
          <span class="shrink-0 font-mono text-11 text-dimmed"
            >{{ one.range.start.line + 1 }}:{{ one.range.start.character + 1 }}</span
          >
        </button>
      </template>
      <p v-if="!files.length" class="px-3 py-2 text-12 text-dimmed">
        {{ filter ? "No problem matches the filter." : "No problems in the open files." }}
      </p>
    </div>
  </div>
</template>
