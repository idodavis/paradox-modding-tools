<!-- Read-only code, colored in the page's theme. -->
<script setup lang="ts">
import { shallowRef, watch } from "vue";
import { editor } from "monaco-editor/editor/editor.api";

import { useSettingsStore } from "../stores/settings";

const { text, language } = defineProps<{ text: string; language: string }>();

const settings = useSettingsStore();
const html = shallowRef("");

watch(
  [() => text, () => language, () => settings.theme()],
  async (_sources, _previous, onCleanup) => {
    let stale = false;
    onCleanup(() => (stale = true));
    const colored = await editor.colorize(text, language, {});
    if (!stale) html.value = colored;
  },
  { immediate: true, flush: "post" },
);
</script>

<template>
  <!-- eslint-disable-next-line vue/no-v-html -- Monaco escapes the text it colors -->
  <pre class="overflow-x-auto font-mono text-code whitespace-pre text-(--fg-1)" v-html="html" />
</template>
