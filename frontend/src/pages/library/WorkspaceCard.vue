<script setup lang="ts">
/**
 * A workspace's card in the Library: its name, its install and the mods it
 * loads. Opening it makes it the active workspace.
 */
import { computed } from "vue";
import { useRouter } from "vue-router";
import { useQuery } from "@pinia/colada";
import type { DropdownMenuItem } from "@nuxt/ui";
import type { Workspace } from "@services/models";
import RemoveWorkspaceModal from "../../ui/RemoveWorkspaceModal.vue";
import { languageQuery } from "../../queries";
import { useSettingsStore } from "../../stores/settings";
import { modColor } from "../../stores/workspace";
import { workspaceHomeRoute } from "../../app/pages";
import OriginChip from "../../ui/OriginChip.vue";

const { workspace, active = false } = defineProps<{ workspace: Workspace; active?: boolean }>();

const router = useRouter();
const settings = useSettingsStore();
const { data: health } = useQuery(() => languageQuery(workspace.id));
const removeModal = useOverlay().create(RemoveWorkspaceModal);
/** "1.14.2, install OK", the version left out where the install gives none. */
const install = computed(() => {
  const live = health.value;
  return live ? [live.gameVersion, live.installOk ? "install OK" : "install not found"].filter(Boolean).join(", ") : "";
});

const more = computed<DropdownMenuItem[]>(() => [
  { label: "Settings", icon: "i-lucide-settings", to: { name: "workspace-settings", params: { id: workspace.id } } },
  {
    label: "Remove from PMT…",
    icon: "i-lucide-trash-2",
    color: "error",
    onSelect: () => void removeModal.open({ id: workspace.id, name: workspace.name }),
  },
]);

function open(): void {
  void router.push(workspaceHomeRoute(workspace, settings.visibleTools));
}
</script>

<template>
  <div
    class="flex min-w-0 cursor-pointer flex-col gap-3 rounded-lg border bg-muted p-4 hover:border-accented"
    :class="active ? 'border-primary' : 'border-default'"
    role="link"
    tabindex="0"
    @click="open"
    @keydown.enter="open"
  >
    <div class="flex min-w-0 items-center gap-2">
      <span class="truncate text-13 font-semibold">{{ workspace.name }}</span>
      <span class="grow" />
      <UBadge v-if="active" label="Active" color="primary" />
      <UDropdownMenu v-else :items="more">
        <UButton icon="i-lucide-ellipsis" variant="ghost" size="sm" square aria-label="More" @click.stop />
      </UDropdownMenu>
    </div>
    <span v-if="!workspace.installId" class="flex items-center gap-1.25 text-12 text-warning">
      <UIcon name="i-lucide-triangle-alert" class="size-3.5" />No install set
    </span>
    <span v-else-if="install" class="text-12 text-dimmed">{{ install }}</span>
    <div class="flex flex-wrap gap-1.5">
      <OriginChip v-for="mod in workspace.mods ?? []" :key="mod.id" :label="mod.name" :color="modColor(mod)" />
      <span v-if="!workspace.mods?.length" class="text-12 text-dimmed">No mods, game files only</span>
    </div>
  </div>
</template>
