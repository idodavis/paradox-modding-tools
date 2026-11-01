<!--
  One place a finding points at: a line with its mod and its file at the line, which opens it in the IDE, over the
  code there. A copy shows its whole definition, and a copy the game ignores is dimmed.
-->
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
import ScriptSnippet, { EDITOR_LANGUAGES } from "../../ui/ScriptSnippet.vue";
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

/**
 * The site's own lines to highlight. A copy, one the game keeps, one it ignores or one whose keeping no source
 * settles, shows a whole definition and highlights none.
 */
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
      :language="EDITOR_LANGUAGES[site.place.format]"
      class="rounded-md bg-(--bg-0) py-1.5"
      :class="{ 'opacity-70': ignored }"
    />
  </div>
</template>
