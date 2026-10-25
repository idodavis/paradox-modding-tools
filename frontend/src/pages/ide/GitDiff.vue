<!--
  A diff tab: the file's live text against the chosen revision, with buttons that stage the file, unstage it or throw
  its changes away.
-->
<script setup lang="ts">
import { computed, shallowRef, watchEffect } from "vue";
import { useMutation, useQuery } from "@pinia/colada";

import * as gitApi from "@services/git/service";

import { commitsQuery, fileQuery, gitFileQuery, gitStatusQuery } from "../../queries";
import { useEditorStore } from "../../stores/editor";
import { useGitStore } from "../../stores/git";
import CompareView from "../../ui/CompareView.vue";
import OriginChip from "../../ui/OriginChip.vue";
import { applyEdits, findModel } from "./editor/models";
import { diffTab, VIEWS, type TabTarget } from "./editor/tabs";

const { tab } = defineProps<{ tab: Extract<TabTarget, { tab: "diff" }> }>();

const editor = useEditorStore();
const git = useGitStore();
const dir = computed(() => git.repository());
const place = computed(() => editor.placeOf(diffTab(tab.side, tab.key, tab.hash)));
const added = shallowRef(0);
const removed = shallowRef(0);

const { data: status } = useQuery(() => ({ ...gitStatusQuery(dir.value), enabled: !!dir.value }));
const { data: log } = useQuery(() => ({
  ...commitsQuery({ dir: dir.value, refs: [], limit: 1 }),
  enabled: !!dir.value,
}));
const entry = computed(() => status.value?.changes?.find((one) => one.place.key === tab.key));
const gitPath = computed(() => entry.value?.path ?? place.value?.rel ?? "");
const sides = computed(() => {
  const last = `Last commit, ${log.value?.[0]?.short ?? ""}`;
  const staged = entry.value?.staged;
  const short = tab.hash?.slice(0, 7);
  return {
    changes: { revisions: [":0", ""], labels: [staged ? "Staged" : last, "Working tree"] },
    staged: { revisions: ["HEAD", ":0"], labels: [last, "Staged"] },
    conflict: { revisions: [":2", ":3"], labels: ["Current (HEAD)", "Incoming"] },
    commit: { revisions: [`${tab.hash}^`, tab.hash ?? ""], labels: [`Before ${short}`, `Commit ${short}`] },
  }[tab.side];
});

const { data: left } = useQuery(() => ({
  ...gitFileQuery({ dir: dir.value, revision: sides.value.revisions[0] ?? "", path: gitPath.value }),
  enabled: !!dir.value && !!place.value,
}));
const { data: revised } = useQuery(() => ({
  ...gitFileQuery({ dir: dir.value, revision: sides.value.revisions[1] ?? "", path: gitPath.value }),
  enabled: !!dir.value && !!place.value && tab.side !== "changes",
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

const action = computed(() => {
  if (tab.side === "changes") return { label: "Stage file", icon: "i-lucide-plus", call: gitApi.Stage };
  return tab.side === "staged" ? { label: "Unstage file", icon: "i-lucide-minus", call: gitApi.Unstage } : null;
});
const { mutate: change } = useMutation({
  mutation: (call: typeof gitApi.Stage) => call(dir.value, [gitPath.value]),
});

/** Puts the staged copy's text in the file's editor in place of its changes, unsaved and undoable. */
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
    :language="place ? VIEWS[place.format].language : 'plaintext'"
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
        v-if="action"
        :label="action.label"
        :icon="action.icon"
        variant="ghost"
        size="xs"
        @click="change(action.call)"
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
