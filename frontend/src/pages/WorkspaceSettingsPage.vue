<script setup lang="ts">
/**
 * Per-workspace settings: section sidebar and two-column form.
 */
import { computed, nextTick, ref, shallowRef, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useDebounceFn } from "@vueuse/core";
import { useSortable } from "@vueuse/integrations/useSortable";
import { useMutation, useQuery } from "@pinia/colada";
import type { NavigationMenuItem, RadioGroupItem } from "@nuxt/ui";
import {
  AddGameInstall,
  AddWorkspaceMod,
  CountWorkspacesUsingInstall,
  FindGameInstalls,
  ListGameInstalls,
  GetWorkspace,
  RemoveWorkspaceMod,
  ReorderWorkspaceMods,
  SetWorkspaceLocLang,
  SetWorkspaceModCalendar,
  UpdateGameInstall,
  UpdateWorkspace,
  UpdateWorkspaceMod,
  UpdateWorkspacePrefs,
  GetIdeRoots,
  DeleteWorkspace,
  ExportWorkshopIgnore,
  ImportWorkshopIgnore,
} from "@services/workspaceservice";
import type { WorkspaceMod } from "@services/models";
import type { Era } from "@services/internal/session/models";
import { CopyDiagnostics } from "@services/logservice";
import FileSelector, { pickDirectory } from "../components/FileSelector.vue";
import WorkspaceToolBar from "../components/WorkspaceToolBar.vue";
import CreateModForm from "../components/CreateModForm.vue";
import RemoveWorkspaceModal from "../components/RemoveWorkspaceModal.vue";
import AddInstallCard from "../components/AddInstallCard.vue";
import ScriptDocsGuide from "../components/ScriptDocsGuide.vue";
import DiagnosticSettings from "../components/DiagnosticSettings.vue";
import { useLanguageHealth } from "../composables/useLanguageHealth";
import OriginColorField, { ORIGIN_COLOR_DESC } from "../components/OriginColorField.vue";
import WorkspaceModCard, { MOD_HELP, type ModPatch } from "../components/WorkspaceModCard.vue";
import { LOC_LANG_ITEMS, useWorkspaceStore } from "../stores/workspace";
import { WORKSPACE_TOOLS } from "../workspaceTools";
import { applyExplorerVisibility, refreshIdeRootDecorations } from "../ide/workbenchHost";

defineOptions({ name: "WorkspaceSettingsPage" });

const route = useRoute();
const router = useRouter();
const wsStore = useWorkspaceStore();
const toast = useToast();
const id = computed(() => String(route.params.id ?? ""));
const creatingMod = ref(false);
const removeOpen = ref(false);

// Shared with the health strip, so the Game tab and the IDE agree on whether a
// type system is loaded.
const { health: langHealth } = useLanguageHealth(id);
const scriptDocsOpen = ref(false);
const schemaBadge = computed(() => {
  const live = langHealth.value;
  if (live?.dumpsFrom === "yours") {
    return { color: "success" as const, label: live.dumpsStale ? "Loaded, older than the game" : "Loaded" };
  }
  if (live?.dumpsFrom === "shipped") return { color: "warning" as const, label: "PMT's copy" };
  return { color: "warning" as const, label: "Not generated" };
});

const name = ref("");
const locLang = ref("english");
const rememberOpen = ref(true);
const hideBinaries = ref(true);
const defaultTool = ref("workspace-ide");
const explorerItems: RadioGroupItem[] = [
  { label: "Hide binaries", value: "hide" },
  { label: "Show all files", value: "all" },
];
const gameColor = ref("");
const installId = ref("");
const mods = ref<WorkspaceMod[]>([]);
const modListEl = ref<HTMLElement | null>(null);
const expandedIds = shallowRef<Record<string, boolean>>({});

const newInstallPath = ref("");
const newInstallName = ref("");
const draftPath = ref("");

