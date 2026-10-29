<!--
  One place a finding points at: a line with its mod and its file at the line, which opens it in the IDE, over the
  code there. A copy shows its whole definition, and a copy the game ignores is dimmed.
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
import ModTitle from "../../ui/ModTitle.vue";
import ScriptSnippet, { EDITOR_LANGUAGES } from "../../ui/ScriptSnippet.vue";

const {
  site,
  snippet = undefined,
  ignored = false,
} = defineProps<{
  site: FindingSite;
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
  site.kept || site.reason || ignored ? undefined : [site.range.startLineNumber, site.range.endLineNumber],
);
</script>

<template>
  <div class="flex min-w-0 flex-col gap-2">
    <button
      type="button"
      class="flex min-w-0 items-center gap-1.75 text-start text-12 whitespace-nowrap text-dimmed hover:underline"
      :title="`Open ${site.place.rel} in the IDE`"
      @click="openSite(site)"
    >
      <ModTitle :origin="site.place.origin" class="shrink-0" />
      <span aria-hidden="true">·</span>
      <span class="min-w-0 truncate font-mono"
        ><template v-if="site.place.folder">{{ site.place.folder }}/</template
        ><span class="font-medium text-highlighted">{{ site.place.name }}</span
        >:{{ site.range.startLineNumber }}</span
      >
    </button>
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
