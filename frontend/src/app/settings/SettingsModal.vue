<!--
  Settings, a dialog over any page of any window. It lists the workspace's, the IDE's and the app's settings in
  sections, and opens when the command bus sends settings.open, at the section it names if any.
-->
<script setup lang="ts">
import { computed, nextTick, shallowRef, useTemplateRef, watch } from "vue";
import { useRouter } from "vue-router";
import { useMutation, useQuery } from "@pinia/colada";
import { createReusableTemplate, useEventBus, useEventListener } from "@vueuse/core";
import { useSortable } from "@vueuse/integrations/useSortable";
import { Dialogs } from "@wailsio/runtime";
import type { SelectItem, TabsItem } from "@nuxt/ui";

import { findCommand } from "../commands";
import { TOOL_CHOICES } from "../pages";
import { describeDumps, matchQuery } from "../../lib/format";
import {
  environmentQuery,
  languagesQuery,
  programsQuery,
  settingsCatalogQuery,
  stateQuery,
  workspacesQuery,
} from "../../queries";
import { gameIcon, THEME_FAMILIES, useSettingsStore } from "../../stores/settings";
import { COMMAND_BUS } from "../../stores/windows";
import { useWorkspaceStore, VANILLA } from "../../stores/workspace";
import ColorPicker from "../../ui/ColorPicker.vue";
import CreateModForm from "../../ui/CreateModForm.vue";
import FileSelector from "../../ui/FileSelector.vue";
import InstallPicker from "../../ui/InstallPicker.vue";
import OriginChip from "../../ui/OriginChip.vue";
import ScriptDocsGuide from "../../ui/ScriptDocsGuide.vue";
import SettingsSection, { SETTING_HIGHLIGHT, SETTING_ROWS } from "../../ui/SettingsSection.vue";
import TermInfo from "../../ui/TermInfo.vue";
import AgentsSection from "./AgentsSection.vue";
import ShortcutsSection from "./ShortcutsSection.vue";
import WorkspaceModCard from "./WorkspaceModCard.vue";
import * as workspace from "@services/workspace/service";
import type { Mod } from "@services/workspace/models";

/**
 * GROUPS holds the sections in listed order, grouped by what they apply to. A workspace group shows only while one is
 * open. A catalog group's sections are drawn from the settings catalog alone, each with a button that resets it.
 */