const pageItems: { label: string; value: string }[] = WORKSPACE_TOOLS.map((t) => ({
  label: t.label,
  value: t.name,
}));

const {
  data: page,
  error: loadError,
  isPending: loading,
  refetch,
} = useQuery({
  key: () => ["workspace-settings", id.value],
  query: async () => {
    const wsId = id.value;
    if (!wsId) throw new Error("workspace id is required");
    wsStore.setActiveWorkspace(wsId);
    const workspace = await GetWorkspace(wsId);
    if (!workspace) throw new Error("workspace not found");
    const [installs, sharing, detected] = await Promise.all([
      ListGameInstalls(workspace.gameId),
      CountWorkspacesUsingInstall(workspace.installId),
      FindGameInstalls(workspace.gameId),
    ]);
    const listed = installs ?? [];
    return {
      workspace,
      mods: workspace.mods ?? [],
      installs: listed,
      sharing: sharing ?? [],
      detected: detected ?? [],
    };
  },
  enabled: () => !!id.value,
});

/** Copy the last loaded workspace into the form (initial load and Cancel). */
function applyWorkspace(): void {
  const p = page.value;
  if (!p) return;
  const w = p.workspace;
  name.value = w.name;
  locLang.value = w.defaultLocLang || "english";
  rememberOpen.value = !w.resetIdeOnOpen;
  hideBinaries.value = w.hideExplorerBinaries !== false;
  const tool = w.defaultTool || "workspace-ide";
  defaultTool.value = pageItems.some((i) => i.value === tool) ? tool : "workspace-ide";
  gameColor.value = w.gameColor ?? "";
  installId.value = w.installId;
  mods.value = p.mods.map((m) => ({ ...m }));
  const inst = p.installs.find((i) => i.id === w.installId);
  draftPath.value = inst?.path ?? "";
}

watch(page, applyWorkspace, { immediate: true });

const selectedInstall = computed(() => page.value?.installs.find((i) => i.id === installId.value));
const supportedVersion = computed(() => selectedInstall.value?.versionDetected ?? "");
const installItems = computed<RadioGroupItem[]>(() =>
  (page.value?.installs ?? []).map((i) => ({
    label: i.versionDetected ? `${i.name} (${i.versionDetected})` : i.name,
    description: i.path,
    value: i.id,
  })),
);
const sharingWarn = computed(() => {
  const names = page.value?.sharing ?? [];
  return names.length > 1 ? names.join(", ") : "";
});
const savedInstallPaths = computed(() => (page.value?.installs ?? []).map((i) => i.path).filter(Boolean));
const error = computed(() => loadError.value?.message ?? "");

watch(
  () => mods.value.map((m) => m.id),
  (ids) => {
    if (!ids.length) return;
    if (ids.some((id) => expandedIds.value[id])) return;
    const first = ids[0];
    if (first) expandedIds.value = { [first]: true };
  },
  { immediate: true },
);

/** Expand or collapse one mod card. */
function setModExpanded(id: string, open: boolean): void {
  expandedIds.value = { ...expandedIds.value, [id]: open };
}

/** Persist the current card order into config and the live session. */
async function persistLoadOrder(): Promise<void> {
  await nextTick();
  mods.value.forEach((m, i) => {
    m.sortOrder = i;
  });
  try {
    await ReorderWorkspaceMods(
      id.value,
      mods.value.map((m) => m.id),
    );
    await wsStore.refresh();
    const rec = await GetIdeRoots(id.value);
    refreshIdeRootDecorations(rec?.roots ?? []);
    toastOk("Load order saved.");
  } catch (e) {
    toast.add({
      title: e instanceof Error ? e.message : String(e),
      color: "error",
    });
  }
}

useSortable(modListEl, mods, {
  handle: ".mod-handle",
  animation: 150,
  watchElement: true,
  onEnd: () => {
    void persistLoadOrder();
  },
});

/** Switch this workspace onto another install (rebuilds the session). */
function onInstallPick(v: unknown): void {
  if (typeof v === "string" && v) void changeInstall(v);
}

