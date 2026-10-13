<script setup lang="ts">
/**
 * Settings: a workspace's General, Game install, and Mods and load order,
 * then the app's Appearance. Find a setting keeps the sections holding a match. Every change saves at once.
 */
import { computed, shallowRef, useTemplateRef } from "vue";
import { useRouter } from "vue-router";
import { useMutation, useQuery, useQueryCache } from "@pinia/colada";
import { useSortable } from "@vueuse/integrations/useSortable";
import type { TabsItem } from "@nuxt/ui";
import type { WorkspaceMod } from "@services/models";
import {
  AddWorkspaceMod,
  ReorderWorkspaceMods,
  SetWorkspaceLocLang,
  SetWorkspaceModPath,
  UpdateWorkspace,
} from "@services/workspaceservice";
import CreateModForm from "../../ui/CreateModForm.vue";
import RemoveWorkspaceModal from "../../ui/RemoveWorkspaceModal.vue";
import ScriptDocsGuide from "../../ui/ScriptDocsGuide.vue";
import { pickDirectory } from "../../lib/nativeDialog";
import { stateQuery, workspacesQuery } from "../../queries";
import { dumpsState, modColor, updatePrefs, useWorkspaceStore } from "../../stores/workspace";
import { LOC_LANGUAGES, SETTINGS, SETTINGS_SECTIONS, WORKSPACE_TOOLS, type SettingsSection } from "../../app/pages";
import AppearanceSettings from "./AppearanceSettings.vue";
import FieldGroup from "./FieldGroup.vue";
import GameIcon from "../../ui/GameIcon.vue";
import InstallPicker from "../../ui/InstallPicker.vue";
import OriginColorField from "./OriginColorField.vue";
import SettingSection from "./SettingSection.vue";
import StatusFacts from "../../ui/StatusFacts.vue";
import SteamState from "../../ui/SteamState.vue";
import WorkspaceModCard from "./WorkspaceModCard.vue";

const { id, section = "general" } = defineProps<{ id: string; section?: string }>();

const router = useRouter();
const store = useWorkspaceStore();
const queryCache = useQueryCache();
const overlay = useOverlay();
const createMod = overlay.create(CreateModForm);
const removeModal = overlay.create(RemoveWorkspaceModal);
const guide = overlay.create(ScriptDocsGuide);

const { data: workspaces } = useQuery(workspacesQuery);
const workspace = computed(() => workspaces.value?.find((ws) => ws.id === id));
const game = computed(() => store.game(workspace.value?.gameId ?? ""));
const { data: state } = useQuery(() => stateQuery(id));
const { mutate: savePrefs } = useMutation({ mutation: updatePrefs });
const { mutate: run } = useMutation({ mutation: (save: () => Promise<unknown>) => save() });
const PAGE_ITEMS: { label: string; value: string }[] = WORKSPACE_TOOLS.map((tool) => ({
  label: tool.label,
  value: tool.name,
}));

// the sections holding a setting that matches, by its name or the words it goes by
const find = shallowRef("");
const shownSections = computed(() => {
  const words = find.value.trim().toLowerCase();
  const all = Object.keys(SETTINGS_SECTIONS) as SettingsSection[];
  if (!words) return all;
  return all.filter((key) =>
    [
      SETTINGS_SECTIONS[key].label,
      ...SETTINGS.filter((s) => s.section === key).map((s) => `${s.label} ${s.words ?? ""}`),
    ].some((text) => text.toLowerCase().includes(words)),
  );
});
const current = computed({
  get: () => shownSections.value.find((key) => key === section) ?? shownSections.value[0] ?? "general",
  set: (key: string | number) => void router.replace({ query: { section: String(key) } }),
});

