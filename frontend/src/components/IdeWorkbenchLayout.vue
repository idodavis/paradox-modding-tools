<script setup lang="ts">
/**
 * VS Code ViewsService attachPart grid. Always mounted at full size.
 * Tool pages cover it; never hide this host with display/visibility/opacity/inert.
 *
 * The monaco root is a class-free element so Vue re-renders cannot wipe
 * `monaco-workbench` (classList.add from LayoutService). Shell z-index lives
 * on the outer wrapper. Sidebar/panel/guide size is host CSS vars + drag sashes.
 * Guide is a Nuxt column, not a monaco auxiliary bar.
 */
import { onMounted, shallowRef, useTemplateRef, watch } from "vue";
import { guideOpen, ideVisible, setGuideOpen } from "../composables/useGuidePrefs";
import GuidePane from "./GuidePane.vue";

const props = defineProps<{
  visible: boolean;
}>();

const SIDEBAR_KEY = "ide.sidebarWidth";
const PANEL_KEY = "ide.panelHeight";
const GUIDE_KEY = "ide.guideWidth";
const SIDEBAR_MIN = 170;
const PANEL_MIN = 120;
const GUIDE_MIN = 320;
const SIDEBAR_DEFAULT = 300;
const PANEL_DEFAULT = 200;
const GUIDE_DEFAULT = 440;

const root = useTemplateRef<HTMLElement>("root");
const activityBar = useTemplateRef<HTMLElement>("activityBar");
const sidebar = useTemplateRef<HTMLElement>("sidebar");
const editor = useTemplateRef<HTMLElement>("editor");
const panel = useTemplateRef<HTMLElement>("panel");
const work = useTemplateRef<HTMLElement>("work");

/** Load a persisted pixel size, or the default. */
function loadPx(key: string, fallback: number): number {
  const n = Number(localStorage.getItem(key));
  return Number.isFinite(n) && n > 0 ? n : fallback;
}

const sidebarWidth = shallowRef(loadPx(SIDEBAR_KEY, SIDEBAR_DEFAULT));
const panelHeight = shallowRef(loadPx(PANEL_KEY, PANEL_DEFAULT));
const guideWidth = shallowRef((() => {
  const n = Number(localStorage.getItem(GUIDE_KEY));
  return Number.isFinite(n) && n >= 360 ? n : GUIDE_DEFAULT;
})());

/** Clamp explorer width to min 170px and 50% of the host. */
function clampSidebar(px: number, host: HTMLElement): number {
  const max = host.clientWidth * 0.5;
  return Math.round(Math.min(max, Math.max(SIDEBAR_MIN, px)));
}

/** Clamp problems height to min 120px and 50% of the host. */
function clampPanel(px: number, host: HTMLElement): number {
  const max = host.clientHeight * 0.5;
  return Math.round(Math.min(max, Math.max(PANEL_MIN, px)));
}

/** Clamp Guide width to min 320px and 50% of the work row. */
function clampGuide(px: number, host: HTMLElement): number {
  const max = host.clientWidth * 0.5;
  return Math.round(Math.min(max, Math.max(GUIDE_MIN, px)));
}

/** Drag the sidebar width, Guide width, or panel height sash. */
function startDrag(
  kind: "sidebar" | "guide" | "panel",
  ev: PointerEvent,
): void {
  const host = kind === "guide" ? work.value : root.value;
  if (!host) return;
  ev.preventDefault();
  const startX = ev.clientX;
  const startY = ev.clientY;
  const startW = sidebarWidth.value;
  const startG = guideWidth.value;
  const startH = panelHeight.value;
  const target = ev.currentTarget as HTMLElement;
  target.setPointerCapture(ev.pointerId);
  const onMove = (e: PointerEvent) => {
    if (kind === "sidebar") {
      sidebarWidth.value = clampSidebar(startW + (e.clientX - startX), host);
      return;
    }
    if (kind === "guide") {
      guideWidth.value = clampGuide(startG - (e.clientX - startX), host);
      return;
    }
    panelHeight.value = clampPanel(startH - (e.clientY - startY), host);
  };
  const onUp = () => {
    target.releasePointerCapture(ev.pointerId);
    target.removeEventListener("pointermove", onMove);
    target.removeEventListener("pointerup", onUp);
    if (kind === "sidebar") {
      localStorage.setItem(SIDEBAR_KEY, String(sidebarWidth.value));
      return;
    }
    if (kind === "guide") {
      localStorage.setItem(GUIDE_KEY, String(guideWidth.value));
      return;
    }
    localStorage.setItem(PANEL_KEY, String(panelHeight.value));
  };
  target.addEventListener("pointermove", onMove);
  target.addEventListener("pointerup", onUp);
}

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
        <div
          ref="root"
          :style="{
            '--ide-sidebar-width': `${sidebarWidth}px`,
            '--ide-panel-height': `${panelHeight}px`,
          }"
        >
          <div class="relative flex min-h-0 min-w-0 flex-1">
            <div ref="activityBar" class="ide-activity-bar w-12 shrink-0" />
            <div
              ref="sidebar"
              class="ide-sidebar my-1.5 me-1 w-[var(--ide-sidebar-width,300px)] min-w-[170px] max-w-[50%] overflow-hidden rounded-xl"
            />
            <div
              class="ide-sash ide-sash--v w-1 shrink-0 cursor-col-resize"
              @pointerdown="startDrag('sidebar', $event)"
            />
            <div ref="editor" class="ide-editor min-h-0 min-w-0 flex-1" />
          </div>
          <div
            class="ide-sash ide-sash--h h-1 shrink-0 cursor-row-resize"
            @pointerdown="startDrag('panel', $event)"
          />
          <div
            ref="panel"
            class="ide-panel h-[var(--ide-panel-height,200px)] min-h-[120px] max-h-[50%]"
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
      <div
        v-show="guideOpen"
        class="ide-sash ide-sash--guide w-1.5 shrink-0 cursor-col-resize"
        @pointerdown="startDrag('guide', $event)"
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

.ide-sash--guide:hover {
  background: color-mix(in oklab, var(--ui-border) 80%, transparent);
}
</style>