function onExplorerPick(v: unknown): void {
  hideBinaries.value = v === "hide";
}

function toastOk(title: string): void {
  toast.add({ title, color: "success" });
}

/** Put the report for a bug report on the clipboard. */
async function copyDiagnostics(): Promise<void> {
  try {
    await CopyDiagnostics();
    toastOk("Diagnostics copied");
  } catch (e) {
    toast.add({ title: e instanceof Error ? e.message : String(e), color: "error" });
  }
}

const { mutateAsync: saveOverview, isLoading: savingOverview } = useMutation({
  mutation: async () => {
    const wsId = id.value;
    const w = page.value?.workspace;
    if (!w) return;
    await UpdateWorkspace(wsId, name.value.trim(), w.installId);
    if (locLang.value !== (w.defaultLocLang || "english")) {
      await SetWorkspaceLocLang(wsId, locLang.value);
    }
    await UpdateWorkspacePrefs(wsId, !rememberOpen.value, defaultTool.value, gameColor.value, hideBinaries.value);
    applyExplorerVisibility(hideBinaries.value);
    await wsStore.refresh();
  },
  onSuccess: () => {
    toastOk("Overview saved.");
    void refetch();
  },
});

const { mutateAsync: changeInstall, isLoading: changingInstall } = useMutation({
  mutation: async (nextId: string) => {
    const w = page.value?.workspace;
    if (!w) return;
    installId.value = nextId;
    await UpdateWorkspace(id.value, w.name, nextId);
    await wsStore.refresh();
  },
  onSuccess: () => {
    toastOk("Install updated.");
    void refetch();
  },
});

const { mutateAsync: saveInstallPaths, isLoading: savingPaths } = useMutation({
  mutation: async () => {
    if (!installId.value) return;
    await UpdateGameInstall(installId.value, draftPath.value);
  },
  onSuccess: () => {
    toastOk("Install paths saved.");
    void refetch();
  },
});

const { mutateAsync: addInstall, isLoading: addingInstall } = useMutation({
  mutation: async () => {
    const gameId = page.value?.workspace.gameId;
    if (!gameId || !newInstallPath.value || !newInstallName.value) return;
    const inst = await AddGameInstall(gameId, newInstallName.value, newInstallPath.value);
    if (!inst) throw new Error("Failed to create install");
    newInstallPath.value = "";
    newInstallName.value = "";
    await changeInstall(inst.id);
  },
});

const persistMod = useDebounceFn(async (mod: WorkspaceMod) => {
  try {
    await UpdateWorkspaceMod(
      id.value,
      mod.id,
      mod.name,
      mod.color ?? "",
      mod.thumbnail ?? "",
      mod.descMdRel ?? "",
      mod.descBbRel ?? "",
      mod.workshopIgnore ?? "",
    );
    await wsStore.refresh();
    const rec = await GetIdeRoots(id.value);
    refreshIdeRootDecorations(rec?.roots ?? []);
  } catch (e) {
    toast.add({
      title: e instanceof Error ? e.message : String(e),
      color: "error",
    });
  }
}, 400);

async function addMod(): Promise<void> {
  const path = await pickDirectory("Select mod folder");
  if (!path) return;
  const label = path.split(/[/\\]/).pop() || "Mod";
  try {
    await AddWorkspaceMod(id.value, label, path, "");
  } catch (e) {
    // a folder with no descriptor, or no folder: Go says which
    toast.add({ title: e instanceof Error ? e.message : String(e), color: "error" });
    return;
  }
  toastOk("Mod added.");
  await refetch();
}

async function onModCreated(path: string, thumbnail = ""): Promise<void> {
  const label = path.split(/[/\\]/).pop() || "Mod";
  await AddWorkspaceMod(id.value, label, path, thumbnail);
  creatingMod.value = false;
  toastOk("Mod created.");
  await refetch();
}

