<script setup lang="ts">
/**
 * The status bar (pmt.css .statusbar): the page's own facts, which a page sends to #status-start and
 * #status-end, then the main window's count of windows, each brought to the front from it, Help, with the page's
 * help and diagnostics for a bug report, and the version, which checks for an update. It tells of a last run that
 * ended without closing.
 */
import { computed, watch } from "vue";
import { useRoute } from "vue-router";
import { useMutation, useQuery } from "@pinia/colada";
import { CheckForUpdates } from "@services/appservice";
import { CopyDiagnostics } from "@services/logservice";
import { crashedQuery, versionQuery } from "../queries";
import { PAGES } from "../workspaceTools";
import { Show } from "@services/windowservice";
import { isMain, others } from "../windows";

const route = useRoute();
const toast = useToast();
const page = computed(() => (route.name ? PAGES[route.name] : undefined));

const { data: version } = useQuery(versionQuery);
const { mutate: checkForUpdates, isLoading: checking } = useMutation({ mutation: () => CheckForUpdates() });
const { mutate: copyDiagnostics } = useMutation({
  mutation: () => CopyDiagnostics(),
  onSuccess: () => toast.add({ title: "Diagnostics copied", description: "Paste them into a bug report on GitHub.", color: "success" }),
});

// a crash, or the app ended from outside
const { data: crashed } = useQuery(crashedQuery);
watch(crashed, (yes) => {
  if (!yes) return;
  toast.add({
    title: "PMT closed unexpectedly",
    description: "The last session ended without closing. Copy diagnostics and paste them into a bug report on GitHub.",
    color: "warning",
    icon: "i-lucide-triangle-alert",
    duration: 0,
    actions: [{ label: "Copy diagnostics", color: "warning", onClick: () => copyDiagnostics() }],
  });
});

const ITEM = "flex h-6 items-center gap-1.25 hover:text-highlighted";
const windows = computed(() => [...others.values()].map((one) => ({ label: one.title, onSelect: () => void Show(one.name) })));
</script>

<template>
  <footer
    class="flex h-6 shrink-0 items-center gap-3.5 border-t border-default bg-muted px-3 text-11 whitespace-nowrap
      text-dimmed"
  >
    <div id="status-start" class="contents" />
    <span class="grow" />
    <div id="status-end" class="contents" />
    <UDropdownMenu v-if="isMain && windows.length" :items="windows" :content="{ side: 'top' }">
      <button type="button" :class="ITEM"><UIcon name="i-lucide-app-window" class="size-3" />{{ windows.length + 1 }} windows</button>
    </UDropdownMenu>
    <!-- one size on every page, the longest help's (Publish) -->
    <UModal :title="`Help: ${page?.title ?? 'PMT'}`" :ui="{ content: 'h-118 sm:max-w-2xl' }">
      <button type="button" :class="ITEM">Help</button>
      <template #body>
        <div class="flex flex-col gap-3 text-13 text-toned">
          <p v-for="(para, i) in page?.help ?? []" :key="i">{{ para }}</p>
        </div>
      </template>
      <template #footer>
        <span class="grow text-12 text-dimmed">Found a bug? Copy diagnostics and paste them into a GitHub issue.</span>
        <UButton label="Copy diagnostics" icon="i-lucide-clipboard-copy" @click="copyDiagnostics()" />
      </template>
    </UModal>
    <UTooltip text="Check for updates">
      <button type="button" :class="ITEM" :disabled="checking" @click="checkForUpdates()">
        {{ version }}
      </button>
    </UTooltip>
  </footer>
</template>
