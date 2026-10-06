<script setup lang="ts">
/**
 * VS Code ViewsService attachPart grid. Always mounted at full size.
 * Tool pages cover it; never hide this host with display/visibility/opacity/inert.
 *
 * The monaco root is a class-free element so Vue re-renders cannot wipe
 * `monaco-workbench` (classList.add from LayoutService). Shell z-index lives
 * on the outer wrapper. The sidebar, panel and Guide sizes are set by resize grips.
 * Guide is a Nuxt column, not a monaco auxiliary bar.
 */
import { computed, onMounted, useTemplateRef, watch } from "vue";
import { useElementSize, useLocalStorage } from "@vueuse/core";
import { guideOpen, ideVisible, setGuideOpen } from "../composables/useGuidePrefs";

const props = defineProps<{
  visible: boolean;
}>();

const root = useTemplateRef<HTMLElement>("root");
const activityBar = useTemplateRef<HTMLElement>("activityBar");
const sidebar = useTemplateRef<HTMLElement>("sidebar");
const editor = useTemplateRef<HTMLElement>("editor");
const panel = useTemplateRef<HTMLElement>("panel");
const work = useTemplateRef<HTMLElement>("work");

// the explorer, problems and Guide sizes, each at most half the space it divides
const SIDEBAR = { min: 170, default: 300 };
const PANEL = { min: 120, default: 200 };
const GUIDE = { min: 320, default: 440 };
const sidebarWidth = useLocalStorage("pmt.ide.sidebarWidth", SIDEBAR.default);
const panelHeight = useLocalStorage("pmt.ide.panelHeight", PANEL.default);
const guideWidth = useLocalStorage("pmt.ide.guideWidth", GUIDE.default);
const host = useElementSize(root);
const row = useElementSize(work);
const halfWide = computed(() => Math.max(SIDEBAR.min, host.width.value / 2));
const halfTall = computed(() => Math.max(PANEL.min, host.height.value / 2));
const halfRow = computed(() => Math.max(GUIDE.min, row.width.value / 2));

watch(
  () => props.visible,
  (v) => {
    ideVisible.value = v;
    if (!v) setGuideOpen(false);
  },
  { immediate: true },
);

onMounted(async () => {
  if (
    !root.value || !activityBar.value || !sidebar.value ||
    !editor.value || !panel.value
  ) {
    return;
  }
  root.value.classList.add("ide-monaco-host");
  const { registerIdeParts } = await import("../ide/workbenchHost");
  registerIdeParts({
    root: root.value,
    activityBar: activityBar.value,
    sidebar: sidebar.value,
    editor: editor.value,
    panel: panel.value,
  });
});
</script>

<template>
  <!-- nokey: the Event Graph's Vue Flow, kept alive, listens for keys on the whole document and
    cancels Space (its pan key) where it does not see an input; Monaco types into a plain div -->
  <div
    class="nokey absolute inset-0 flex min-h-0 flex-col overflow-hidden bg-default"
    :class="visible ? 'z-10' : 'z-0 pointer-events-none'"
  >
    <slot name="toolbar" />
    <div
      ref="work"
      class="flex min-h-0 min-w-0 flex-1 overflow-hidden"
    >
      <div class="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
        <div ref="root">
          <div class="relative flex min-h-0 min-w-0 flex-1">
            <div ref="activityBar" class="ide-activity-bar w-12 shrink-0" />
            <div
              ref="sidebar"
              class="ide-sidebar my-1.5 me-1 min-w-42.5 max-w-1/2 overflow-hidden rounded-xl"
              :style="{ width: `${sidebarWidth}px` }"
            />
            <ResizeGrip
              v-model="sidebarWidth"
              class="ide-sash--v"
              :min="SIDEBAR.min"
              :max="halfWide"
              :default-size="SIDEBAR.default"
            />
            <div ref="editor" class="ide-editor min-h-0 min-w-0 flex-1" />
          </div>
          <ResizeGrip
            v-model="panelHeight"
            class="ide-sash--h"
            axis="y"
            reverse
            :min="PANEL.min"
            :max="halfTall"
            :default-size="PANEL.default"
          />
          <div
            ref="panel"
            class="ide-panel min-h-30 max-h-1/2"
            :style="{ height: `${panelHeight}px` }"
          />
        </div>
      </div>
      <UButton
        v-if="visible && !guideOpen"
        class="ide-guide-tab"
        icon="i-lucide-book-open"
        label="Guide"
        size="xs"
        color="neutral"
        variant="soft"
        @click="setGuideOpen(true)"
      />
      <ResizeGrip
        v-show="guideOpen"
        v-model="guideWidth"
        reverse
        :min="GUIDE.min"
        :max="halfRow"
        :default-size="GUIDE.default"
      />
      <aside
        v-show="guideOpen"
        class="ide-guide my-1.5 me-1 min-h-0 shrink-0 overflow-hidden rounded-xl"
        :style="{ width: `${guideWidth}px` }"
      >
        <GuidePane />
      </aside>
    </div>
  </div>
</template>

<style scoped>
.ide-monaco-host {
  display: flex;
  flex: 1 1 0%;
  min-height: 0;
  min-width: 0;
  flex-direction: column;
  overflow: hidden;
}

.ide-sidebar.ide-part-hidden + .ide-sash--v,
.ide-sash--h:has(+ .ide-panel.ide-part-hidden) {
  display: none;
}

.ide-guide-tab {
  align-self: center;
  margin-inline: 0.25rem;
  writing-mode: vertical-rl;
  rotate: 180deg;
}
</style>
