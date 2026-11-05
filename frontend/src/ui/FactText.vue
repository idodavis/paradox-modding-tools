<!-- Shows a message's parts, each fact in its own style. A fact named again shows as plain text. -->
<script setup lang="ts">
import { computed } from "vue";

import type { Part } from "@services/lsp/models";
import { useWorkspaceStore, VANILLA } from "../stores/workspace";

const { parts = [] } = defineProps<{ parts?: readonly Part[] | null }>();

const workspace = useWorkspaceStore();
/** The text color of the game's and each mod's name. */
const modColors = computed(
  () =>
    new Map(
      [VANILLA, ...workspace.listMods().map((mod) => mod.id)].map((origin) => [
        workspace.originName(origin),
        workspace.originTextColor(origin),
      ]),
    ),
);

/**
 * drawn holds each part with its style: plain for a fact shown before, a mod in its color with the 's after it, a path
 * split into folder and file, and code split after each underscore, where its line may break.
 */
const drawn = computed(() => {
  const keys = new Set<string>();
  const styled = (parts ?? []).map((part) => {
    const key = `${part.style}:${part.text}`;
    const style = keys.has(key) ? "" : part.style;
    keys.add(key);
    return { text: part.text, style };
  });
  return styled.map(({ text, style }, index) => {
    const owns = style === "mod" && !!styled[index + 1]?.text.startsWith("'s");
    const owned = styled[index - 1]?.style === "mod" && text.startsWith("'s");
    const fileStart = style === "path" ? text.lastIndexOf("/") + 1 : 0;
    return {
      text: (owned ? text.slice(2) : text) + (owns ? "'s" : ""),
      style,
      color: style === "mod" ? modColors.value.get(text) : undefined,
      folder: text.slice(0, fileStart),
      file: text.slice(fileStart),
      pieces: text.split(/(?<=_)/),
    };
  });
});
</script>

<template>
  <template v-for="(part, index) in drawn" :key="index">
    <b
      v-if="part.style === 'mod' || part.style === 'count'"
      class="font-semibold text-highlighted tabular-nums"
      :style="part.color ? { color: part.color } : undefined"
      >{{ part.text }}</b
    >
    <code
      v-else-if="part.style === 'code'"
      class="rounded-sm bg-elevated px-1 py-px font-mono text-[0.88em] box-decoration-clone wrap-anywhere text-default"
      ><template v-for="(piece, at) in part.pieces" :key="at">{{ piece }}<wbr /></template
    ></code>
    <span v-else-if="part.style === 'path'" class="font-mono text-[0.92em] wrap-anywhere text-dimmed"
      >{{ part.folder }}<span class="font-medium text-highlighted">{{ part.file }}</span></span
    >
    <template v-else>{{ part.text }}</template>
  </template>
</template>
