<!-- Two files of the workspace side by side, read while the compare is open. The first is on the left. -->
<script setup lang="ts">
import { useQuery } from "@pinia/colada";

import type { Place } from "@services/workspace/models";

import { fileQuery } from "../../queries";
import { useWorkspaceStore } from "../../stores/workspace";
import CompareView from "../../ui/CompareView.vue";
import { EDITOR_LANGUAGES } from "../../ui/ScriptSnippet.vue";

const { left, right } = defineProps<{ left: Place; right: Place }>();

const workspace = useWorkspaceStore();
const leftFile = useQuery(() => fileQuery(left.path));
const rightFile = useQuery(() => fileQuery(right.path));

/** describePlace returns a file's label above its side, its origin's name and its path. */
function describePlace(place: Place): string {
  return `${workspace.originName(place.origin)} · ${place.rel}`;
}
</script>

<template>
  <div class="flex h-full min-h-0 flex-col overflow-hidden rounded-lg border border-default">
    <p v-if="leftFile.isPending.value || rightFile.isPending.value" class="p-4 text-12 text-dimmed">
      Reading the two files…
    </p>
    <p v-else-if="leftFile.error.value || rightFile.error.value" class="p-4 text-12 text-dimmed">
      {{ describePlace(leftFile.error.value ? left : right) }} could not be read.
    </p>
    <CompareView
      v-else-if="leftFile.data.value && rightFile.data.value"
      :left="leftFile.data.value.text.text"
      :right="rightFile.data.value.text.text"
      :language="EDITOR_LANGUAGES[right.format]"
      :left-label="describePlace(left)"
      :right-label="describePlace(right)"
    />
  </div>
</template>
