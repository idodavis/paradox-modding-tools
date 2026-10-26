<!--
  One place a finding points at, as a card with its file, its line and the code there. A copy among several also says
  why the game keeps it and whether the game uses it.
-->
<script lang="ts">
import type { FindingSite } from "@services/health/models";

import { useEditorStore } from "../../stores/editor";

/** openSite opens a site in the editor at the start of its range. */
export function openSite(site: FindingSite): void {
  useEditorStore().show(site.place, { lineNumber: site.range.startLineNumber, column: site.range.startColumn });
}
</script>

<script setup lang="ts">
import { computed } from "vue";
import type { Snippet } from "@services/health/models";
import { REASONS, describeCount } from "../../lib/format";
import { useWorkspaceStore } from "../../stores/workspace";
import ScriptSnippet, { EDITOR_LANGUAGES } from "../../ui/ScriptSnippet.vue";

const {
  site,
  snippet = undefined,
  used,
} = defineProps<{
  site: FindingSite;
  /** The code around the site. */
  snippet?: Snippet;
  /** Whether the game uses this copy, or unset if no copy is kept. */
  used?: boolean;
}>();

const workspace = useWorkspaceStore();
/** The file and line in pieces ending at each slash, so the path wraps only there. */
const location = computed(() => `${site.place.rel}:${site.range.startLineNumber}`.split(/(?<=\/)/));
/** Whether the site is one copy among several. An undecided contest's copies have a reason, though none is kept. */
const isCopy = computed(() => used !== undefined || !!site.reason);
/** The problem's own lines to highlight. A copy shows a whole definition and highlights none. */
const focus = computed((): [number, number] | undefined =>
  isCopy.value ? undefined : [site.range.startLineNumber, site.range.endLineNumber],
);
</script>

<template>
  <div
    class="shrink-0 overflow-hidden rounded-lg border border-default bg-(--bg-0)"
    :class="{ 'opacity-70': used === false }"
  >
    <div class="flex flex-col gap-0.5 border-b border-default bg-elevated px-2.5 py-1.5 text-12">
      <div v-if="isCopy" class="flex items-center gap-2">
        <span
          class="size-1.5 shrink-0 rounded-full"
          :style="{ background: workspace.originColor(site.place.origin) }"
        />
        <span class="min-w-0 truncate font-medium text-highlighted" :title="workspace.originName(site.place.origin)">
          {{ workspace.originName(site.place.origin) }}
        </span>
        <span class="grow" />
        <UBadge v-if="site.reason" :label="REASONS[site.reason] ?? site.reason" variant="outline" size="sm" />
        <UBadge
          v-if="used !== undefined"
          :label="used ? 'Used' : 'Not used'"
          :color="used ? 'success' : 'neutral'"
          size="sm"
        />
      </div>
      <button
        type="button"
        class="flex items-start gap-2 text-start hover:underline"
        :title="`Open ${site.place.rel} in the IDE`"
        @click="openSite(site)"
      >
        <span
          v-if="!isCopy"
          class="mt-1.5 size-1.5 shrink-0 rounded-full"
          :style="{ background: workspace.originColor(site.place.origin) }"
        />
        <!-- a piece wider than the card still breaks inside -->
        <span class="font-mono text-11 wrap-anywhere text-dimmed">
          <template v-for="(piece, index) in location" :key="index">{{ piece }}<wbr /></template>
        </span>
      </button>
    </div>
    <div v-if="site.defines?.length" class="flex flex-col gap-1.5 px-2.5 py-2">
      <span class="text-11 text-dimmed">
        Defines {{ describeCount(site.defines.length) }}{{ site.removed?.length ? ";" : "" }}
        <span v-if="site.removed?.length" class="text-warning">
          the kept file leaves out {{ describeCount(site.removed.length) }}
        </span>
      </span>
      <div class="flex flex-wrap gap-x-3 gap-y-0.5 font-mono text-12">
        <span v-for="name in site.defines" :key="name" :class="{ 'text-warning': site.removed?.includes(name) }">
          {{ name }}
        </span>
      </div>
    </div>
    <ScriptSnippet
      v-else-if="snippet?.text"
      :text="snippet.text"
      :line="snippet.line"
      :focus="focus"
      :language="EDITOR_LANGUAGES[site.place.format]"
      class="pb-1.5"
    />
  </div>
</template>
