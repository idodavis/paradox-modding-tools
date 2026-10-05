<script setup lang="ts">
/**
 * The Library (the Main board): the active workspace at the top, with its install, load order, language
 * and Health counts, then every workspace by game.
 */
import { computed, onActivated, shallowRef } from "vue";
import { useRouter } from "vue-router";
import { useMutation, useQuery, useQueryCache } from "@pinia/colada";
import { useTimeAgo } from "@vueuse/core";
import type { DropdownMenuItem } from "@nuxt/ui";
import { ResetData } from "@services/appservice";
import CreateModForm from "../components/CreateModForm.vue";
import RemoveWorkspaceModal from "../components/RemoveWorkspaceModal.vue";
import ScriptDocsGuide from "../components/ScriptDocsGuide.vue";
import { healthQuery, languageQuery, workspacesQuery } from "../queries";
import { modColor, useWorkspaceStore } from "../stores/workspace";

const router = useRouter();
const store = useWorkspaceStore();
const queryCache = useQueryCache();
const overlay = useOverlay();
const createMod = overlay.create(CreateModForm);
const removeModal = overlay.create(RemoveWorkspaceModal);
const guide = overlay.create(ScriptDocsGuide);

const { data: workspaces, isPending } = useQuery(workspacesQuery);
const active = computed(() => store.activeWorkspace);
const { data: language } = useQuery(() => languageQuery(active.value?.id ?? ""));
const { data: health, refresh } = useQuery(() =>
  healthQuery({ id: active.value?.id ?? "", live: !!language.value?.ready }),
);
// edits made since were indexed, so counts a while old are asked for again on return
onActivated(() => {
  if (active.value && language.value?.ready) void refresh();
});

const scanned = useTimeAgo(() => language.value?.scannedAt ?? 0);
/** "Crusader Kings III 1.14.2, install OK. Scanned 4 minutes ago." */
const installLine = computed(() => {
  const ws = active.value;
  const live = language.value;
  const name = [store.game(ws?.gameId ?? "")?.name, live?.gameVersion].filter(Boolean).join(" ");
  if (!ws?.installId) return `${name}, no install set.`;
  if (!live) return name;
  const state = `${name}, ${live.installOk ? "install OK" : "install not found"}.`;
  return live.scannedAt ? `${state} Scanned ${scanned.value}.` : state;
});

const games = computed(() =>
  store.games
    .map((game) => ({ game, workspaces: (workspaces.value ?? []).filter((w) => w.gameId === game.id) }))
    .filter((group) => group.workspaces.length),
);
const summary = computed(() => {
  const count = workspaces.value?.length ?? 0;
  const across = games.value.length;
  return `${count} ${count === 1 ? "workspace" : "workspaces"} across ${across} ${across === 1 ? "game" : "games"}.`;
});

/** Whose script_docs the live session reads: the game's own, current, or not. */
const dumps = computed(() => {
  const live = language.value;
  if (!live?.ready) return undefined;
  if (live.dumpsFrom === "yours" && !live.dumpsStale) return { ok: true, label: "script_docs loaded" };
  if (live.dumpsFrom === "yours") return { ok: false, label: "script_docs older than the game" };
  if (live.dumpsFrom === "shipped") return { ok: false, label: "Using PMT's copy of script_docs" };
  return { ok: false, label: "No script_docs" };
});

const counts = computed(() => {
  const report = health.value;
  if (!report) return [];
  const gaps = (report.loc ?? []).reduce((sum, language) => sum + (language.untranslated?.length ?? 0), 0);
  return [
    { count: report.contests?.length ?? 0, label: "contests" },
    { count: report.dangling?.length ?? 0, label: "dangling" },
    { count: gaps, label: "loc gaps" },
  ];
});

const more = computed<DropdownMenuItem[]>(() => [
  {
    label: "Remove from PMT…",
    icon: "i-lucide-trash-2",
    color: "error",
    onSelect: () => void removeActive(),
  },
]);

async function removeActive(): Promise<void> {
  const ws = active.value;
  if (!ws || !(await removeModal.open({ id: ws.id, name: ws.name }))) return;
  store.select(workspaces.value?.find((w) => w.id !== ws.id)?.id ?? "");
}

function newMod(): void {
  const ws = active.value;
  if (!ws) {
    void router.push({ name: "wizard" });
    return;
  }
  void createMod.open({
    gameId: ws.gameId,
    workspaceId: ws.id,
    locLang: ws.defaultLocLang || "english",
    supportedVersion: language.value?.gameVersion ?? "",
  });
}

const toast = useToast();
const resetOpen = shallowRef(false);
const resetTyped = shallowRef("");
const { mutate: resetData, isLoading: resetting } = useMutation({
  mutation: () => ResetData(),
  onSuccess: () => {
    resetOpen.value = false;
    toast.add({ title: "PMT's data was reset", color: "success" });
  },
  onSettled: () => queryCache.invalidateQueries(),
});
</script>

