<!-- The Library page: the workspaces as cards, the active one first, with New workspace, New mod and Launch. -->
<script setup lang="ts">
import { computed, onActivated, onDeactivated, onScopeDispose, shallowRef } from "vue";
import { useRouter } from "vue-router";
import { useMutation, useQuery } from "@pinia/colada";
import { Events } from "@wailsio/runtime";
import type { DropdownMenuItem } from "@nuxt/ui";

import { homeRoute, listShownTools } from "../../app/pages";
import { describeDumps, installLine, plural } from "../../lib/format";
import { gamesQuery, installsQuery, modsQuery, reportQuery, stateQuery, workspacesQuery } from "../../queries";
import { gameIcon } from "../../stores/settings";
import { useWorkspaceStore } from "../../stores/workspace";
import CreateModForm from "../../ui/CreateModForm.vue";
import ModAvatar from "../../ui/ModAvatar.vue";
import OriginChip from "../../ui/OriginChip.vue";
import ScriptDocsGuide from "../../ui/ScriptDocsGuide.vue";
import StatusFacts from "../../ui/StatusFacts.vue";
import * as workspace from "@services/workspace/service";
import type { Workspace } from "@services/app/models";

// the Health cards the active workspace's panel counts
const COUNTED = ["contests", "dangling", "localization"];

const router = useRouter();
const toast = useToast();
const store = useWorkspaceStore();
const creatingMod = shallowRef(false);
const guiding = shallowRef(false);
const resetting = shallowRef(false);
const confirmation = shallowRef("");
const visible = shallowRef(true);
onActivated(() => (visible.value = true));
onDeactivated(() => (visible.value = false));

const { data: workspaces, isPending } = useQuery(workspacesQuery);
const { data: installs } = useQuery(installsQuery);
const { data: games } = useQuery(gamesQuery);
const { data: mods } = useQuery(modsQuery);
const { data: state } = useQuery(stateQuery);
const { data: report } = useQuery(() => ({
  ...reportQuery((state.value?.mods ?? []).map((mod) => mod.id)),
  enabled: store.isOpen(),
}));

const visibleTools = computed(listShownTools);
const rows = computed(() =>
  (workspaces.value ?? [])
    .map((record) => {
      const install = installs.value?.find((one) => one.id === record.install);
      const game = games.value?.find((one) => one?.id === install?.game);
      return {
        record,
        install,
        game,
        active: record.id === store.activeId(),
        line: install ? installLine(game?.shortName ?? install.game, install.version, install.scannedAt) : "",
      };
    })
    .sort((a, b) => Number(b.active) - Number(a.active)),
);
const active = computed(() => rows.value.find((row) => row.active));
const groups = computed(() =>
  [...(games.value ?? []).filter((game) => game !== null), undefined]
    .map((game) => ({ game, rows: rows.value.filter((row) => row.game?.id === game?.id) }))
    .filter((group) => group.rows.length),
);
const summary = computed(
  () => `${plural(rows.value.length, "workspace")} across ${plural(groups.value.length, "game")}.`,
);
const dumps = computed(() => (store.isOpen() && state.value ? describeDumps(state.value.dumps) : undefined));
const counts = computed(() => report.value?.sections?.filter((section) => COUNTED.includes(section.id)) ?? []);

const { mutate: launch } = useMutation({ mutation: () => workspace.Launch() });
const { mutate: rescan } = useMutation({ mutation: (id: string) => workspace.RescanInstall(id) });
const { mutate: resetData, isLoading: resettingData } = useMutation({
  mutation: () => workspace.ResetData(),
  onSuccess: () => {
    resetting.value = false;
    confirmation.value = "";
    toast.add({ title: "PMT's data was reset", color: "success" });
  },
});

onScopeDispose(
  Events.On("game:exited", ({ data }) => {
    const install = active.value?.record.install;
    if (data && install) {
      toast.add({
        title: "The game wrote fresher script_docs",
        description: "Scan the install again to read them.",
        actions: [{ label: "Rescan the install", onClick: () => rescan(install) }],
      });
    }
  }),
);

/** open goes to the page the workspace opens on. The router makes the workspace the active one. */
function open(record: Workspace): void {
  void router.push(homeRoute(record.id, record.homePage, visibleTools.value));
}

