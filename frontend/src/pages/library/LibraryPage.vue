<!--
  The Library page shows the workspaces as cards, the active one first, with the first-mod checklist on its card. With
  none, it offers the two ways into modding and the game installs found on this computer.
-->
<script setup lang="ts">
import { computed, shallowRef } from "vue";
import { useRouter } from "vue-router";
import { useMutation, useQuery } from "@pinia/colada";
import { useEventBus } from "@vueuse/core";
import type { DropdownMenuItem } from "@nuxt/ui";

import { findCommand } from "../../app/commands";
import { homeRoute, useShown } from "../../app/pages";
import { describeDumps, describeLexicon, installLine, plural } from "../../lib/format";
import { gamesQuery, reportQuery, stateQuery, workspacesQuery } from "../../queries";
import { gameIcon } from "../../stores/settings";
import { COMMAND_BUS } from "../../stores/windows";
import { useWorkspaceStore, VANILLA } from "../../stores/workspace";
import CreateModForm from "../../ui/CreateModForm.vue";
import FirstModChecklist from "../../ui/FirstModChecklist.vue";
import GameInstalls from "../../ui/GameInstalls.vue";
import LevelCounts from "../../ui/LevelCounts.vue";
import ModAvatar from "../../ui/ModAvatar.vue";
import OriginChip from "../../ui/OriginChip.vue";
import ScriptDocsGuide from "../../ui/ScriptDocsGuide.vue";
import StatusFacts from "../../ui/StatusFacts.vue";
import TermInfo from "../../ui/TermInfo.vue";
import * as workspace from "@services/workspace/service";
import type { Workspace } from "@services/app/models";

const LAUNCH = findCommand("game.launch");

const router = useRouter();
const toast = useToast();
const store = useWorkspaceStore();
const commandBus = useEventBus(COMMAND_BUS);
const creatingMod = shallowRef(false);
const guiding = shallowRef(false);
const resetting = shallowRef(false);
const confirmation = shallowRef("");
const visible = useShown();

const { data: workspaces, isPending } = useQuery(workspacesQuery);
const { data: games } = useQuery(gamesQuery);
const { data: state } = useQuery(stateQuery);
const { data: report } = useQuery(() => ({ ...reportQuery(store.listModIds()), enabled: store.isOpen() }));

const rows = computed(() =>
  (workspaces.value ?? [])
    .map((row) => ({
      ...row,
      active: row.record.id === store.activeId(),
      line: row.install.id ? installLine(row.install.name, row.install.version, row.install.scannedAt) : "",
    }))
    .sort((first, second) => Number(second.active) - Number(first.active)),
);
const active = computed(() => rows.value.find((row) => row.active));
const groups = computed(() =>
  [...(games.value ?? []), undefined]
    .map((game) => ({ game, rows: rows.value.filter((row) => row.game?.id === game?.id) }))
    .filter((group) => group.rows.length),
);
const summary = computed(
  () => `${plural(rows.value.length, "workspace")} across ${plural(groups.value.length, "game")}.`,
);
const dumps = computed(() => (store.isOpen() && state.value ? describeDumps(state.value.dumps) : undefined));
// The dump folders are left out when there are none, as when PMT reads its own copy.
const lexicon = computed(() => {
  if (!state.value) return "";
  const count = state.value.dumps.folders?.length ?? 0;
  const folders = count ? `, from ${plural(count, "dump folder")}` : "";
  return `${describeLexicon(state.value)}${folders}.`;
});

const { mutate: resetData, isLoading: resettingData } = useMutation({
  mutation: () => workspace.ResetData(),
  onSuccess: () => {
    resetting.value = false;
    confirmation.value = "";
    toast.add({ title: "PMT's data was reset", color: "success" });
  },
});

/** open goes to a workspace's home page, which makes it the active one. It returns the failure if cancelled. */
function open(record: Workspace) {
  return router.push(homeRoute(record.id, record.homePage));
}

/** openSettings opens Settings on a workspace. A workspace that is not the open one is opened first. */
async function openSettings(record: Workspace): Promise<void> {
  const isCurrent = record.id === store.activeId() && store.isOpen();
  if (!isCurrent && (await open(record))) return;
  commandBus.emit("settings.open");
}

/** openModDialog opens the new mod dialog, or Make a new mod if no workspace is open. */
function openModDialog(): void {
  if (state.value?.open) creatingMod.value = true;
  else void router.push({ name: "new-mod" });
}

/** describeMenu returns a workspace's menu: the given leading items, then Remove. */
function describeMenu(record: Workspace, leading: DropdownMenuItem[]): DropdownMenuItem[] {
  return [
    ...leading,
    { label: "Remove from PMT…", icon: "i-lucide-trash-2", color: "error", onSelect: () => void store.remove(record) },
  ];
}
</script>