const GROUPS = [
  {
    label: "App",
    mark: "i-lucide-monitor",
    sub: "Every workspace on this computer",
    sections: [
      { value: "appearance", label: "Appearance" },
      { value: "guidance", label: "Guidance" },
      { value: "shortcuts", label: "Keyboard shortcuts" },
      { value: "terminal", label: "Terminal" },
      { value: "agents", label: "Agents and tools" },
    ],
  },
  {
    label: "IDE",
    mark: "i-lucide-code",
    sub: "Kept for this workspace",
    workspace: true,
    catalog: true,
    sections: [
      { value: "editor", label: "Editor" },
      { value: "explorer", label: "Explorer" },
      { value: "language", label: "Language features" },
      { value: "checks", label: "Checks" },
      { value: "visual", label: "Visual view" },
      { value: "wiki", label: "Wiki" },
    ],
  },
  {
    label: "Workspace",
    mark: "i-lucide-folder",
    workspace: true,
    sections: [
      { value: "general", label: "General" },
      { value: "install", label: "Game install" },
      { value: "mods", label: "Mods and load order" },
    ],
  },
];
/** HELP holds the paragraphs of Settings' help. */
const HELP = [
  "General: the workspace's name, its localization language, the page it opens on, the IDE's tabs, start page and explorer, and the first-mod checklist.",
  "Game install: the install this workspace reads, Launch, which starts the game, and how to make the game write its documentation.",
  "Mods and load order: drag to reorder. When two mods ship a file at the same path, the game reads the one lower in the order; Health's Compatibility view explains how each game picks between copies. Colors stay with the mod, not its position. Each mod's calendar, Workshop link and upload exclusions are on its card. Changes save as you make them.",
  "IDE: the editor, the explorer, the language features, the checks, the Visual view and the wiki, kept for this workspace.",
  "Appearance applies to every workspace: the theme, light and dark, matching the game's theme, the interface scale, the pages shown and the simple interface, which hides the IDE's more advanced parts.",
  "Guidance applies to every workspace: one switch for the parts of PMT that only teach modding, and a list of what stays whichever way it is set.",
  "Agents and tools: the port and token of PMT's tools for coding agents, the text that registers PMT with an agent you run yourself, and agents of your own.",
];
// the parts of PMT that the Guidance switch hides, and the ones that stay whatever it says
const GUIDED = [
  {
    icon: "i-lucide-target",
    title: "What do you want to make?",
    note: "The first screen of Make a new mod. Turned off, the form opens on the game and the name.",
  },
  { icon: "i-lucide-layers", title: "How a mod works", note: "A link on Make a new mod. Help keeps it either way." },
  {
    icon: "i-lucide-list-checks",
    title: "Your first mod",
    note: "The checklist on the Library's card, while a step is left. Its Hide takes it off one workspace only.",
  },
];
const ALWAYS_THERE = [
  "Override one entry",
  "Launch and the game's log",
  "The start page",
  "Where you left off",
  "What to do, on every problem",
  "Game readme/info on folders",
  "Workshop mods read only",
  "Help's Start here",
];
const HEADING = {
  ui: {
    trigger:
      "mt-3.5 h-auto rounded-none border-t border-default pt-3 ps-2.5 pb-1.5 disabled:cursor-default disabled:opacity-100",
    label: "flex flex-col items-start gap-0.5 overflow-hidden",
  },
};
// the splitter's two panes: the load order and the picked mod's card; the list keeps its width
const PANES = [
  { slot: "list", sizeUnit: "px" as const, defaultSize: 420, minSize: 320, maxSize: 640 },
  { slot: "card" },
];
const LAUNCH = findCommand("game.launch");
const ROW = "relative flex h-14 min-w-0 items-center gap-2.5 border-b border-default ps-2 pe-3 text-start";
const PICKED = "bg-(--accent-soft) before:absolute before:inset-y-0 before:start-0 before:w-0.5 before:bg-primary";
// the game's one swatch: its automatic color, which the record holds as ""
const GAME_COLORS = [{ value: "", background: "var(--game)", label: "Automatic" }];
// the shell picker's two choices that are no path: PMT's own choice, which the setting holds as "", and a program
// chosen with the file dialog
const DEFAULT_SHELL = "default";
const OTHER_SHELL = "other";
// the rows of a theme tile's sidebar: a file or an origin's name, and the origin's color if it is one
const SIDEBAR = [
  { label: "Sunset Invasion", origin: "--o1" },
  { label: "sir_events.txt" },
  { label: "SIR Music", origin: "--o2" },
  { label: "Crusader Kings III", origin: "--game" },
];

// a section's page: its title and note, the buttons beside the title if any, and its content
const [DefinePage, Page] = createReusableTemplate<
  { title: string; note: string; term?: string },
  { default: undefined; actions: undefined }
>();
// a two-column row of a page: its title and note on the left and its control on the right, the line above it and not
// below when it is the page's last; the key of the setting it edits, if any, makes it a row the palette can open at
const [DefineRow, Row] = createReusableTemplate<{ title: string; note: string; last?: boolean; setting?: string }>();

const router = useRouter();
const settings = useSettingsStore();
const store = useWorkspaceStore();
const commandBus = useEventBus(COMMAND_BUS);
const open = shallowRef(false);
const section = shallowRef("general");
const search = shallowRef("");
const creating = shallowRef(false);
const guiding = shallowRef(false);
const origin = shallowRef("");
const preview = shallowRef<Mod[]>();
// the key of the setting the palette opened Settings at, whose row is drawn in the highlight color until the next click
const highlighted = shallowRef("");

const { data: state } = useQuery(stateQuery);
const { data: workspaces } = useQuery(workspacesQuery);
const { data: catalog } = useQuery(settingsCatalogQuery);

// the command's target is a setting's key, else a section
commandBus.on((command, target) => {
  if (command !== "settings.open") return;
  const setting = catalog.value?.find((row) => row.key === target);
  if (target) section.value = setting?.section ?? target;
  highlighted.value = setting?.key ?? "";
  if (setting) search.value = "";
  open.value = true;
  if (setting) {
    void nextTick().then(() => document.getElementById(`setting-${setting.key}`)?.scrollIntoView({ block: "center" }));
  }
});
useEventListener(document, "pointerdown", () => (highlighted.value = ""), { capture: true });

