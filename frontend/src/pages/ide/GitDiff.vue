<!-- A diff tab that compares a file's text with a revision. -->
<script setup lang="ts">
import { computed, shallowRef, watchEffect } from "vue";
import { useQuery } from "@pinia/colada";

import { commitsQuery, fileQuery, gitFileQuery, gitStatusQuery } from "../../queries";
import { useEditorStore, type DiffSide, type Tab } from "../../stores/editor";
import CompareView from "../../ui/CompareView.vue";
import OriginChip from "../../ui/OriginChip.vue";
import { findModel } from "./editor/models";

const { tab, side, root = "" } = defineProps<{ tab: Tab; side: DiffSide; root?: string }>();

const editor = useEditorStore();
const place = computed(() => editor.placeOf(tab.key));

const { data: status } = useQuery(() => ({ ...gitStatusQuery(root), enabled: !!root }));
const { data: log } = useQuery(() => ({
  ...commitsQuery({ dir: root, refs: [], limit: 1 }),
  enabled: !!root,
}));
const entry = computed(() => status.value?.changes?.find((change) => change.place.key === tab.key));
const deleted = computed(() => entry.value?.status === "deleted");
const gitPath = computed(() => entry.value?.path ?? place.value?.rel ?? "");
const sides = computed(() => {
  const last = `Last commit, ${log.value?.[0]?.short ?? ""}`;
  const short = tab.hash?.slice(0, 7);
  return {
    changes: { revisions: [":0", ""], labels: [entry.value?.staged ? "Staged" : last, "Working tree"] },
    staged: { revisions: ["HEAD", ":0"], labels: [last, "Staged"] },
    conflict: { revisions: [":2", ":3"], labels: ["Current (HEAD)", "Incoming"] },
    commit: { revisions: [`${tab.hash}^`, tab.hash ?? ""], labels: [`Before ${short}`, `Commit ${short}`] },
  }[side];
});

const { data: left } = useQuery(() => ({
  ...gitFileQuery({ dir: root, revision: sides.value.revisions[0] ?? "", path: gitPath.value }),
  enabled: !!root && !!place.value,
}));
const { data: revised } = useQuery(() => ({
  ...gitFileQuery({ dir: root, revision: sides.value.revisions[1] ?? "", path: gitPath.value }),
  enabled: !!root && !!place.value && side !== "changes",
}));
// The working tree is read once git status says the file is there. A deleted file's side stays empty.
const { data: file, error: unreadable } = useQuery(() => ({
  ...fileQuery(place.value?.path ?? ""),
  enabled: !!place.value && side === "changes" && !!status.value && !deleted.value,
}));

const working = shallowRef("");
watchEffect((onCleanup) => {
  const model = findModel(tab.key);
  if (!model) {
    working.value = file.value?.text.text ?? "";
    return;
  }
  working.value = model.model.getValue();
  const subscription = model.model.onDidChangeContent(() => (working.value = model.model.getValue()));
  onCleanup(() => subscription.dispose());
});
const right = computed(() => (side === "changes" ? working.value : (revised.value ?? "")));
</script>

<template>
  <CompareView
    :left="left ?? ''"
    :right="right"
    :language="place?.language ?? 'plaintext'"
    :left-label="sides.labels[0] ?? ''"
    :right-label="sides.labels[1] ?? ''"
  >
    <template #header>
      <OriginChip v-if="place" :origin="place.origin" dot />
      <span class="truncate font-medium text-toned">{{ place?.rel }}</span>
      <span v-if="unreadable" class="truncate text-error">{{ unreadable.message }}</span>
    </template>
  </CompareView>
</template>