// a group's heading is a tab no one can pick
const HEADING = {
  ui: {
    trigger:
      "mt-3.5 h-auto rounded-none border-t border-default pt-3 ps-2.5 pb-1.5 disabled:cursor-default disabled:opacity-100",
    label: "flex flex-col items-start gap-0.5 overflow-hidden",
  },
};
const tabs = computed<TabsItem[]>(() => {
  const item = (key: SettingsSection): TabsItem => ({ label: SETTINGS_SECTIONS[key].label, value: key, slot: key });
  const ours = shownSections.value.filter((key) => SETTINGS_SECTIONS[key].scope === "Workspace");
  const app = shownSections.value.filter((key) => SETTINGS_SECTIONS[key].scope === "App");
  return [
    ...(ours.length
      ? [
          {
            label: "Workspace",
            sub: workspace.value?.name,
            mark: "i-lucide-folder",
            value: "workspace",
            disabled: true,
            ...HEADING,
          },
          ...ours.map(item),
        ]
      : []),
    ...(app.length
      ? [
          {
            label: "App",
            sub: "Every workspace on this computer",
            mark: "i-lucide-monitor",
            value: "app",
            disabled: true,
            ...HEADING,
          },
          ...app.map(item),
        ]
      : []),
  ];
});

async function removeWorkspace(): Promise<void> {
  const ws = workspace.value;
  if (ws && (await removeModal.open({ id: ws.id, name: ws.name }))) void router.push({ name: "library" });
}

function setLanguage(lang: string): void {
  run(() => SetWorkspaceLocLang(id, lang));
}

function rename(name: string): void {
  const ws = workspace.value;
  if (ws && name.trim()) run(() => UpdateWorkspace(ws.id, name.trim(), ws.installId));
}

const installId = computed({
  get: () => workspace.value?.installId ?? "",
  set: (next: string) => {
    const ws = workspace.value;
    if (ws && next && next !== ws.installId) run(() => UpdateWorkspace(ws.id, ws.name, next));
  },
});

const dumps = computed(() => dumpsState(state.value));

// Mods and load order: the game, then the mods in the order they load
const mods = computed<WorkspaceMod[]>({
  get: () => workspace.value?.mods ?? [],
  set: (next) => reorder(next),
});
const chosen = shallowRef("");
const selected = computed(
  () => mods.value.find((mod) => mod.id === chosen.value) ?? (chosen.value === "game" ? undefined : mods.value[0]),
);
const gameSelected = computed(() => chosen.value === "game" || !mods.value.length);
// the load order and the mod picked; the splitter keeps the list's width
const PANES = [
  { slot: "list", sizeUnit: "px" as const, defaultSize: 420, minSize: 320, maxSize: 640 },
  { slot: "card" },
];
const list = useTemplateRef("list");
useSortable(list, mods, { handle: ".mod-handle", animation: 150, watchElement: true });

function reorder(next: WorkspaceMod[]): void {
  queryCache.setQueryData(workspacesQuery.key, (all) =>
    (all ?? []).map((ws) => (ws.id === id ? { ...ws, mods: next.map((mod, i) => ({ ...mod, sortOrder: i })) } : ws)),
  );
  run(() =>
    ReorderWorkspaceMods(
      id,
      next.map((mod) => mod.id),
    ),
  );
}

async function locate(mod: WorkspaceMod): Promise<void> {
  const path = await pickDirectory(`Where ${mod.name} is now`);
  if (path) run(() => SetWorkspaceModPath(id, mod.id, path));
}

async function addExisting(): Promise<void> {
  const path = await pickDirectory("A mod folder");
  // Go names it from its descriptor, and finds its thumbnail
  if (path) run(() => AddWorkspaceMod(id, path));
}

function createNew(): void {
  const ws = workspace.value;
  if (!ws) return;
  void createMod.open({
    gameId: ws.gameId,
    workspaceId: ws.id,
    locLang: ws.defaultLocLang || "english",
    supportedVersion: state.value?.gameVersion ?? "",
  });
}