const id = computed(() => store.activeId());
// the open workspace's record; Settings shows the app's sections alone while none is open
const record = computed(() =>
  store.isOpen() ? workspaces.value?.find((row) => row.record.id === id.value)?.record : undefined,
);
const { data: languages } = useQuery(() => ({
  ...languagesQuery(record.value?.install ?? ""),
  enabled: !!record.value?.install,
}));
const mods = computed(() => store.listMods());
const selected = computed(
  () => mods.value.find((mod) => mod.id === origin.value) ?? (origin.value === VANILLA ? undefined : mods.value[0]),
);
const order = computed({
  get: () => preview.value ?? mods.value,
  set: (next) => {
    preview.value = next;
    reorder(next.map((mod) => mod.id));
  },
});
const dumps = computed(() => state.value && describeDumps(state.value.dumps));
const custom = computed(() => !!record.value?.gameColor || mods.value.some((mod) => store.isCustom(mod.color)));
const theme = computed(() => settings.theme());
const pickedFamily = computed(() => settings.value("appearance.theme"));
// the chips of the mod colors: the game's files, then each mod
const chips = computed(() => [VANILLA, ...mods.value.map((mod) => mod.id)]);

// a section is listed when its title or one of its settings matches what was typed
const sections = computed(() => {
  const matches = (item: { value: string; label: string }) =>
    matchQuery(
      search.value,
      item.label,
      ...(catalog.value ?? []).filter((row) => row.section === item.value).map((row) => `${row.label} ${row.words}`),
    );
  return GROUPS.filter((group) => !group.workspace || record.value)
    .map((group) => ({ ...group, sections: group.sections.filter(matches) }))
    .filter((group) => group.sections.length);
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
    // a catalog group's sections draw in the content slot, the others in a slot of their own
    ...group.sections.map((tab) => ({
      label: tab.label,
      value: tab.value,
      slot: group.catalog ? undefined : tab.value,
    })),
  ]),
);
const current = computed({
  get: () => (listed.value.find((item) => item.value === section.value) ?? listed.value[0])?.value ?? "appearance",
  set: (value: string | number) => (section.value = `${value}`),
});

useSortable(useTemplateRef<HTMLElement>("list"), order, { handle: ".mod-handle", animation: 150, watchElement: true });
watch(mods, () => (preview.value = undefined));

const { update } = store;
const { mutate: reorder } = useMutation({
  mutation: (ids: string[]) => workspace.ReorderMods(id.value, ids),
  onError: () => (preview.value = undefined),
});
const { mutate: addMod } = useMutation({ mutation: (dir: string) => workspace.AddMod(id.value, dir) });
const { mutate: updateMod } = useMutation({
  mutation: ({ modId, dir }: { modId: string; dir: string }) => store.updateMod(id.value, modId, { dir }),
});
const { mutate: resetColors } = useMutation({ mutation: () => workspace.ResetColors(id.value) });
const { mutate: rescan } = useMutation({ mutation: () => workspace.RescanInstall(state.value?.install.id ?? "") });

const { data: programs } = useQuery(() => ({ ...programsQuery, enabled: open.value && current.value === "terminal" }));
const { data: environment } = useQuery(() => ({
  ...environmentQuery,
  enabled: open.value && current.value === "terminal",
}));
const shellRow = computed(() => catalog.value?.find((row) => row.key === "terminal.shell"));
// PMT's own choice, each shell of the system, the program the setting names if it is none of them, and another
const shellChoices = computed<SelectItem[]>(() => {
  const system = programs.value ?? [];
  const chosen = settings.value("terminal.shell");
  return [
    { label: "Found by PMT", description: system[0]?.name ?? "No shell was found", value: DEFAULT_SHELL },
    ...system.map((program) => ({ label: program.name, description: program.path, value: program.path })),
    ...(chosen && !system.some((program) => program.path === chosen)
      ? [{ label: "Your program", description: chosen, value: chosen }]
      : []),
    { type: "separator" },
    { label: "Another program…", value: OTHER_SHELL },
  ];
});
const { mutate: writeShell } = useMutation({
  mutation: async (choice: string) => {
    const path =
      choice === OTHER_SHELL ? await Dialogs.OpenFile({ CanChooseFiles: true, Title: "The shell to run" }) : choice;
    if (path) settings.write("terminal.shell", path === DEFAULT_SHELL ? "" : path);
  },
});