const { mutateAsync: deleteThis, isLoading: deleting } = useMutation({
  mutation: async () => {
    await DeleteWorkspace(id.value);
    removeOpen.value = false;
    await wsStore.refresh();
    void router.push({ name: "library" });
  },
  onSuccess: () => toastOk("Workspace removed."),
});

async function removeMod(mod: WorkspaceMod): Promise<void> {
  await RemoveWorkspaceMod(id.value, mod.id);
  toastOk("Mod removed.");
  await refetch();
}

const persistColors = useDebounceFn(async () => {
  await UpdateWorkspacePrefs(id.value, !rememberOpen.value, defaultTool.value, gameColor.value, hideBinaries.value);
  await wsStore.refresh();
  const rec = await GetIdeRoots(id.value);
  refreshIdeRootDecorations(rec?.roots ?? []);
}, 400);

/** Field descriptions for the settings page (filter + UFormField). */
const COPY = {
  name: "Display name in the library and title bar.",
  loc: "Language used for localization coverage, hover text, and new-mod localization files.",
  remember: "Maintain IDE tabs status across workspace switches and app closes.",
  defaultPage: "Page opened when you enter this workspace.",
  explorer: "Hide dll/exe/pdb and similar binaries. Images, audio, meshes, and .metadata stay listed.",
  gameColor: ORIGIN_COLOR_DESC,
  installList: "Which scanned game install this workspace reads. Shared installs warn before you switch.",
  installPath: "Top-level folder of the game; needed for scanning the game and supporting most features.",
  addInstall: "Draft a Steam or browsed path, then Add install. Selecting a found path does not save it.",
  mods: "Last listed wins (LIOS). Drag the left grip to reorder. Color and thumbnail paint explorer and origin chips.",
  modColor: ORIGIN_COLOR_DESC,
  ...MOD_HELP,
  editor: "Which language checks report findings in the editor, and how loudly. Applies to every workspace — these are IDE settings, not workspace ones.",
  remove: "Drops the workspace record. Mod folders on disk stay.",
} as const;

const filter = shallowRef("");

/** Case-insensitive match against group headings and field copy. */
function matchesFilter(...parts: string[]): boolean {
  const q = filter.value.trim().toLowerCase();
  if (!q) return true;
  return parts.some((p) => p.toLowerCase().includes(q));
}

const showOverview = computed(() =>
  matchesFilter(
    "Overview",
    "Name",
    COPY.name,
    "Default loc language",
    COPY.loc,
    "Remember open files",
    COPY.remember,
    "Default page",
    COPY.defaultPage,
    "Explorer files",
    COPY.explorer,
  ),
);
const showGame = computed(() =>
  matchesFilter(
    "Game",
    "Origin color (game)",
    COPY.gameColor,
    "Install list",
    COPY.installList,
    "Install path",
    COPY.installPath,
    "Add install",
    COPY.addInstall,
    "Install name",
    "Found on this machine",
  ),
);
const showMods = computed(() =>
  matchesFilter(
    "Mods",
    COPY.mods,
    "Origin color",
    COPY.modColor,
    "Thumbnail",
    COPY.thumb,
    "Description Markdown",
    COPY.descMd,
    "Description BBCode",
    COPY.descBb,
    "Workshop ignore",
    COPY.workshopIgnore,
    "Add existing mod to workspace",
  ),
);

const loadOrderCopy = computed(() => {
  const extra = wsStore.firstWins(page.value?.workspace.gameId ?? "");
  return extra
    ? `Last listed wins for most kinds. ${extra} Vanilla loses to any mod. Conflicts labels each row.`
    : "Last listed wins for most kinds. Vanilla loses to any mod. Conflicts labels each row.";
});
const showDanger = computed(() => matchesFilter("Danger", "Remove workspace", COPY.remove));
const showEditor = computed(() =>
  matchesFilter("Editor", "Diagnostics", "Checks", "Severity", COPY.editor),
);