const ROW = "relative flex h-14 min-w-0 items-center gap-2.5 border-b border-default ps-2 pe-3 text-start";
function rowClass(on: boolean): string[] {
  return [
    ROW,
    on
      ? "bg-(--accent-soft) before:absolute before:inset-y-0 before:start-0 before:w-0.5 before:bg-primary"
      : "hover:bg-elevated",
  ];
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
      <UInput v-model="find" icon="i-lucide-search" placeholder="Find a setting" size="sm" class="w-full" />
    </template>
    <template #default="{ item }">
      <template v-if="item.mark">
        <span class="flex items-center gap-2 text-13 font-semibold text-highlighted">
          <UIcon :name="item.mark" class="size-4" />{{ item.label }}
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
        @click="removeWorkspace"
      >
        <UIcon name="i-lucide-trash-2" class="size-3.5" />Remove this workspace
      </button>
    </template>

    <template #general>
      <div v-if="workspace" class="h-full overflow-y-auto px-8 py-6">
        <div class="flex flex-col gap-0.5 pb-1">
          <h1 class="text-20">General</h1>
          <p class="text-13 text-dimmed">This workspace's name, language and the IDE's start.</p>
        </div>
        <SettingSection title="Name" hint="Shown in the Library and the workspace switcher.">
          <UInput
            :model-value="workspace.name"
            :model-modifiers="{ lazy: true }"
            class="w-80"
            @update:model-value="rename"
          />
        </SettingSection>
        <SettingSection
          title="Localization language"
          hint="Used for localization gaps, hover text and new mods' localization files. Changing it reads the workspace again."
        >
          <USelect
            :model-value="workspace.defaultLocLang || 'english'"
            :items="LOC_LANGUAGES"
            class="w-60"
            @update:model-value="setLanguage"
          />
        </SettingSection>
        <SettingSection title="Page it opens on" hint="Where the workspace opens from the Library and the switcher.">
          <USelect
            :model-value="workspace.defaultTool || 'workspace-ide'"
            :items="PAGE_ITEMS"
            class="w-60"
            @update:model-value="savePrefs({ ws: workspace, defaultTool: $event })"
          />
        </SettingSection>
        <SettingSection title="IDE" hint="How the IDE opens this workspace.">
          <UFormField
            label="Reopen the IDE's tabs"
            description="The files that were open when you left or closed PMT."
            orientation="horizontal"
          >
            <USwitch
              :model-value="!workspace.resetIdeOnOpen"
              @update:model-value="savePrefs({ ws: workspace, resetIdeOnOpen: !$event })"
            />
          </UFormField>
          <UFormField
            label="Hide binaries in the explorer"
            description="dll, exe, pdb and the like. Images, sounds, meshes and .metadata stay."
            orientation="horizontal"
          >
            <USwitch
              :model-value="workspace.hideExplorerBinaries"
              @update:model-value="savePrefs({ ws: workspace, hideExplorerBinaries: $event })"
            />
          </UFormField>
        </SettingSection>
      </div>
    </template>

    <template #install>
      <div v-if="workspace" class="h-full overflow-y-auto px-8 py-6">
        <div class="flex flex-col gap-0.5 pb-5">
          <h1 class="text-20">Game install</h1>
          <p class="text-13 text-dimmed">
            The {{ game?.name }} files this workspace reads. Changing it reads the workspace again.
          </p>
        </div>
        <InstallPicker v-model="installId" :game-id="workspace.gameId" class="border-b border-default pb-5" />
        <SettingSection
          title="script_docs"
          hint="The game prints its own scripting documentation once you ask it to. PMT reads it rather than guessing, and keeps its own copy for when it is missing."
        >
          <div class="flex items-center gap-2">
            <UBadge
              :label="dumps?.label ?? 'Read when the workspace opens'"
              :color="dumps ? (dumps.ok ? 'success' : 'warning') : 'neutral'"
            />
          </div>
          <p v-for="folder in state?.dumpFolders ?? []" :key="folder" class="font-mono text-12 break-all text-dimmed">
            {{ folder }}
          </p>
          <UButton
            label="How to make script_docs"
            icon="i-lucide-book-open"
            class="self-start"
            @click="guide.open({ folders: state?.dumpFolders ?? [] })"
          />
        </SettingSection>
      </div>
    </template>

    <template #mods>
      <div v-if="workspace" class="flex h-full flex-col px-8 py-6">
        <div class="flex items-center gap-3 pb-4">
          <div class="flex min-w-0 flex-col gap-0.5">
            <h1 class="text-20">Mods and load order</h1>
            <p class="text-13 text-dimmed">
              Mods load top to bottom after the game. For most kinds the last one to define an object wins.
              {{ game?.firstWins }}
            </p>
          </div>
          <span class="grow" />
          <UButton label="Create a new mod" size="md" @click="createNew" />
          <UButton label="Add an existing mod" icon="i-lucide-plus" size="md" @click="addExisting" />
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
                  :class="rowClass(gameSelected)"
                  class="w-full text-dimmed"
                  @click="chosen = 'game'"
                >
                  <span class="w-3.5 shrink-0" />
                  <span class="w-3.5 shrink-0 text-center font-mono text-11">0</span>
                  <span
                    class="size-5.5 shrink-0 rounded-md border border-accented"
                    :style="{ background: store.originColor(store.originVanilla) }"
                  />
                  <span class="flex min-w-0 grow flex-col">
                    <span class="truncate text-13 font-medium text-toned">{{ game?.name }}</span>
                    <span class="truncate text-11">Game install, read only</span>
                  </span>
                  <UIcon name="i-lucide-lock" class="size-3.5" />
                </button>
                <div ref="list">
                  <div
                    v-for="(mod, i) in mods"
                    :key="mod.id"
                    :class="rowClass(!gameSelected && selected?.id === mod.id)"
                    role="button"
                    tabindex="0"
                    @click="chosen = mod.id"
                    @keydown.enter="chosen = mod.id"
                  >
                    <UIcon name="i-lucide-grip-vertical" class="mod-handle size-3.5 shrink-0 cursor-grab text-dimmed" />
                    <span class="w-3.5 shrink-0 text-center font-mono text-11 text-dimmed">{{ i + 1 }}</span>
                    <span
                      class="size-5.5 shrink-0 rounded-md border border-accented"
                      :style="{ background: modColor(mod) }"
                    />
                    <span class="flex min-w-0 grow flex-col">
                      <span class="truncate text-13 font-medium">{{ mod.name }}</span>
                      <span v-if="mod.isBroken" class="truncate text-11 text-error">Folder not found</span>
                      <SteamState v-else :workspace-id="id" :mod-id="mod.id" />
                    </span>
                    <UButton v-if="mod.isBroken" label="Locate" @click.stop="locate(mod)" />
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
            <WorkspaceModCard
              v-if="selected && !gameSelected"
              :key="selected.id"
              :workspace-id="id"
              :mod="selected"
              @removed="chosen = ''"
            />
            <div v-else class="flex min-w-0 flex-1 flex-col overflow-hidden rounded-lg border border-default bg-muted">
              <div class="flex items-center gap-3.5 border-b border-default px-5 py-4">
                <GameIcon :game-id="workspace.gameId" size="lg" />
                <div class="flex min-w-0 flex-col gap-0.5">
                  <span class="truncate text-16">{{ game?.name }}</span>
                  <span class="text-12 text-dimmed">Game install, read only</span>
                </div>
              </div>
              <FieldGroup
                title="Color"
                :hint="workspace.gameColor ? 'Picked by hand' : 'Automatic, a grey of the theme'"
              >
                <OriginColorField
                  :model-value="workspace.gameColor ?? ''"
                  @update:model-value="savePrefs({ ws: workspace, gameColor: $event })"
                />
              </FieldGroup>
            </div>
          </template>
        </USplitter>
      </div>
    </template>

    <template #appearance>
      <div class="h-full overflow-y-auto px-8 py-6">
        <AppearanceSettings />
      </div>
    </template>
  </UTabs>

  <StatusFacts>
    <span>Saved automatically</span>
  </StatusFacts>
</template>