/** resetSection puts every setting of the given catalog section back to its default for this workspace. */
function resetSection(value: string): void {
  for (const row of catalog.value ?? []) if (row.section === value) settings.write(row.key, row.default);
}

/** remove removes the workspace after asking, then closes Settings and goes to the Library. Cancelling stays. */
async function remove(): Promise<void> {
  if (!record.value || !(await store.remove(record.value))) return;
  open.value = false;
  void router.push({ name: "library" });
}
</script>

<template>
  <UModal
    v-model:open="open"
    title="Settings"
    description="Saved automatically"
    :ui="{
      content: 'h-[min(48.75rem,90vh)] sm:max-w-[min(73.75rem,92vw)]',
      header: 'min-h-13 py-2.5 pe-14 sm:pe-14',
      wrapper: 'flex-1',
      body: 'flex min-h-0 flex-1 overflow-hidden p-0 sm:p-0',
    }"
  >
    <template #actions>
      <UPopover :content="{ align: 'end' }">
        <UButton label="Help" icon="i-lucide-circle-help" variant="ghost" size="sm" />
        <template #content>
          <div class="flex max-w-120 flex-col gap-3 p-4 text-13 text-toned">
            <p v-for="(paragraph, index) in HELP" :key="index">{{ paragraph }}</p>
          </div>
        </template>
      </UPopover>
    </template>
    <template #body>
      <DefinePage v-slot="{ title, note, term, $slots }">
        <div class="flex h-full flex-col gap-4 overflow-y-auto px-8 py-6">
          <div class="flex items-center gap-3">
            <div class="flex min-w-0 grow flex-col gap-0.5">
              <h1 class="flex items-center gap-2 text-20">
                {{ title }}
                <TermInfo v-if="term" :term="term" />
              </h1>
              <p class="text-13 text-dimmed">{{ note }}</p>
            </div>
            <component :is="$slots.actions" />
          </div>
          <component :is="$slots.default" />
        </div>
      </DefinePage>
      <DefineRow v-slot="{ title, note, last, setting, $slots }">
        <section
          :id="setting && `setting-${setting}`"
          class="grid grid-cols-[260px_minmax(0,1fr)] gap-8 border-accented"
          :class="[last ? 'border-t pt-5' : 'border-b pb-5', setting && setting === highlighted && SETTING_HIGHLIGHT]"
        >
          <div class="min-w-0">
            <h2 class="text-13 font-semibold">{{ title }}</h2>
            <p class="mt-0.75 text-12 leading-4.5 text-dimmed">{{ note }}</p>
          </div>
          <component :is="$slots.default" />
        </section>
      </DefineRow>
      <UTabs
        v-model="current"
        :items="tabs"
        orientation="vertical"
        activation-mode="manual"
        class="h-full w-full"
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
        <template #list-trailing>
          <UButton
            v-if="record && !search"
            label="Remove this workspace"
            icon="i-lucide-trash-2"
            color="error"
            variant="ghost"
            size="sm"
            class="mt-1 w-full justify-start ps-2.5"
            @click="remove()"
          />
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
        <template #content="{ item }">
          <Page :title="item.label ?? ''" note="Kept for this workspace.">
            <template #actions>
              <UButton
                :label="item.value === 'checks' ? 'Reset the checks' : 'Reset this section'"
                icon="i-lucide-rotate-ccw"
                variant="ghost"
                @click="resetSection(String(item.value))"
              />
            </template>
            <SettingsSection :section="String(item.value)" :highlight="highlighted" />
          </Page>
        </template>

        <template #general>
          <Page v-if="record" title="General" note="This workspace's name, language and the IDE's start.">
            <div :class="SETTING_ROWS">
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
                  :items="languages ?? []"
                  value-key="folder"
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
                <USwitch
                  :model-value="!record.resetTabs"
                  @update:model-value="update({ ...record, resetTabs: !$event })"
                />
              </UFormField>
              <UFormField
                label="Open the start page"
                description="Opens it as a tab, in front of the reopened ones, each time the workspace opens. With no tab open, the editor shows it either way."
                orientation="horizontal"
              >
                <USwitch
                  :model-value="!record.hideStartPage"
                  @update:model-value="update({ ...record, hideStartPage: !$event })"
                />
              </UFormField>
              <UFormField
                label="Show the first-mod checklist"
                description="On the Library's card while a step is left, with Guidance for new modders on."
                orientation="horizontal"
              >
                <USwitch
                  :model-value="!record.hideChecklist"
                  @update:model-value="update({ ...record, hideChecklist: !$event })"
                />
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
          </Page>
        </template>

        <template #install>
          <Page
            v-if="state?.open && record"
            title="Game install"
            :note="`The ${state.game?.name} files this workspace reads. Changing it reads the workspace again.`"
          >
            <InstallPicker
              :model-value="record.install"
              :game-id="state.game?.id ?? ''"
              @update:model-value="update({ ...record, install: $event })"
            />
            <p v-if="!state.installFound" class="text-12 text-error">
              The install's folder was not found. Pick where the game is now with Change folder.
            </p>
            <div class="flex flex-col gap-3 border-t border-accented pt-5">
              <div class="flex flex-col gap-0.75">
                <h2 class="text-13 font-semibold">Launch</h2>
                <p class="text-12 text-dimmed">Starts {{ state.game?.name }} with its debug flag.</p>
              </div>
              <UButton label="Launch" icon="i-lucide-play" class="self-start" @click="LAUNCH?.onSelect?.($event)" />
            </div>
            <div class="flex flex-col gap-3 border-t border-accented pt-5">
              <div class="flex flex-col gap-0.75">
                <h2 class="flex items-center gap-1.5 text-13 font-semibold">
                  Game documentation
                  <TermInfo term="game documentation" />
                </h2>
                <p class="text-12 text-dimmed">
                  The game writes its own documentation once you ask it to. PMT reads it rather than guessing, and keeps
                  its own copy for when it is missing.
                </p>
              </div>
              <UBadge :label="dumps?.label" :color="dumps?.current ? 'success' : 'warning'" class="self-start" />
              <div class="flex gap-2">
                <UButton
                  label="How to get the game's documentation"
                  icon="i-lucide-book-open"
                  @click="guiding = true"
                />
                <UButton label="Rescan the install" icon="i-lucide-refresh-cw" @click="rescan()" />
              </div>
            </div>
            <ScriptDocsGuide v-model:open="guiding" :game="state.game?.id ?? ''" />
          </Page>
        </template>

        <template #mods>
          <Page
            v-if="state?.open && record"
            title="Mods and load order"
            term="load order"
            note="Mods load top to bottom after the game."
          >
            <template #actions>
              <UButton label="Create a new mod" size="md" @click="creating = true" />
              <FileSelector path="" folder :filter="[]" @update:path="addMod">
                <template #default="{ browse }">
                  <UButton label="Add an existing mod" icon="i-lucide-plus" size="md" @click="browse" />
                </template>
              </FileSelector>
            </template>
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
                      @click="origin = VANILLA"
                    >
                      <span class="w-3.5 shrink-0" />
                      <span class="w-3.5 shrink-0 text-center font-mono text-11">0</span>
                      <span
                        class="size-5.5 shrink-0 rounded-md border border-accented"
                        :style="{ background: store.originColor(VANILLA) }"
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
                        <UIcon
                          name="i-lucide-grip-vertical"
                          class="mod-handle size-3.5 shrink-0 cursor-grab text-dimmed"
                        />
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
                    Health and the Event Graph follow a new order as soon as you drop it. Colors stay with the mod, not
                    the position.
                  </p>
                </div>
              </template>
              <template #card>
                <WorkspaceModCard v-if="selected" :key="selected.id" :workspace-id="id" :mod="selected" />
                <div
                  v-else
                  class="flex min-w-0 flex-1 flex-col overflow-hidden rounded-lg border border-default bg-muted"
                >
                  <div class="flex items-center gap-3.5 border-b border-default px-5 py-4">
                    <img :src="gameIcon(state.game?.id ?? '')" alt="" class="size-9 rounded-lg" />
                    <div class="flex min-w-0 flex-col gap-0.5">
                      <span class="truncate text-16">{{ state.game?.name }}</span>
                      <span class="text-12 text-dimmed">Game install, read only</span>
                    </div>
                  </div>
                  <ColorPicker
                    :model-value="record.gameColor"
                    :items="GAME_COLORS"
                    note="Automatic, a grey of the theme"
                    class="px-5 py-4"
                    @update:model-value="update({ ...record, gameColor: $event })"
                  />
                </div>
              </template>
            </USplitter>
            <CreateModForm v-model:open="creating" :workspace-id="id" />
          </Page>
        </template>

        <template #appearance>
          <Page title="Appearance" note="Applies to every workspace and every PMT window.">
            <SettingsSection section="appearance" :keys="['appearance.simple']" :highlight="highlighted" />
            <Row
              setting="appearance.theme"
              title="Theme"
              note="Every theme is the same set of tokens with different values, so all pages and the editor change together. Each comes dark and light."
            >
              <div class="grid grid-cols-3 gap-3.5">
                <button
                  v-for="family in THEME_FAMILIES"
                  :key="family.id"
                  type="button"
                  class="flex flex-col gap-2 rounded-tile border bg-muted p-2 text-start"
                  :class="family.id === pickedFamily ? 'border-primary ring-2 ring-primary/15' : 'border-default'"
                  @click="settings.write('appearance.theme', family.id)"
                >
                  <div
                    :data-theme="`${family.id}-${theme.dark ? 'dark' : 'light'}`"
                    class="flex h-33 flex-col overflow-hidden rounded-md border border-(--line-2) bg-(--bg-1) text-[9px] leading-[1.45] text-(--fg-1)"
                  >
                    <div
                      class="flex h-5 flex-none items-center gap-2 border-b border-(--line-1) bg-(--bg-2) px-1.75 text-(--fg-2)"
                    >
                      <span class="size-2.25 rounded-[3px] bg-(--accent)" />
                      <span class="flex h-5 items-center border-b-[1.5px] border-(--accent) text-(--fg-1)">IDE</span>
                      <span>Health</span>
                      <span>Graph</span>
                    </div>
                    <div class="flex min-h-0 grow">
                      <div
                        class="flex w-19 flex-none flex-col gap-px border-e border-(--line-1) bg-(--bg-2) py-1 text-(--fg-2)"
                      >
                        <span
                          v-for="row in SIDEBAR"
                          :key="row.label"
                          class="ms-1 block h-2.75 truncate ps-1.25 leading-[11px]"
                          :class="{ 'bg-(--bg-4) text-(--fg-1)': !row.origin }"
                          :style="{ boxShadow: row.origin && `inset 2px 0 0 var(${row.origin})` }"
                        >
                          {{ row.label }}
                        </span>
                      </div>
                      <pre
                        class="m-0 grow overflow-hidden bg-(--bg-0) py-1 font-mono text-[8.5px] leading-3"
                      ><span class="text-(--fg-3)">4  </span><span class="text-(--syn-entry)">sunset_invasion.0001</span> <span class="text-(--syn-operator)">=</span> {
<span class="text-(--fg-3)">5  </span>    <span class="text-(--syn-block)">trigger</span> = { <span class="text-(--syn-field)">is_adult</span> = <span class="text-(--syn-literal)">yes</span> }
<span class="text-(--fg-3)">6  </span>    <span class="text-(--syn-comment)"># envoys arrive</span>
<span class="text-(--fg-3)">7  </span>    <span class="text-(--syn-scope)">scope:envoy</span> = { <span class="text-(--syn-field)">add_gold</span> = <span class="text-(--syn-number)">50</span> }
<span class="text-(--fg-3)">8  </span>    <span class="text-(--syn-field)">title</span> = <span class="text-(--syn-string)">sir.0001.t</span></pre>
                    </div>
                    <div class="flex h-5.5 flex-none items-center gap-1 border-t border-(--line-1) bg-(--bg-1) px-1.5">
                      <span
                        class="rounded-[3px] bg-(--accent) px-1.25 text-[8px] leading-[13px] font-semibold text-(--accent-ink)"
                      >
                        Upload
                      </span>
                      <span class="rounded-[3px] bg-(--red-soft) px-1 text-[8px] text-(--red)">1</span>
                      <span class="rounded-[3px] bg-(--orange-soft) px-1 text-[8px] text-(--orange)">2</span>
                      <span
                        v-for="n in 3"
                        :key="n"
                        class="size-1.5 rounded-full"
                        :style="{ background: `var(--o${n})` }"
                      />
                      <span class="ms-auto px-1 text-[8px] text-(--link)">link</span>
                    </div>
                  </div>
                  <span class="flex min-w-0 items-center gap-1.5 px-1 pb-0.5 text-12 font-medium whitespace-nowrap">
                    {{ family.label }}
                    <span class="grow" />
                    <UBadge v-if="family.id === pickedFamily" label="Your pick" size="sm" />
                    <UBadge v-else-if="family.id === theme.family" label="In use here" color="secondary" size="sm" />
                    <span v-else class="truncate text-11 font-normal text-dimmed">{{ family.note }}</span>
                  </span>
                </button>
              </div>
            </Row>
            <SettingsSection section="appearance" :highlight="highlighted" />
            <Row
              v-if="record"
              title="Mod colors"
              note="Each mod gets a color when it joins a workspace and keeps it. Pick one by hand in Mods and load order."
              last
            >
              <div class="flex flex-wrap items-center gap-3">
                <OriginChip v-for="chip in chips" :key="chip" :origin="chip" />
                <span class="grow" />
                <UButton label="Reset all to automatic" size="sm" :disabled="!custom" @click="resetColors()" />
              </div>
            </Row>
          </Page>
        </template>

        <template #guidance>
          <Page title="Guidance" note="Applies to every workspace and every PMT window.">
            <SettingsSection section="guidance" :highlight="highlighted" />
            <section class="flex flex-col gap-2.5">
              <h2 class="text-13 font-semibold">What it shows</h2>
              <div class="flex flex-col divide-y divide-default rounded-lg border border-default bg-muted">
                <div v-for="part in GUIDED" :key="part.title" class="flex items-center gap-3.5 px-3.5 py-2.5">
                  <UIcon :name="part.icon" class="size-4 shrink-0 text-dimmed" />
                  <div class="flex min-w-0 flex-col gap-px">
                    <span class="text-13 font-medium">{{ part.title }}</span>
                    <span class="text-12 text-dimmed">{{ part.note }}</span>
                  </div>
                </div>
              </div>
            </section>
            <section class="flex flex-col gap-2.5">
              <h2 class="text-13 font-semibold">Always there</h2>
              <p class="text-12 text-dimmed">These are part of PMT and stay whatever the switch says.</p>
              <div class="flex flex-wrap gap-1.5">
                <UBadge v-for="name in ALWAYS_THERE" :key="name" :label="name" />
              </div>
            </section>
          </Page>
        </template>

        <template #shortcuts>
          <Page
            title="Keyboard shortcuts"
            note="Every command with a shortcut. A shortcut works where its command applies. Click one to press new keys; Escape keeps it."
          >
            <ShortcutsSection />
          </Page>
        </template>

        <template #terminal>
          <Page title="Terminal" note="The shells in the IDE's panel and in their own window.">
            <Row setting="terminal.shell" :title="shellRow?.label ?? ''" :note="shellRow?.help ?? ''">
              <USelect
                :model-value="settings.value('terminal.shell') || DEFAULT_SHELL"
                :items="shellChoices"
                icon="i-lucide-terminal"
                class="w-75 self-start"
                :ui="{
                  content: 'min-w-75',
                  item: 'has-data-[slot=itemDescription]:h-11',
                  itemWrapper: 'gap-px',
                  itemLabel: 'leading-4.5',
                  itemDescription: 'font-mono text-11 leading-4 text-dimmed',
                }"
                @update:model-value="writeShell(String($event))"
              />
            </Row>
            <Row
              title="Where a shell starts"
              note="The folder of the mod that holds the active file, else the workspace's first mod."
            />
            <section class="flex flex-col gap-2.5">
              <h2 class="text-13 font-semibold">What every shell is given</h2>
              <dl class="grid grid-cols-[minmax(0,max-content)_minmax(0,1fr)] gap-x-4 gap-y-1.5 text-12">
                <template v-for="row in environment ?? []" :key="row.name">
                  <dt class="font-mono text-highlighted">{{ row.value ? `${row.name}=${row.value}` : row.name }}</dt>
                  <dd class="text-dimmed">{{ row.help }}</dd>
                </template>
              </dl>
            </section>
          </Page>
        </template>

        <template #agents>
          <Page
            title="Agents and tools"
            note="The agents the agent pane runs, PMT's tools for coding agents, and the agents you run yourself."
          >
            <AgentsSection />
          </Page>
        </template>
      </UTabs>
    </template>
  </UModal>
</template>