type SettingsSection = "overview" | "game" | "mods" | "editor" | "danger";

const section = shallowRef<SettingsSection>("overview");

const visibleSections = computed<SettingsSection[]>(() => {
  const rows: [SettingsSection, boolean][] = [
    ["overview", showOverview.value],
    ["game", showGame.value],
    ["mods", showMods.value],
    ["editor", showEditor.value],
    ["danger", showDanger.value],
  ];
  return rows.filter(([, on]) => on).map(([id]) => id);
});

watch(visibleSections, (ids) => {
  if (!ids.includes(section.value) && ids[0]) section.value = ids[0];
});

watch(
  () => route.query.section,
  (raw) => {
    const s = String(raw ?? "");
    switch (s) {
      case "overview":
      case "game":
      case "mods":
      case "editor":
      case "danger":
        section.value = s;
        return;
      default:
        return;
    }
  },
  { immediate: true },
);

const sectionMeta: Record<SettingsSection, { label: string; icon: string }> = {
  overview: { label: "Overview", icon: "i-lucide-layout-dashboard" },
  game: { label: "Game", icon: "i-lucide-gamepad-2" },
  mods: { label: "Mods", icon: "i-lucide-puzzle" },
  editor: { label: "Editor", icon: "i-lucide-file-search" },
  danger: { label: "Danger", icon: "i-lucide-triangle-alert" },
};

const navItems = computed<NavigationMenuItem[]>(() =>
  visibleSections.value.map((id) => ({
    ...sectionMeta[id],
    value: id,
    active: section.value === id,
    onSelect(e: Event) {
      e.preventDefault();
      section.value = id;
    },
  })),
);

const showFooter = computed(() => section.value === "overview");

/** Save the section that uses the sticky footer (overview). */
function saveSection(): void {
  switch (section.value) {
    case "overview":
      void saveOverview();
      return;
    case "game":
    case "mods":
    case "editor":
    case "danger":
      return;
    default: {
      const _exhaustive: never = section.value;
      void _exhaustive;
    }
  }
}

/** Merge one card edit into the draft mod and persist. */
function patchMod(mod: WorkspaceMod, patch: ModPatch): void {
  Object.assign(mod, patch);
  void persistMod(mod);
}

/** The calendar's own setter, debounced: setting one drops the session, which is rebuilt. */
const persistCalendar = useDebounceFn(
  async (mod: WorkspaceMod, eras: Era[]) => {
    try {
      await SetWorkspaceModCalendar(id.value, mod.id, eras);
    } catch (e) {
      toast.add({
        title: e instanceof Error ? e.message : String(e),
        color: "error",
      });
    }
  },
  600,
);

/** Show the change at once, persist it once the typing stops. */
function setModCalendar(mod: WorkspaceMod, eras: Era[]): void {
  mod.eras = eras;
  void persistCalendar(mod, eras);
}

async function exportIgnore(mod: WorkspaceMod): Promise<void> {
  try {
    await ExportWorkshopIgnore(id.value, mod.id);
    toastOk("Wrote .workshop-ignore in the mod folder.");
  } catch (e) {
    toast.add({
      title: e instanceof Error ? e.message : String(e),
      color: "error",
    });
  }
}

async function importIgnore(mod: WorkspaceMod): Promise<void> {
  try {
    const text = await ImportWorkshopIgnore(id.value, mod.id);
    mod.workshopIgnore = text ?? "";
    toastOk("Imported .workshop-ignore.");
    await refetch();
  } catch (e) {
    toast.add({
      title: e instanceof Error ? e.message : String(e),
      color: "error",
    });
  }
}
</script>