/** openModDialog opens the new mod dialog for the active workspace. With none open it goes to the wizard instead. */
function openModDialog(): void {
  if (state.value?.open) creatingMod.value = true;
  else void router.push({ name: "wizard" });
}

/** describeMenu returns the entries of a workspace's menu: the given ones, then Remove. */
function describeMenu(record: Workspace, leading: DropdownMenuItem[]): DropdownMenuItem[] {
  return [
    ...leading,
    { label: "Remove from PMT…", icon: "i-lucide-trash-2", color: "error", onSelect: () => void store.remove(record) },
  ];
}
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
        <UButton label="New mod" size="md" @click="openModDialog" />
        <UButton
          label="New workspace"
          icon="i-lucide-plus"
          color="primary"
          variant="solid"
          size="md"
          :to="{ name: 'wizard' }"
        />
      </div>

      <UEmpty
        v-if="!isPending && !rows.length"
        icon="i-lucide-folder-plus"
        title="No workspaces yet"
        description="A workspace is one game install plus the mods you load with it."
        :actions="[
          { label: 'New workspace', icon: 'i-lucide-plus', color: 'primary', variant: 'solid', to: { name: 'wizard' } },
        ]"
      />

      <section v-if="active" class="flex flex-col gap-4 rounded-lg border border-accented bg-elevated p-5">
        <div class="flex items-center gap-3">
          <img v-if="active.game" :src="gameIcon(active.game.id)" :alt="active.game.name" class="size-9 rounded-lg" />
          <div class="flex min-w-0 flex-col gap-0.5">
            <div class="flex items-center gap-2">
              <span class="truncate text-16">{{ active.record.name }}</span>
              <UBadge label="Active" color="primary" />
            </div>
            <span class="text-12 text-dimmed">{{ active.line }}</span>
          </div>
          <span class="grow" />
          <UButton
            label="Open IDE"
            icon="i-lucide-code"
            color="primary"
            variant="solid"
            size="md"
            :to="{ name: 'workspace-ide', params: { id: active.record.id } }"
          />
          <UButton label="Health" size="md" :to="{ name: 'health', params: { id: active.record.id } }" />
          <UButton label="Settings" size="md" :to="{ name: 'workspace-settings', params: { id: active.record.id } }" />
          <UDropdownMenu
            :items="
              describeMenu(active.record, [
                { label: 'Launch the game', icon: 'i-lucide-play', onSelect: () => launch() },
              ])
            "
          >
            <UButton icon="i-lucide-ellipsis" size="md" square aria-label="More" />
          </UDropdownMenu>
        </div>
        <div class="grid grid-cols-3 gap-6">
          <div class="flex min-w-0 flex-col gap-2">
            <span class="text-12 font-medium text-toned">Load order</span>
            <div class="flex flex-wrap gap-1.5">
              <OriginChip origin="vanilla" />
              <OriginChip v-for="mod in mods?.[active.record.id] ?? []" :key="mod.id" :origin="mod.id" />
            </div>
          </div>
          <div class="flex min-w-0 flex-col gap-2">
            <span class="text-12 font-medium text-toned">Language</span>
            <button
              v-if="dumps"
              type="button"
              class="flex h-5 items-center gap-1.25 self-start text-12 text-toned hover:text-highlighted"
              @click="guiding = true"
            >
              <UIcon
                :name="dumps.current ? 'i-lucide-check' : 'i-lucide-triangle-alert'"
                class="size-3.5"
                :class="dumps.current ? 'text-success' : 'text-warning'"
              />
              {{ dumps.label }}
            </button>
            <span v-else class="text-12 text-dimmed">Read when the workspace opens.</span>
          </div>
          <div class="flex min-w-0 flex-col gap-2">
            <span class="text-12 font-medium text-toned">Health</span>
            <template v-if="counts.length">
              <div class="flex items-center gap-3.5 text-12">
                <span v-for="section in counts" :key="section.id" class="flex items-baseline gap-1">
                  <span class="font-semibold tabular-nums">{{ section.count }}</span>
                  <span class="text-dimmed">{{ section.label }}</span>
                </span>
              </div>
              <span class="text-12 text-dimmed">From the last scan. Counts refresh when files change.</span>
            </template>
            <span v-else class="text-12 text-dimmed">Counted once the workspace is open.</span>
          </div>
        </div>
      </section>

      <section v-for="group in groups" :key="group.game?.id ?? ''" class="flex flex-col gap-3">
        <div class="flex items-center gap-2.5 pt-2">
          <img v-if="group.game" :src="gameIcon(group.game.id)" :alt="group.game.name" class="size-4.5 rounded-sm" />
          <span class="text-13 font-semibold">{{ group.game?.name ?? "No game" }}</span>
          <span class="text-12 text-dimmed">{{ plural(group.rows.length, "workspace") }}</span>
        </div>
        <div class="grid grid-cols-3 gap-4">
          <div
            v-for="row in group.rows"
            :key="row.record.id"
            class="flex min-w-0 cursor-pointer flex-col gap-3 rounded-lg border bg-muted p-4 hover:border-accented"
            :class="row.active ? 'border-primary' : 'border-default'"
            role="link"
            tabindex="0"
            @click="open(row.record)"
            @keydown.enter="open(row.record)"
          >
            <div class="flex min-w-0 items-center gap-2">
              <span class="truncate text-13 font-semibold">{{ row.record.name }}</span>
              <span class="grow" />
              <UBadge v-if="row.active" label="Active" color="primary" />
              <UDropdownMenu
                v-else
                :items="
                  describeMenu(row.record, [
                    {
                      label: 'Settings',
                      icon: 'i-lucide-settings',
                      to: { name: 'workspace-settings', params: { id: row.record.id } },
                    },
                  ])
                "
              >
                <UButton icon="i-lucide-ellipsis" variant="ghost" size="sm" square aria-label="More" @click.stop />
              </UDropdownMenu>
            </div>
            <span v-if="!row.install" class="flex items-center gap-1.25 text-12 text-warning">
              <UIcon name="i-lucide-triangle-alert" class="size-3.5" />No install set
            </span>
            <span v-else class="text-12 text-dimmed">{{ row.line }}</span>
            <div class="flex flex-wrap gap-1.5">
              <UTooltip v-for="mod in mods?.[row.record.id] ?? []" :key="mod.id" :text="mod.descriptor.name">
                <ModAvatar :mod="mod" />
              </UTooltip>
              <span v-if="!mods?.[row.record.id]?.length" class="text-12 text-dimmed">No mods, game files only</span>
            </div>
          </div>
          <RouterLink
            :to="{ name: 'wizard', query: { game: group.game?.id } }"
            class="flex items-center justify-center gap-1.5 rounded-lg border border-dashed border-default p-4 text-dimmed hover:border-accented hover:text-toned"
          >
            <UIcon name="i-lucide-plus" class="size-4" />
            <span class="text-12 font-medium">New workspace for this game</span>
          </RouterLink>
        </div>
      </section>

      <UModal
        v-model:open="creatingMod"
        title="Create a mod"
        description="A descriptor, the game's mod folders, readmes and a localization file."
        :ui="{ content: 'h-128' }"
      >
        <template #body>
          <CreateModForm :workspace-id="state?.id ?? ''" @created="creatingMod = false" />
        </template>
        <template #footer="{ close }">
          <span class="grow" />
          <UButton label="Cancel" @click="close" />
        </template>
      </UModal>
      <ScriptDocsGuide v-model:open="guiding" :game="state?.game?.id ?? ''" />
    </div>

    <StatusFacts :facts="[{ label: 'Workspaces', value: `${rows.length}` }]" />
    <Teleport v-if="visible" defer to="#status-end">
      <UModal
        v-model:open="resetting"
        title="Reset all of PMT's data?"
        description="Mods, game files and your settings stay."
      >
        <button type="button" class="h-6 hover:text-highlighted">Reset all PMT data</button>
        <template #body>
          <ul class="flex list-disc flex-col gap-1 ps-4 text-13 text-toned">
            <li>Every workspace is removed from PMT, and every install record: add your installs again.</li>
            <li>The caches are cleared, so the next open scans the game again.</li>
          </ul>
          <UFormField label="Type RESET to confirm" class="mt-4">
            <UInput v-model="confirmation" placeholder="RESET" />
          </UFormField>
        </template>
        <template #footer="{ close }">
          <span class="grow" />
          <UButton label="Cancel" @click="close" />
          <UButton
            label="Reset all data"
            color="error"
            variant="soft"
            :disabled="confirmation !== 'RESET'"
            :loading="resettingData"
            @click="resetData()"
          />
        </template>
      </UModal>
    </Teleport>
  </div>
</template>
