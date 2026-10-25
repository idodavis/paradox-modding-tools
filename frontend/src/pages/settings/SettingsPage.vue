<!-- The Settings page: a section per part of the catalog, the mods' cards, the theme preview and the shortcuts. -->
<script setup lang="ts">
import { computed, shallowRef, useTemplateRef, watch } from "vue";
import { useRouter } from "vue-router";
import { useMutation, useQuery } from "@pinia/colada";
import { useSortable } from "@vueuse/integrations/useSortable";
import type { TabsItem } from "@nuxt/ui";

import { COMMANDS } from "../../app/commands";
import { TOOL_CHOICES } from "../../app/pages";
import { describeDumps, describeLanguages } from "../../lib/format";
import { languagesQuery, settingsCatalogQuery, stateQuery, workspacesQuery } from "../../queries";
import { gameIcon, THEME_FAMILIES, useSettingsStore } from "../../stores/settings";
import { useWorkspaceStore } from "../../stores/workspace";
import CreateModForm from "../../ui/CreateModForm.vue";
import FileSelector from "../../ui/FileSelector.vue";
import InstallPicker from "../../ui/InstallPicker.vue";
import OriginChip from "../../ui/OriginChip.vue";
import ScriptDocsGuide from "../../ui/ScriptDocsGuide.vue";
import StatusFacts from "../../ui/StatusFacts.vue";
import ValueField from "../../ui/ValueField.vue";
import SettingsSection from "../../ui/SettingsSection.vue";
import ThemePreview from "./ThemePreview.vue";
import WorkspaceModCard from "./WorkspaceModCard.vue";
import * as workspace from "@services/workspace/service";
import type { Workspace } from "@services/app/models";
import type { Mod } from "@services/workspace/models";

/** The sections in the order they are listed, grouped by what they apply to. */
const GROUPS = [
  {
    label: "Workspace",
    mark: "i-lucide-folder",
    sections: [
      { value: "general", label: "General" },
      { value: "install", label: "Game install" },
      { value: "mods", label: "Mods and load order" },
    ],
  },
  {
    label: "App",
    mark: "i-lucide-monitor",
    sub: "Every workspace on this computer",
    sections: [
      { value: "appearance", label: "Appearance" },
      { value: "shortcuts", label: "Keyboard shortcuts" },
    ],
  },
];
/** The commands that have a keyboard shortcut, in the command table's order. */
const SHORTCUTS = COMMANDS.filter((command) => command.kbds?.length);
const HEADING = {
  ui: {
    trigger:
      "mt-3.5 h-auto rounded-none border-t border-default pt-3 ps-2.5 pb-1.5 disabled:cursor-default disabled:opacity-100",
    label: "flex flex-col items-start gap-0.5 overflow-hidden",
  },
};
const NOTES: Record<string, string> = {
  pmt: "PMT's own",
  catppuccin: "Mocha and Latte",
  one: "One Dark and One Light",
  pierre: "Pierre Dark Soft and Pierre Light Soft",
  ck3: "CK3 workspaces",
  eu5: "EU5 workspaces",
  vic3: "Vic3 workspaces",
};
// the load order and the mod picked; the splitter keeps the list's width
const PANES = [
  { slot: "list", sizeUnit: "px" as const, defaultSize: 420, minSize: 320, maxSize: 640 },
  { slot: "card" },
];
const ROW = "relative flex h-14 min-w-0 items-center gap-2.5 border-b border-default ps-2 pe-3 text-start";
const PICKED = "bg-(--accent-soft) before:absolute before:inset-y-0 before:start-0 before:w-0.5 before:bg-primary";
/** The origin of the game's own files, which is also what the game's row in the load order picks. */
const GAME = "vanilla";

const { id, section = "general" } = defineProps<{ id: string; section?: string }>();

const router = useRouter();
const settings = useSettingsStore();
const store = useWorkspaceStore();
const search = shallowRef("");
const creating = shallowRef(false);
const guiding = shallowRef(false);
const origin = shallowRef("");
const preview = shallowRef<Mod[]>();

const { data: state } = useQuery(stateQuery);
const { data: workspaces } = useQuery(workspacesQuery);
const { data: catalog } = useQuery(settingsCatalogQuery);

