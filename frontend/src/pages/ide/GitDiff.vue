<!--
  A diff tab that compares a file's text with a revision. Its buttons stage, unstage or discard the file's changes.
-->
<script setup lang="ts">
import { computed, shallowRef, watchEffect } from "vue";
import { useMutation, useQuery } from "@pinia/colada";

import * as gitApi from "@services/git/service";

import { commitsQuery, fileQuery, gitFileQuery, gitStatusQuery } from "../../queries";
import { useEditorStore } from "../../stores/editor";
import CompareView from "../../ui/CompareView.vue";
import OriginChip from "../../ui/OriginChip.vue";
import { EDITOR_LANGUAGES } from "../../ui/ScriptSnippet.vue";
import { applyEdits, findModel } from "./editor/models";
import { fileTab, type TabTarget } from "./editor/tabs";

const { tab } = defineProps<{ tab: Extract<TabTarget, { tab: "diff" }> }>();

const editor = useEditorStore();
const place = computed(() => editor.placeOf(fileTab(tab.key)));
const added = shallowRef(0);
const removed = shallowRef(0);

const { data: status } = useQuery(() => ({ ...gitStatusQuery(tab.root), enabled: !!tab.root }));
const { data: log } = useQuery(() => ({
  ...commitsQuery({ dir: tab.root, refs: [], limit: 1 }),
  enabled: !!tab.root,
}));
const entry = computed(() => status.value?.changes?.find((one) => one.place.key === tab.key));
const gitPath = computed(() => entry.value?.path ?? place.value?.rel ?? "");
const sides = computed(() => {
  const last = `Last commit, ${log.value?.[0]?.short ?? ""}`;
  const staged = entry.value?.staged;
  const short = tab.hash?.slice(0, 7);
  return {
    changes: {
      revisions: [":0", ""],
      labels: [staged ? "Staged" : last, "Working tree"],
      action: { label: "Stage file", icon: "i-lucide-plus", call: gitApi.Stage },
    },
    staged: {
      revisions: ["HEAD", ":0"],
      labels: [last, "Staged"],
      action: { label: "Unstage file", icon: "i-lucide-minus", call: gitApi.Unstage },
    },
    conflict: { revisions: [":2", ":3"], labels: ["Current (HEAD)", "Incoming"] },
    commit: { revisions: [`${tab.hash}^`, tab.hash ?? ""], labels: [`Before ${short}`, `Commit ${short}`] },
  }[tab.side];
});

const { data: left } = useQuery(() => ({
  ...gitFileQuery({ dir: tab.root, revision: sides.value.revisions[0] ?? "", path: gitPath.value }),
  enabled: !!tab.root && !!place.value,
}));
const { data: revised } = useQuery(() => ({
  ...gitFileQuery({ dir: tab.root, revision: sides.value.revisions[1] ?? "", path: gitPath.value }),
  enabled: !!tab.root && !!place.value && tab.side !== "changes",
}));
const { data: file, error: unreadable } = useQuery(() => ({
  ...fileQuery(place.value?.path ?? ""),
  enabled: !!place.value && tab.side === "changes",
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
const right = computed(() => (tab.side === "changes" ? working.value : (revised.value ?? "")));

const { mutate: change } = useMutation({
  mutation: (call: typeof gitApi.Stage) => call(tab.root, [gitPath.value]),
});

/** discard throws the file's changes away in its editor. The edit is unsaved and can be undone. */
function discard(): void {
  const model = findModel(tab.key);
  if (model && left.value !== undefined)
    applyEdits(model, [{ range: model.model.getFullModelRange(), text: left.value }]);
}
</script>

<template>
  <CompareView
    :left="left ?? ''"
    :right="right"
    :language="place ? EDITOR_LANGUAGES[place.format] : 'plaintext'"
    :left-label="sides.labels[0] ?? ''"
    :right-label="sides.labels[1] ?? ''"
    @changes="(plus, minus) => ((added = plus), (removed = minus))"
  >
    <template #header>
      <OriginChip v-if="place" :origin="place.origin" dot />
      <span class="truncate text-toned">{{ place?.rel }}</span>
      <span class="grow" />
      <span v-if="unreadable" class="truncate">{{ unreadable.message }}</span>
      <UBadge :label="`+${added}`" color="success" size="sm" />
      <UBadge :label="`-${removed}`" color="error" size="sm" />
      <UButton
        v-if="sides.action"
        :label="sides.action.label"
        :icon="sides.action.icon"
        variant="ghost"
        size="xs"
        @click="change(sides.action.call)"
      />
      <UButton
        v-if="tab.side === 'changes' && !unreadable"
        label="Discard"
        color="error"
        variant="ghost"
        size="xs"
        @click="discard"
      />
    </template>
  </CompareView>
</template>