<template>
  <div class="flex h-full justify-center overflow-y-auto px-6 py-8">
    <div class="flex w-280 max-w-full flex-col gap-7">
      <div class="flex items-center gap-4">
        <div class="flex min-w-0 flex-col gap-0.5">
          <h1 class="text-20">Workspaces</h1>
          <p class="text-13 text-dimmed">
            {{ summary }} A workspace is one game install plus the mods you load with it.
          </p>
        </div>
        <span class="grow" />
        <UButton label="New mod" size="md" @click="newMod" />
        <UButton label="New workspace" icon="i-lucide-plus" color="primary" variant="solid" size="md" :to="{ name: 'wizard' }" />
      </div>

      <UEmpty
        v-if="!isPending && !workspaces?.length"
        icon="i-lucide-folder-plus"
        title="No workspaces yet"
        description="A workspace is one game install plus the mods you load with it."
        :actions="[{ label: 'New workspace', icon: 'i-lucide-plus', color: 'primary', variant: 'solid', to: { name: 'wizard' } }]"
      />

      <section v-if="active" class="flex flex-col gap-4 rounded-lg border border-accented bg-elevated p-5">
        <div class="flex items-center gap-3">
          <GameIcon :game-id="active.gameId" size="lg" />
          <div class="flex min-w-0 flex-col gap-0.5">
            <div class="flex items-center gap-2">
              <span class="truncate text-16">{{ active.name }}</span>
              <UBadge label="Active" color="primary" />
            </div>
            <span class="text-12 text-dimmed">{{ installLine }}</span>
          </div>
          <span class="grow" />
          <UButton
            label="Open IDE"
            icon="i-lucide-code"
            color="primary"
            variant="solid"
            size="md"
            :to="{ name: 'workspace-ide', params: { id: active.id } }"
          />
          <UButton label="Health" size="md" :to="{ name: 'health', params: { id: active.id } }" />
          <UButton label="Settings" size="md" :to="{ name: 'workspace-settings', params: { id: active.id } }" />
          <UDropdownMenu :items="more">
            <UButton icon="i-lucide-ellipsis" size="md" square aria-label="More" />
          </UDropdownMenu>
        </div>
        <div class="grid grid-cols-3 gap-6">
          <div class="flex min-w-0 flex-col gap-2">
            <span class="text-12 font-medium text-toned">Load order</span>
            <div class="flex flex-wrap gap-1.5">
              <OriginChip :label="store.game(active.gameId)?.name ?? ''" :color="store.originColor(store.originVanilla)" />
              <OriginChip v-for="mod in active.mods ?? []" :key="mod.id" :label="mod.name" :color="modColor(mod)" />
            </div>
          </div>
          <div class="flex min-w-0 flex-col gap-2">
            <span class="text-12 font-medium text-toned">Language</span>
            <button
              v-if="dumps"
              type="button"
              class="flex h-5 items-center gap-1.25 self-start text-12 text-toned hover:text-highlighted"
              @click="guide.open({ folders: language?.dumpFolders ?? [] })"
            >
              <UIcon
                :name="dumps.ok ? 'i-lucide-check' : 'i-lucide-triangle-alert'"
                class="size-3.5"
                :class="dumps.ok ? 'text-success' : 'text-warning'"
              />
              {{ dumps.label }}
            </button>
            <span v-else class="text-12 text-dimmed">Read when the workspace opens.</span>
          </div>
          <div class="flex min-w-0 flex-col gap-2">
            <span class="text-12 font-medium text-toned">Health</span>
            <template v-if="counts.length">
              <div class="flex items-center gap-3.5 text-12">
                <span v-for="row in counts" :key="row.label" class="flex items-baseline gap-1">
                  <span class="font-semibold tabular-nums">{{ row.count }}</span>
                  <span class="text-dimmed">{{ row.label }}</span>
                </span>
              </div>
              <span class="text-12 text-dimmed">From the last scan. Counts refresh when files change.</span>
            </template>
            <span v-else class="text-12 text-dimmed">Counted once the workspace is open.</span>
          </div>
        </div>
      </section>

      <section v-for="group in games" :key="group.game.id" class="flex flex-col gap-3">
        <div class="flex items-center gap-2.5 pt-2">
          <GameIcon :game-id="group.game.id" />
          <span class="text-13 font-semibold">{{ group.game.name }}</span>
          <span class="text-12 text-dimmed">
            {{ group.workspaces.length }} {{ group.workspaces.length === 1 ? "workspace" : "workspaces" }}
          </span>
        </div>
        <div class="grid grid-cols-3 gap-4">
          <WorkspaceCard
            v-for="ws in group.workspaces"
            :key="ws.id"
            :workspace="ws"
            :active="ws.id === store.activeWorkspaceId"
          />
          <RouterLink
            :to="{ name: 'wizard', query: { game: group.game.id } }"
            class="flex items-center justify-center gap-1.5 rounded-lg border border-dashed border-default p-4 text-dimmed
              hover:border-accented hover:text-toned"
          >
            <UIcon name="i-lucide-plus" class="size-4" />
            <span class="text-12 font-medium">New workspace for this game</span>
          </RouterLink>
        </div>
      </section>
    </div>

    <StatusFacts>
      <span>{{ workspaces?.length ?? 0 }} {{ workspaces?.length === 1 ? "workspace" : "workspaces" }}</span>
    </StatusFacts>
    <StatusFacts end>
      <UModal
        v-model:open="resetOpen"
        title="Reset all of PMT's data?"
        description="Mods, game files and your appearance settings stay."
      >
        <button type="button" class="h-6 hover:text-highlighted">Reset all PMT data</button>
        <template #body>
          <ul class="flex list-disc flex-col gap-1 ps-4 text-13 text-toned">
            <li>Every workspace is removed from PMT, and every install record: add your installs again.</li>
            <li>The language cache is cleared, so the next open scans the game again.</li>
            <li>Leftover folders in PMT's settings folder are deleted.</li>
          </ul>
          <UFormField label="Type RESET to confirm" class="mt-4">
            <UInput v-model="resetTyped" placeholder="RESET" />
          </UFormField>
        </template>
        <template #footer="{ close }">
          <span class="grow" />
          <UButton label="Cancel" @click="close" />
          <UButton
            label="Reset all data"
            color="error"
            variant="soft"
            :disabled="resetTyped !== 'RESET'"
            :loading="resetting"
            @click="resetData()"
          />
        </template>
      </UModal>
    </StatusFacts>
  </div>
</template>
