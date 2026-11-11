<!-- One place a finding points at: a link to its file and line over the code there. -->
<script lang="ts">
import type { Site } from "@services/lsp/models";

import { useEditorStore } from "../../stores/editor";

/** openSite opens a site in the editor at its line, at the start of its range where it has one. */
export function openSite(site: Site): void {
  useEditorStore().show(site.place, { lineNumber: site.line, column: site.range.startColumn || 1 });
}
</script>

<script setup lang="ts">
import { computed } from "vue";
import type { Snippet } from "@services/health/models";
import ModTitle from "../../ui/ModTitle.vue";
import ScriptSnippet from "../../ui/ScriptSnippet.vue";
import SourceLink from "../../ui/SourceLink.vue";

const {
  site,
  snippet = undefined,
  ignored = false,
} = defineProps<{
  site: Site;
  /** The code around the site. */
  snippet?: Snippet;
  /** Whether the site is a copy the game ignores. */
  ignored?: boolean;
}>();

/** The site's own lines to highlight. A kept, ignored or unsettled copy shows its whole definition, with none. */
const focus = computed((): [number, number] | undefined =>
  site.kept || site.reason || ignored ? undefined : [site.line, snippet?.last ?? site.line],
);
</script>

<template>
  <div class="flex min-w-0 flex-col gap-2">
    <SourceLink :place="site.place" :line="site.line" :column="site.range.startColumn || undefined" class="self-start">
      <template #leading><ModTitle :origin="site.place.origin" class="shrink-0" /></template>
    </SourceLink>
    <ScriptSnippet
      v-if="snippet?.text"
      :text="snippet.text"
      :line="snippet.line"
      :focus="focus"
      :path="site.place.path"
      class="rounded-md bg-(--bg-0) py-1.5"
      :class="{ 'opacity-70': ignored }"
    />
  </div>
</template>