const record = computed(() => workspaces.value?.find((row) => row.id === id));
const { data: languages } = useQuery(() => ({
  ...languagesQuery(record.value?.install ?? ""),
  enabled: !!record.value?.install,
}));
const mods = computed(() => state.value?.mods ?? []);
const selected = computed(
  () => mods.value.find((mod) => mod.id === origin.value) ?? (origin.value === GAME ? undefined : mods.value[0]),
);
const order = computed({
  get: () => preview.value ?? mods.value,
  set: (next) => {
    preview.value = next;
    reorder(next.map((mod) => mod.id));
  },
});
const dumps = computed(() => state.value && describeDumps(state.value.dumps));
const custom = computed(() => !!record.value?.gameColor || mods.value.some((mod) => store.isCustom(mod.id)));

// a section is listed when its title or one of its settings matches what was typed
const sections = computed(() => {
  const needle = search.value.trim().toLowerCase();
  const matches = (one: { value: string; label: string }) =>
    !needle ||
    [
      one.label,
      ...(catalog.value ?? []).filter((row) => row.section === one.value).map((row) => `${row.label} ${row.words}`),
    ].some((text) => text.toLowerCase().includes(needle));
  return GROUPS.map((group) => ({ ...group, sections: group.sections.filter(matches) })).filter(
    (group) => group.sections.length,
  );
});
const listed = computed(() => sections.value.flatMap((group) => group.sections));
const tabs = computed<TabsItem[]>(() =>
  sections.value.flatMap((group) => [
    {
      label: group.label,
      mark: group.mark,
      sub: group.sub ?? record.value?.name,
      value: group.label,
      disabled: true,
      ...HEADING,
    },
    ...group.sections.map((one) => ({ label: one.label, value: one.value, slot: one.value })),
  ]),
);
const current = computed({
  get: () => (listed.value.find((one) => one.value === section) ?? listed.value[0])?.value ?? "general",
  set: (value: string | number) => void router.replace({ query: { section: `${value}` } }),
});

useSortable(useTemplateRef<HTMLElement>("list"), order, { handle: ".mod-handle", animation: 150, watchElement: true });
watch(mods, () => (preview.value = undefined));

const { mutate: update } = useMutation({ mutation: (next: Workspace) => workspace.UpdateWorkspace(next) });
const { mutate: reorder } = useMutation({
  mutation: (ids: string[]) => workspace.ReorderMods(id, ids),
  onError: () => (preview.value = undefined),
});
const { mutate: addMod } = useMutation({ mutation: (dir: string) => workspace.AddMod(id, dir) });
const { mutate: updateMod } = useMutation({
  mutation: ({ modId, dir }: { modId: string; dir: string }) => {
    const latest = record.value?.mods?.find((mod) => mod.id === modId);
    return latest ? workspace.UpdateMod(id, { ...latest, dir }) : Promise.resolve();
  },
});
const { mutate: resetColors } = useMutation({ mutation: () => workspace.ResetColors(id) });
const { mutate: rescan } = useMutation({ mutation: () => workspace.RescanInstall(state.value?.install.id ?? "") });

/** remove removes the workspace after asking, then goes to the Library. Cancelling stays on the page. */
async function remove(): Promise<void> {
  if (record.value && (await store.remove(record.value))) void router.push({ name: "library" });
}
</script>

