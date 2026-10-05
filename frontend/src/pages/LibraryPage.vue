<script setup lang="ts">
/**
 * Library page: all workspaces grouped by game, hero for active workspace.
 */
import { computed, onActivated, ref } from "vue";
import { useRouter } from "vue-router";
import { useMutation } from "@pinia/colada";
import { modColor, useWorkspaceStore } from "../stores/workspace";
import { Workspace } from "@services/models";
import { AddWorkspaceMod, DeleteWorkspace } from "@services/workspaceservice";
import CreateModForm from "../components/CreateModForm.vue";
import { ResetData } from "@services/appservice";
import GameIcon from "../components/GameIcon.vue";
import LanguageHealthStrip from "../components/LanguageHealthStrip.vue";
import RemoveWorkspaceModal from "../components/RemoveWorkspaceModal.vue";
import { workspaceHomeRoute } from "../workspaceTools";
import { useSettingsStore } from "../stores/settings";

defineOptions({ name: "LibraryPage" });

const router = useRouter();
const ctx = useWorkspaceStore();
const settings = useSettingsStore();
const toast = useToast();

const showWizardPrompt = computed(() => !ctx.loading && !ctx.hasWorkspaces);
const pending = ref<Workspace | null>(null);
const pendingOpen = computed({
  get: () => pending.value !== null,
  set: (v: boolean) => {
    if (!v) pending.value = null;
  },
});
const newModOpen = ref(false);
const attachWsId = ref("");
const resetOpen = ref(false);
const resetTyped = ref("");
const canReset = computed(() => resetTyped.value === "RESET");

/** Workspaces grouped by game for sectioned library browsing. */
const sections = computed(() => ctx.workspacesByGame);

/** Open a workspace at its default (still-visible) tool page. */
function openWorkspace(ws: Workspace): void {
  ctx.setActiveWorkspace(ws.id);
  void router.push(workspaceHomeRoute(ws, settings.visibleTools));
}

/** Open workspace settings without triggering the card's open handler. */
function editWorkspace(ws: Workspace, e: Event): void {
  e.stopPropagation();
  ctx.setActiveWorkspace(ws.id);
  void router.push({ name: "workspace-settings", params: { id: ws.id } });
}

/** Ask before removing a workspace from PMT. */
function askDelete(ws: Workspace, e: Event): void {
  e.stopPropagation();
  pending.value = ws;
}

/** Navigate to wizard to create new workspace. */
function createWorkspace(): void {
  void router.push({ name: "wizard" });
}

const attachItems = computed(() =>
  ctx.workspaces.map((w) => ({
    label: `${w.name} (${ctx.shortName(w.gameId)})`,
    value: w.id,
  })),
);
const attachGameId = computed(() => ctx.workspaces.find((w) => w.id === attachWsId.value)?.gameId ?? ctx.currentGameId);

/** Open New Mod modal when a workspace exists; otherwise start the wizard. */
function createMod(): void {
  if (!ctx.hasWorkspaces) {
    void router.push({ name: "wizard", query: { newMod: "1" } });
    return;
  }
  attachWsId.value = ctx.activeWorkspaceId || ctx.workspaces[0]?.id || "";
  newModOpen.value = true;
}

/** Attach a created skeleton to the chosen workspace. */
async function onModCreated(path: string, thumbnail: string): Promise<void> {
  const id = attachWsId.value;
  if (!id) return;
  const label = path.split(/[/\\]/).pop() || "Mod";
  await AddWorkspaceMod(id, label, path, thumbnail);
  newModOpen.value = false;
  await ctx.refresh();
  toast.add({ title: "Mod created.", color: "success" });
}

const { mutateAsync: removeWorkspace, isLoading: removing } = useMutation({
  mutation: async () => {
    const ws = pending.value;
    if (!ws) return;
    const wasActive = ctx.activeWorkspaceId === ws.id;
    await DeleteWorkspace(ws.id);
    pending.value = null;
    await ctx.refresh();
    if (wasActive) {
      const next = ctx.workspaces[0];
      ctx.setActiveWorkspace(next?.id ?? "");
    }
  },
  onSuccess: () => toast.add({ title: "Workspace removed.", color: "success" }),
});