<template>
  <div class="flex h-full min-h-0 flex-col">
    <WorkspaceToolBar :workspace-id="id" />
    <div class="flex min-h-0 flex-1">
      <aside class="flex w-52 shrink-0 flex-col gap-3 border-e border-default p-3">
        <UInput v-model="filter" icon="i-lucide-search" placeholder="Filter settings…" class="w-full" />
        <UNavigationMenu
          :items="navItems"
          orientation="vertical"
          highlight
          class="data-[orientation=vertical]:w-full"
          :ui="{
            link: [
              'after:absolute after:-start-1.5 after:inset-y-0.5 after:block',
              'after:w-px after:rounded-full data-[active]:after:bg-primary',
            ],
          }"
        />
      </aside>
      <div class="flex min-w-0 flex-1 flex-col">
        <div class="min-h-0 flex-1 overflow-auto p-6">
          <UAlert v-if="error" color="error" variant="subtle" :description="error" />
          <p v-if="loading" class="text-sm text-muted">Loading…</p>

          <div v-else-if="section === 'overview'" class="grid grid-cols-1 gap-x-6 gap-y-5 md:grid-cols-2">
            <UFormField label="Name" :description="COPY.name">
              <UInput v-model="name" class="w-full" />
            </UFormField>
            <UFormField label="Default loc language" :description="COPY.loc">
              <USelect v-model="locLang" :items="LOC_LANG_ITEMS" value-key="value" class="w-full" />
            </UFormField>
            <UFormField label="Default page" :description="COPY.defaultPage">
              <USelect
                v-model="defaultTool"
                :items="pageItems"
                value-key="value"
                class="w-full"
                :ui="{ content: 'min-w-max' }"
              />
            </UFormField>
            <UFormField label="Remember open files" :description="COPY.remember">
              <USwitch v-model="rememberOpen" label="Enabled" />
            </UFormField>
            <UFormField label="Explorer files" :description="COPY.explorer">
              <URadioGroup
                :model-value="hideBinaries ? 'hide' : 'all'"
                :items="explorerItems"
                @update:model-value="onExplorerPick"
              />
            </UFormField>
          </div>

          <div v-else-if="section === 'game'" class="space-y-5">
            <UAlert
              v-if="sharingWarn"
              color="warning"
              variant="subtle"
              title="Shared install"
              :description="`Also used by: ${sharingWarn}`"
            />
            <div class="grid grid-cols-1 gap-4 lg:grid-cols-2 lg:items-start">
              <UCard title="Added installs" description="These are saved. Pick one for this workspace.">
                <div class="space-y-4">
                  <URadioGroup
                    v-if="installItems.length"
                    :model-value="installId"
                    :items="installItems"
                    variant="card"
                    :disabled="changingInstall"
                    @update:model-value="onInstallPick"
                  />
                  <p v-else class="text-sm text-muted">No installs added yet.</p>
                  <template v-if="selectedInstall">
                    <p class="truncate text-xs text-muted">{{ selectedInstall.path }}</p>
                    <p class="text-xs text-muted">
                      {{
                        selectedInstall.scannedAt ? `cache scanned ${selectedInstall.scannedAt}` : "cache not scanned"
                      }}
                      <template v-if="selectedInstall.versionDetected">
                        · detected {{ selectedInstall.versionDetected }}
                      </template>
                    </p>
                    <UFormField label="Change folder" :description="COPY.installPath">
                      <FileSelector v-model="draftPath" mode="folder" dialog-title="Select game install folder" />
                    </UFormField>
                    <UButton
                      label="Save folder"
                      size="xs"
                      variant="outline"
                      :loading="savingPaths"
                      @click="saveInstallPaths()"
                    />
                  </template>
                  <OriginColorField
                    v-model="gameColor"
                    kind="game"
                    label="Origin color (game)"
                    @update:model-value="persistColors()"
                  />
                </div>
              </UCard>
              <AddInstallCard
                v-if="page?.workspace"
                v-model:path="newInstallPath"
                v-model:name="newInstallName"
                :game-id="page.workspace.gameId"
                :detected="page.detected"
                :saved-paths="savedInstallPaths"
                :loading="addingInstall"
                @add="addInstall()"
              />
            </div>
            <UCard
              v-if="langHealth"
              title="Scripting documentation"
              description="The game prints its own type system. PMT reads it rather than guessing, and keeps its own copy so you only do this once per game version."
            >
              <div class="space-y-3">
                <div class="flex flex-wrap items-center gap-2 text-sm">
                  <UBadge :color="schemaBadge.color" variant="subtle" :label="schemaBadge.label" />
                </div>
                <p v-for="folder in langHealth.dumpFolders ?? []" :key="folder" class="break-all text-xs text-muted">
                  {{ folder }}
                </p>
                <UButton
                  label="How to generate it"
                  icon="i-lucide-book-open"
                  size="xs"
                  variant="outline"
                  @click="scriptDocsOpen = true"
                />
              </div>
            </UCard>
            <ScriptDocsGuide
              v-if="langHealth"
              v-model:open="scriptDocsOpen"
              :folders="langHealth.dumpFolders ?? []"
              :unread="langHealth.unread ?? []"
            />
            <UCard
              title="Diagnostics"
              description="PMT writes what it does to pmt.log in its settings folder. Copy a report to paste into a bug report."
            >
              <UButton
                label="Copy diagnostics"
                icon="i-lucide-clipboard-copy"
                size="xs"
                variant="outline"
                @click="copyDiagnostics()"
              />
            </UCard>
          </div>

          <div v-else-if="section === 'mods'" class="space-y-3">
            <UAlert
              color="neutral"
              variant="subtle"
              title="Last listed wins (LIOS). Drag the left grip to reorder."
              :description="loadOrderCopy"
            />
            <div ref="modListEl" class="space-y-2">
              <div v-for="(mod, idx) in mods" :key="mod.id">
                <WorkspaceModCard
                  :mod="mod"
                  :index="idx"
                  :expanded="!!expandedIds[mod.id]"
                  :thumb-url="wsStore.thumbUrls[mod.id]"
                  @update:expanded="setModExpanded(mod.id, $event)"
                  @patch="patchMod(mod, $event)"
                  @calendar="(eras) => setModCalendar(mod, eras)"
                  @import-ignore="importIgnore(mod)"
                  @export-ignore="exportIgnore(mod)"
                  @remove="removeMod(mod)"
                />
              </div>
            </div>
            <p v-if="!mods.length" class="text-sm text-muted">No mods yet.</p>
            <div class="flex flex-wrap gap-2">
              <UButton
                label="Add existing mod to workspace"
                icon="i-lucide-folder-plus"
                variant="outline"
                @click="addMod"
              />
              <UButton
                label="Create new mod"
                icon="i-lucide-package-plus"
                variant="outline"
                @click="creatingMod = !creatingMod"
              />
            </div>
            <CreateModForm
              v-if="creatingMod && page?.workspace"
              :game-id="page.workspace.gameId"
              :loc-lang="locLang"
              :supported-version="supportedVersion"
              @created="onModCreated"
            />
          </div>

          <div v-else-if="section === 'editor'" class="space-y-5">
            <UFormField label="Diagnostics" :description="COPY.editor">
              <DiagnosticSettings />
            </UFormField>
          </div>
          <div v-else-if="section === 'danger'" class="grid grid-cols-1 gap-x-6 gap-y-5 md:grid-cols-2">
            <UFormField label="Remove workspace" :description="COPY.remove">
              <UButton label="Remove workspace" color="error" variant="outline" size="sm" @click="removeOpen = true" />
            </UFormField>
          </div>
        </div>
        <div v-if="showFooter" class="flex justify-end gap-2 border-t border-default px-6 py-3">
          <UButton label="Cancel" color="neutral" variant="outline" @click="applyWorkspace" />
          <UButton label="Save" :loading="savingOverview" @click="saveSection" />
        </div>
      </div>
    </div>
    <RemoveWorkspaceModal v-model:open="removeOpen" :name="name" :loading="deleting" @confirm="deleteThis()" />
  </div>
</template>