<template>
  <div class="flex h-full justify-center overflow-y-auto px-6 py-6">
    <div class="flex w-280 max-w-full flex-col gap-6 self-start">
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

      <template v-if="!isPending && !rows.length">
        <section class="grid grid-cols-2 gap-4">
          <div class="flex flex-col gap-3.5 rounded-lg border border-default bg-muted p-6">
            <UIcon name="i-lucide-file-plus" class="size-5 text-dimmed" />
            <div class="flex flex-col gap-1">
              <h2 class="text-16">Make a new mod</h2>
              <p class="text-13 text-toned">
                Pick the game and name your mod. PMT makes its folders, its descriptor and a localization file, then
                opens it in the IDE.
              </p>
            </div>
            <div class="flex flex-wrap gap-3.5">
              <span v-for="game in games" :key="game.id" class="flex items-center gap-1.5 text-12 text-toned">
                <img :src="gameIcon(game.id)" :alt="game.name" class="size-4.5 rounded-sm" />{{ game.name }}
              </span>
            </div>
            <UButton
              label="Make a new mod"
              trailing-icon="i-lucide-arrow-right"
              color="primary"
              variant="solid"
              size="md"
              class="self-start"
              :to="{ name: 'new-mod' }"
            />
          </div>
          <div class="flex flex-col gap-3.5 rounded-lg border border-default bg-muted p-6">
            <UIcon name="i-lucide-folder" class="size-5 text-dimmed" />
            <div class="flex flex-col gap-1">
              <h2 class="text-16">Work on a mod I have</h2>
              <p class="text-13 text-toned">
                Choose your mod's folder. PMT reads it with its game's files and opens Health, which lists its problems.
              </p>
            </div>
            <UButton
              label="Choose a mod folder"
              size="md"
              class="mt-auto self-start"
              :to="{ name: 'new-mod', query: { goal: 'fix' } }"
            />
          </div>
        </section>

        <section class="flex flex-col gap-2.5">
          <div class="flex items-center gap-2">
            <span class="text-13 font-semibold">Found on this computer</span>
            <span class="text-12 text-dimmed">
              PMT reads each game's files from its install. Nothing is changed in them.
            </span>
          </div>
          <div class="flex flex-col divide-y divide-default rounded-lg border border-default bg-muted">
            <GameInstalls v-for="game in games" v-slot="{ recorded, detected }" :key="game.id" :game-id="game.id">
              <div
                v-for="install in [...recorded, ...detected]"
                :key="install.path"
                class="flex h-11 items-center gap-2.5 px-3.5"
              >
                <img :src="gameIcon(game.id)" :alt="game.name" class="size-4.5 rounded-sm" />
                <span class="w-45 shrink-0 text-13 font-medium">{{ game.name }}</span>
                <span class="w-20 shrink-0 text-12 text-dimmed">{{ install.version }}</span>
                <span class="min-w-0 grow truncate font-mono text-12 text-dimmed">{{ install.path }}</span>
              </div>
            </GameInstalls>
          </div>
          <p class="text-12 text-dimmed">
            To pick an install, its language and several mods yourself, use
            <RouterLink :to="{ name: 'wizard' }" class="text-secondary hover:underline">New workspace</RouterLink>.
          </p>
        </section>
      </template>

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
          <UButton label="Settings" size="md" @click="openSettings(active.record)" />
          <UDropdownMenu
            :items="
              describeMenu(active.record, [
                { label: 'Launch the game', icon: 'i-lucide-play', onSelect: (event) => LAUNCH?.onSelect?.(event) },
              ])
            "
          >
            <UButton icon="i-lucide-ellipsis" size="md" square aria-label="More" />
          </UDropdownMenu>
        </div>
        <div v-if="store.failure?.id === active.record.id" class="flex items-start gap-2 text-12 text-error">
          <UIcon name="i-lucide-circle-alert" class="mt-0.5 size-3.5 shrink-0" />
          <span class="min-w-0 grow whitespace-pre-wrap">{{ store.failure.reason }}</span>
          <UButton label="Try again" size="xs" @click="store.open(active.record.id)" />
        </div>
        <div class="grid grid-cols-3 gap-6">
          <div class="flex min-w-0 flex-col gap-2">
            <span class="flex items-center gap-1 text-12 font-medium text-toned">
              Load order
              <TermInfo term="load order" />
            </span>
            <div class="flex flex-wrap gap-1.5">
              <OriginChip :origin="VANILLA" />
              <OriginChip v-for="mod in active.mods ?? []" :key="mod.id" :origin="mod.id" />
            </div>
          </div>
          <div class="flex min-w-0 flex-col gap-2">
            <span class="flex items-center gap-1 text-12 font-medium text-toned">
              Game knowledge
              <TermInfo term="game documentation" />
            </span>
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
            <span v-if="dumps && !dumps.current" class="text-12 text-dimmed">
              It may be older than {{ state?.install.version || "the game" }}.
              <UButton
                label="Get the game's own"
                variant="link"
                color="secondary"
                size="xs"
                class="p-0"
                @click="guiding = true"
              />
            </span>
            <span v-else class="text-12 text-dimmed">{{ dumps ? lexicon : "Read when the workspace opens." }}</span>
          </div>
          <div class="flex min-w-0 flex-col gap-2">
            <span class="text-12 font-medium text-toned">Health</span>
            <template v-if="report">
              <LevelCounts :levels="report.counts" />
              <span class="text-12 text-dimmed">From the last scan.</span>
            </template>
            <span v-else class="text-12 text-dimmed">Counted once the workspace is open.</span>
          </div>
        </div>
        <FirstModChecklist />
      </section>

      <section v-for="group in groups" :key="group.game?.id ?? ''" class="flex flex-col gap-3">
        <div class="flex items-center gap-2.5">
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
                      onSelect: () => openSettings(row.record),
                    },
                  ])
                "
              >
                <UButton icon="i-lucide-ellipsis" variant="ghost" size="sm" square aria-label="More" @click.stop />
              </UDropdownMenu>
            </div>
            <span v-if="!row.install.id" class="flex items-center gap-1.25 text-12 text-warning">
              <UIcon name="i-lucide-triangle-alert" class="size-3.5" />No install set
            </span>
            <span v-else class="text-12 text-dimmed">{{ row.line }}</span>
            <div class="flex flex-wrap gap-1.5">
              <UTooltip v-for="mod in row.mods ?? []" :key="mod.id" :text="mod.descriptor.name">
                <ModAvatar :mod="mod" />
              </UTooltip>
              <span v-if="!row.mods?.length" class="text-12 text-dimmed">No mods, game files only</span>
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

      <CreateModForm v-model:open="creatingMod" :workspace-id="state?.id ?? ''" />
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