<template>
  <UTabs
    v-model="current"
    :items="tabs"
    orientation="vertical"
    class="h-full"
    :ui="{
      root: 'h-full items-stretch gap-0',
      list: 'h-full w-62 shrink-0 items-stretch gap-0.5 overflow-y-auto rounded-none border-e border-default bg-muted px-2.5 py-3.5 ring-0',
      indicator:
        'inset-x-2.5 overflow-hidden rounded-md bg-elevated before:absolute before:inset-y-0 before:start-0 before:w-0.5 before:bg-primary',
      trigger:
        'h-7.5 w-full justify-start rounded-md ps-8.5 pe-2.5 text-13 font-normal text-toned data-[state=active]:font-medium',
      content: 'h-full min-w-0 flex-1 overflow-hidden',
    }"
  >
    <template #list-leading>
      <UInput v-model="search" icon="i-lucide-search" placeholder="Find a setting" size="sm" class="w-full" />
    </template>
    <template #default="{ item }">
      <template v-if="'mark' in item">
        <span class="flex items-center gap-2 text-13 font-semibold text-highlighted">
          <UIcon :name="`${item.mark}`" class="size-4" />{{ item.label }}
        </span>
        <span class="max-w-full truncate ps-6 text-11 font-normal text-dimmed">{{ item.sub }}</span>
      </template>
      <template v-else>{{ item.label }}</template>
    </template>
    <template #list-trailing>
      <span class="grow" />
      <button
        type="button"
        class="flex h-7.5 items-center gap-2 rounded-md px-2.5 text-13 text-error hover:bg-elevated"
        @click="remove"
      >
        <UIcon name="i-lucide-trash-2" class="size-3.5" />Remove this workspace
      </button>
    </template>

    <template #general>
      <div v-if="record" class="flex h-full flex-col gap-4 overflow-y-auto px-8 py-6">
        <div class="flex flex-col gap-0.5">
          <h1 class="text-20">General</h1>
          <p class="text-13 text-dimmed">This workspace's name, language and the IDE's start.</p>
        </div>
        <div class="divide-y divide-default">
          <UFormField
            label="Name"
            description="Shown in the Library and the workspace switcher."
            orientation="horizontal"
          >
            <UInput
              :model-value="record.name"
              :model-modifiers="{ lazy: true }"
              class="w-80"
              @update:model-value="update({ ...record, name: `${$event}`.trim() || record.name })"
            />
          </UFormField>
          <UFormField
            label="Localization language"
            description="Used for localization gaps, hover text and new mods' localization files."
            orientation="horizontal"
          >
            <USelect
              :model-value="record.language"
              :items="describeLanguages(languages)"
              placeholder="The game's base language"
              class="w-60"
              @update:model-value="update({ ...record, language: $event })"
            />
          </UFormField>
          <UFormField
            label="Page it opens on"
            description="Where the workspace opens from the Library and the switcher."
            orientation="horizontal"
          >
            <USelect
              :model-value="record.homePage || 'workspace-ide'"
              :items="TOOL_CHOICES"
              class="w-60"
              @update:model-value="update({ ...record, homePage: $event })"
            />
          </UFormField>
          <UFormField
            label="Reopen the IDE's tabs"
            description="Opens the tabs that were open when the workspace was last closed."
            orientation="horizontal"
          >
            <USwitch :model-value="!record.resetTabs" @update:model-value="update({ ...record, resetTabs: !$event })" />
          </UFormField>
          <UFormField
            label="Hide binaries in the explorer"
            description="Leaves images, fonts and other binary files out of the explorer's tree."
            orientation="horizontal"
          >
            <USwitch
              :model-value="!record.showBinaries"
              @update:model-value="update({ ...record, showBinaries: !$event })"
            />
          </UFormField>
        </div>
      </div>
    </template>

    <template #install>
      <div v-if="state?.open && record" class="flex h-full flex-col gap-5 overflow-y-auto px-8 py-6">
        <div class="flex flex-col gap-0.5">
          <h1 class="text-20">Game install</h1>
          <p class="text-13 text-dimmed">
            The {{ state.game?.name }} files this workspace reads. Changing it reads the workspace again.
          </p>
        </div>
        <InstallPicker
          :model-value="record.install"
          :game-id="state.game?.id ?? ''"
          @update:model-value="update({ ...record, install: $event })"
        />
        <p v-if="!state.installFound" class="text-12 text-error">
          The install's folder was not found. Pick where the game is now with Change folder.
        </p>
        <div class="flex flex-col gap-3 border-t border-default pt-5">
          <div class="flex flex-col gap-0.75">
            <h2 class="text-13 font-semibold">script_docs</h2>
            <p class="text-12 text-dimmed">
              The game prints its own scripting documentation once you ask it to. PMT reads it rather than guessing, and
              keeps its own copy for when it is missing.
            </p>
          </div>
          <UBadge :label="dumps?.label" :color="dumps?.current ? 'success' : 'warning'" class="self-start" />
          <div class="flex gap-2">
            <UButton label="How to make script_docs" icon="i-lucide-book-open" @click="guiding = true" />
            <UButton label="Rescan the install" icon="i-lucide-refresh-cw" @click="rescan()" />
          </div>
        </div>
        <ScriptDocsGuide v-model:open="guiding" :game="state.game?.id ?? ''" />
      </div>
    </template>

    <template #mods>
      <div v-if="state?.open && record" class="flex h-full flex-col px-8 py-6">
        <div class="flex items-center gap-3 pb-4">
          <div class="flex min-w-0 flex-col gap-0.5">
            <h1 class="text-20">Mods and load order</h1>
            <p class="text-13 text-dimmed">Mods load top to bottom after the game.</p>
          </div>
          <span class="grow" />
          <UButton label="Create a new mod" size="md" @click="creating = true" />
          <FileSelector path="" folder :filter="[]" @update:path="addMod">
            <template #default="{ browse }">
              <UButton label="Add an existing mod" icon="i-lucide-plus" size="md" @click="browse" />
            </template>
          </FileSelector>
        </div>
        <USplitter :items="PANES" auto-save-id="pmt.settings.mods" class="min-h-0 grow" :ui="{ handle: 'mx-2.5' }">
          <template #list>
            <div class="flex min-w-0 flex-1 flex-col overflow-hidden rounded-lg border border-default bg-muted">
              <div class="flex h-10 shrink-0 items-center border-b border-default px-3.5">
                <span class="text-13 font-semibold">Load order</span>
                <span class="grow" />
                <span class="text-12 text-dimmed">Drag to reorder</span>
              </div>
              <div class="min-h-0 grow overflow-y-auto">
                <button
                  type="button"
                  class="w-full text-dimmed"
                  :class="[ROW, selected ? 'hover:bg-elevated' : PICKED]"
                  @click="origin = GAME"
                >
                  <span class="w-3.5 shrink-0" />
                  <span class="w-3.5 shrink-0 text-center font-mono text-11">0</span>
                  <span
                    class="size-5.5 shrink-0 rounded-md border border-accented"
                    :style="{ background: store.originColor(GAME) }"
                  />
                  <span class="flex min-w-0 grow flex-col">
                    <span class="truncate text-13 font-medium text-toned">{{ state.game?.name }}</span>
                    <span class="truncate text-11">Game install, read only</span>
                  </span>
                  <UIcon name="i-lucide-lock" class="size-3.5" />
                </button>
                <div ref="list">
                  <div
                    v-for="(mod, index) in order"
                    :key="mod.id"
                    role="button"
                    tabindex="0"
                    :class="[ROW, selected?.id === mod.id ? PICKED : 'hover:bg-elevated']"
                    @click="origin = mod.id"
                    @keydown.enter="origin = mod.id"
                  >
                    <UIcon name="i-lucide-grip-vertical" class="mod-handle size-3.5 shrink-0 cursor-grab text-dimmed" />
                    <span class="w-3.5 shrink-0 text-center font-mono text-11 text-dimmed">{{ index + 1 }}</span>
                    <span
                      class="size-5.5 shrink-0 rounded-md border border-accented"
                      :style="{ background: store.originColor(mod.id) }"
                    />
                    <span class="flex min-w-0 grow flex-col">
                      <span class="truncate text-13 font-medium">{{ mod.descriptor.name }}</span>
                      <span v-if="mod.broken" class="truncate text-11 text-error">Folder not found</span>
                      <span v-else class="truncate text-11 text-dimmed">
                        {{ mod.descriptor.version }} · {{ mod.workshopId ? "On Steam" : "Not published" }}
                      </span>
                    </span>
                    <FileSelector
                      v-if="mod.broken"
                      path=""
                      folder
                      :filter="[]"
                      @update:path="updateMod({ modId: mod.id, dir: $event })"
                    >
                      <template #default="{ browse }">
                        <UButton label="Locate" @click.stop="browse" />
                      </template>
                    </FileSelector>
                    <UIcon v-else name="i-lucide-chevron-right" class="size-3.5 shrink-0 text-dimmed" />
                  </div>
                </div>
              </div>
              <p class="shrink-0 border-t border-default px-3.5 py-3 text-12 text-dimmed">
                Health and the Event Graph follow a new order as soon as you drop it. Colors stay with the mod, not the
                position.
              </p>
            </div>
          </template>
          <template #card>
            <WorkspaceModCard v-if="selected" :key="selected.id" :workspace-id="id" :mod="selected" />
            <div v-else class="flex min-w-0 flex-1 flex-col overflow-hidden rounded-lg border border-default bg-muted">
              <div class="flex items-center gap-3.5 border-b border-default px-5 py-4">
                <img :src="gameIcon(state.game?.id ?? '')" alt="" class="size-9 rounded-lg" />
                <div class="flex min-w-0 flex-col gap-0.5">
                  <span class="truncate text-16">{{ state.game?.name }}</span>
                  <span class="text-12 text-dimmed">Game install, read only</span>
                </div>
              </div>
              <div class="flex flex-col gap-3 px-5 py-4">
                <h3 class="flex items-baseline gap-2.5 text-13 font-semibold">
                  Color
                  <span class="text-12 font-normal text-dimmed">
                    {{ record.gameColor ? "Picked by hand" : "Automatic, a grey of the theme" }}
                  </span>
                </h3>
                <div class="flex flex-wrap items-center gap-2">
                  <button
                    type="button"
                    class="size-5.5 shrink-0 rounded-md border border-accented bg-(--game)"
                    :class="{ 'ring-1 ring-primary ring-offset-2 ring-offset-(--bg-3)': !record.gameColor }"
                    aria-label="Automatic"
                    @click="update({ ...record, gameColor: '' })"
                  />
                  <span class="mx-1 h-4.5 w-px shrink-0 bg-accented" />
                  <ValueField
                    kind="color"
                    :value="record.gameColor"
                    label="Custom…"
                    @update:value="update({ ...record, gameColor: $event })"
                  />
                </div>
              </div>
            </div>
          </template>
        </USplitter>
        <UModal
          v-model:open="creating"
          title="Create a mod"
          description="A descriptor, the game's mod folders, readmes and a localization file."
          :ui="{ content: 'h-128' }"
        >
          <template #body>
            <CreateModForm :workspace-id="id" @created="creating = false" />
          </template>
          <template #footer="{ close }">
            <span class="grow" />
            <UButton label="Cancel" @click="close" />
          </template>
        </UModal>
      </div>
    </template>

    <template #appearance>
      <div class="flex h-full flex-col gap-4 overflow-y-auto px-8 py-6">
        <div class="flex flex-col gap-0.5">
          <h1 class="text-20">Appearance</h1>
          <p class="text-13 text-dimmed">Applies to every workspace and every PMT window.</p>
        </div>
        <section class="grid grid-cols-[260px_minmax(0,1fr)] gap-8 border-b border-default pb-5">
          <div class="min-w-0">
            <h2 class="text-13 font-semibold">Theme</h2>
            <p class="mt-0.75 text-12 leading-4.5 text-dimmed">
              Every theme is the same set of tokens with different values, so all pages and the editor change together.
              Each comes dark and light.
            </p>
          </div>
          <div class="grid grid-cols-3 gap-3.5">
            <button
              v-for="family in THEME_FAMILIES"
              :key="family.id"
              type="button"
              class="flex flex-col gap-2 rounded-tile border bg-muted p-2 text-start"
              :class="
                family.id === settings.value('appearance.theme')
                  ? 'border-primary ring-2 ring-primary/15'
                  : 'border-default'
              "
              @click="settings.write('appearance.theme', family.id)"
            >
              <ThemePreview :family="family.id" :dark="settings.theme().dark" />
              <span class="flex min-w-0 items-center gap-1.5 px-1 pb-0.5 text-12 font-medium whitespace-nowrap">
                {{ family.label }}
                <span class="grow" />
                <UBadge v-if="family.id === settings.value('appearance.theme')" label="Your pick" size="sm" />
                <UBadge
                  v-else-if="family.id === settings.theme().family"
                  label="In use here"
                  color="secondary"
                  size="sm"
                />
                <span v-else class="truncate text-11 font-normal text-dimmed">{{ NOTES[family.id] }}</span>
              </span>
            </button>
          </div>
        </section>
        <SettingsSection section="appearance" />
        <section class="grid grid-cols-[260px_minmax(0,1fr)] gap-8 border-t border-default pt-5">
          <div class="min-w-0">
            <h2 class="text-13 font-semibold">Mod colors</h2>
            <p class="mt-0.75 text-12 leading-4.5 text-dimmed">
              Each mod gets a color when it joins a workspace and keeps it. Pick one by hand in Mods and load order.
            </p>
          </div>
          <div class="flex flex-wrap items-center gap-3">
            <OriginChip v-for="chip in [GAME, ...mods.map((mod) => mod.id)]" :key="chip" :origin="chip" />
            <span class="grow" />
            <UButton label="Reset all to automatic" size="sm" :disabled="!custom" @click="resetColors()" />
          </div>
        </section>
      </div>
    </template>

    <template #shortcuts>
      <div class="flex h-full flex-col gap-4 overflow-y-auto px-8 py-6">
        <div class="flex flex-col gap-0.5">
          <h1 class="text-20">Keyboard shortcuts</h1>
          <p class="text-13 text-dimmed">Every command with a shortcut. A shortcut works where its command applies.</p>
        </div>
        <div class="divide-y divide-default">
          <div v-for="command in SHORTCUTS" :key="command.id" class="flex h-9 items-center gap-2.5 text-13">
            <UIcon v-if="command.icon" :name="command.icon" class="size-4 shrink-0 text-dimmed" />
            <span v-if="command.prefix" class="text-dimmed">{{ command.prefix }}</span>
            <span class="min-w-0 grow truncate">{{ command.label }}</span>
            <span class="flex shrink-0 gap-0.5">
              <UKbd
                v-for="(key, index) in command.kbds"
                :key="index"
                v-bind="typeof key === 'string' ? { value: key } : key"
              />
            </span>
          </div>
        </div>
      </div>
    </template>
  </UTabs>

  <StatusFacts :facts="[{ label: 'Settings', value: 'Saved automatically' }]" />
</template>