const { mutateAsync: resetData, isLoading: resetting } = useMutation({
  mutation: async () => {
    resetOpen.value = false;
    resetTyped.value = "";
    await ResetData();
    await ctx.refresh();
  },
  onSuccess: () => toast.add({ title: "PMT data reset.", color: "success" }),
});

onActivated(() => {
  void ctx.refresh();
});

</script>

<template>
  <div class="flex h-full min-h-0 flex-col overflow-auto p-4">
    <div class="mb-4 flex items-center justify-between gap-3">
      <div>
        <h1 class="text-xl font-bold">Workspace Library</h1>
        <p class="text-sm text-muted">Manage your modding workspaces</p>
      </div>
      <div v-if="!showWizardPrompt && !ctx.loading" class="flex gap-2">
        <UButton label="New Mod" icon="i-lucide-package-plus" color="neutral" variant="outline" @click="createMod" />
        <UButton label="New Workspace" icon="i-lucide-plus" @click="createWorkspace" />
      </div>
    </div>

    <UAlert v-if="ctx.error" color="error" variant="subtle" :description="ctx.error" class="mb-4" />

    <div v-if="ctx.loading" class="flex flex-1 items-center justify-center">
      <UButton loading variant="ghost" label="Loading workspaces..." />
    </div>

    <div v-else-if="showWizardPrompt" class="flex flex-1 items-center justify-center">
      <UEmpty icon="i-lucide-folder-plus" title="No workspaces yet"
        description="Create your first workspace or a new mod to get started.">
        <template #actions>
          <UButton label="Create Workspace" icon="i-lucide-plus" @click="createWorkspace" />
          <UButton label="New Mod" icon="i-lucide-package-plus" color="neutral" variant="outline" @click="createMod" />
        </template>
      </UEmpty>
    </div>

    <template v-else>
      <div v-if="ctx.activeWorkspace" class="mb-4 cursor-pointer" @click="openWorkspace(ctx.activeWorkspace)">
        <UCard class="transition-shadow hover:shadow-md">
          <template #header>
            <div class="flex items-center justify-between">
              <div class="flex items-center gap-2">
                <UBadge color="primary" variant="subtle">Active</UBadge>
                <span class="font-semibold">{{ ctx.activeWorkspace.name }}</span>
              </div>
              <div class="flex items-center gap-2">
                <UButton label="Edit" size="xs" color="neutral" variant="outline"
                  @click.stop="editWorkspace(ctx.activeWorkspace, $event)" />
                <UButton label="Delete" size="xs" color="error" variant="ghost"
                  @click.stop="askDelete(ctx.activeWorkspace, $event)" />
                <UBadge color="neutral" variant="outline" class="inline-flex items-center gap-1">
                  <GameIcon :game-id="ctx.activeWorkspace.gameId" />
                  {{ ctx.shortName(ctx.activeWorkspace.gameId) }}
                </UBadge>
              </div>
            </div>
          </template>
          <div class="text-sm text-muted">
            <p>{{ ctx.workspaceMods.length }} mod(s) attached</p>
            <UAvatarGroup v-if="ctx.workspaceMods.length" :max="5" size="sm" class="mt-2">
              <UAvatar v-for="(mod, i) in ctx.workspaceMods" :key="mod.id" :src="ctx.thumbUrls[mod.id]" :alt="mod.name"
                :style="{ boxShadow: `0 0 0 2px ${modColor(mod)}` }" />
            </UAvatarGroup>
            <LanguageHealthStrip class="mt-2" :workspace-id="ctx.activeWorkspace.id" />
          </div>
        </UCard>
      </div>

      <div v-for="section in sections" :key="section.gameId" class="mb-6">
        <h2 class="mb-2 flex items-center gap-1.5 text-sm font-semibold tracking-wide text-muted uppercase">
          <GameIcon :game-id="section.gameId" size="md" />
          {{ section.label }}
        </h2>
        <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
          <div v-for="ws in section.workspaces" :key="ws.id" class="cursor-pointer" @click="openWorkspace(ws)">
            <UCard class="h-full transition-shadow hover:shadow-md"
              :class="{ 'ring-2 ring-primary': ws.id === ctx.activeWorkspaceId }">
              <template #header>
                <div class="flex items-center justify-between gap-2">
                  <span class="truncate font-medium">{{ ws.name }}</span>
                  <div class="flex shrink-0 items-center gap-1">
                    <UButton label="Edit" size="xs" color="neutral" variant="outline"
                      @click.stop="editWorkspace(ws, $event)" />
                    <UButton label="Delete" size="xs" color="error" variant="ghost"
                      @click.stop="askDelete(ws, $event)" />
                    <UBadge color="neutral" variant="outline" size="xs" class="inline-flex items-center gap-1">
                      <GameIcon :game-id="ws.gameId" />
                      {{ ctx.shortName(ws.gameId) }}
                    </UBadge>
                  </div>
                </div>
              </template>
              <div class="space-y-2 text-xs text-muted">
                <p v-if="ws.installId">Game Install Configured</p>
                <p v-else class="text-warning">No install set</p>
                <UAvatarGroup v-if="ws.mods?.length" :max="5" size="sm">
                  <UAvatar v-for="(mod, i) in ws.mods" :key="mod.id" :src="ctx.thumbUrls[mod.id]" :alt="mod.name"
                    :style="{ boxShadow: `0 0 0 2px ${modColor(mod)}` }" />
                </UAvatarGroup>
              </div>
            </UCard>
          </div>
        </div>
      </div>
    </template>

    <div v-if="!ctx.loading" class="mt-auto space-y-2 border-t border-default pt-4">
      <p class="text-sm font-medium">Reset all PMT data</p>
      <p class="text-sm text-muted">
        Workspaces, install records, leftover folders under the PMT config tree,
        and caches. Does not delete mods or game files on disk.
      </p>
      <UButton label="Reset all data" color="error" variant="outline" size="sm" @click="resetOpen = true" />
    </div>

    <RemoveWorkspaceModal v-model:open="pendingOpen" :name="pending?.name ?? ''" :loading="removing"
      @confirm="removeWorkspace()" />

    <UModal v-model:open="newModOpen" title="New mod"
      description="Create a skeleton and attach it, or start a new workspace.">
      <template #body>
        <div class="space-y-3">
          <UFormField v-if="attachItems.length > 1" label="Add to workspace">
            <USelect v-model="attachWsId" :items="attachItems" value-key="value" />
          </UFormField>
          <CreateModForm v-if="attachWsId" :game-id="attachGameId" @created="onModCreated" />
        </div>
      </template>
      <template #footer>
        <UButton label="New workspace…" color="neutral" variant="outline" @click="
          newModOpen = false;
        void router.push({ name: 'wizard', query: { newMod: '1' } });
        " />
      </template>
    </UModal>

    <UModal v-model:open="resetOpen" title="Reset all Paradox Modding Tools data?">
      <template #body>
        <ul class="list-disc space-y-1 ps-4 text-sm text-muted">
          <li>Every workspace is removed from PMT.</li>
          <li>You must re-add game installs.</li>
          <li>The language cache is wiped; the next open rescans vanilla.</li>
          <li>Leftover folders under the PMT config tree are deleted.</li>
          <li>Theme, UI scale, and editor font are kept.</li>
          <li>Mod folders and Steam/game installs on disk are not deleted.</li>
        </ul>
        <UFormField class="mt-4" label="Type RESET to confirm">
          <UInput v-model="resetTyped" placeholder="RESET" />
        </UFormField>
      </template>
      <template #footer="{ close }">
        <UButton label="Cancel" color="neutral" variant="outline" @click="close" />
        <UButton label="Reset" color="error" :disabled="!canReset" :loading="resetting" @click="resetData()" />
      </template>
    </UModal>
  </div>
</template>
