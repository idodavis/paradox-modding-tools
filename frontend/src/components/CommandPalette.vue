<script setup lang="ts">
/**
 * The command palette (the CommandPalette and PaletteSettings boards), opened by the top bar's search or
 * Ctrl K on every page: the pages, the workspaces and every setting. A setting that is on or off flips
 * where you are; one with a value opens the page or the card that sets it.
 */
import { computed } from "vue";
import { useRouter, type RouteLocationRaw } from "vue-router";
import { useQuery } from "@pinia/colada";
import type { CommandPaletteGroup, CommandPaletteItem } from "@nuxt/ui";
import { ideSettingsOpen } from "./IdeSettingsCard.vue";
import { useWorkspacePrefs } from "../composables/useWorkspaceSave";
import { codesQuery, workspacesQuery } from "../queries";
import { useSettingsStore } from "../stores/settings";
import { useWorkspaceStore } from "../stores/workspace";
import {
  PAGES,
  SETTINGS,
  SETTINGS_SECTIONS,
  WORKSPACE_TOOLS,
  workspaceHomeRoute,
  type SettingSwitch,
} from "../workspaceTools";

const emit = defineEmits<{ close: [] }>();

const router = useRouter();
const store = useWorkspaceStore();
const settings = useSettingsStore();
const { mutate: savePrefs } = useWorkspacePrefs();
const { data: workspaces } = useQuery(workspacesQuery);
const { data: codes } = useQuery(codesQuery);

type Item = CommandPaletteItem & { words?: string; scope?: string; on?: boolean; opens?: string };

function go(to: RouteLocationRaw): void {
  void router.push(to);
  emit("close");
}

/** A switch's state and how it flips, where it has a workspace to flip on. */
function flip(id: SettingSwitch): { on: boolean; set: (on: boolean) => void } | undefined {
  const ws = store.activeWorkspace;
  switch (id) {
    case "matchGame":
      return { on: settings.matchGameTheme, set: (on) => void settings.setMatchGame(on) };
    case "followSystem":
      return { on: settings.appearance === "system", set: (on) => void settings.followSystem(on) };
    case "reopenTabs":
      return ws && { on: !ws.resetIdeOnOpen, set: (on) => savePrefs({ ws, resetIdeOnOpen: !on }) };
    case "hideBinaries":
      return ws && { on: ws.hideExplorerBinaries, set: (on) => savePrefs({ ws, hideExplorerBinaries: on }) };
  }
}

function switchItem(label: string, scope: string, on: boolean, set: (on: boolean) => void, words = ""): Item {
  return {
    label,
    prefix: "Toggle:",
    icon: "i-lucide-check",
    slot: "switch",
    scope,
    on,
    words,
    onSelect: () => set(!on),
  };
}

const groups = computed<CommandPaletteGroup<Item>[]>(() => {
  const id = store.activeWorkspaceId;
  const settingsAt = (section: string) => ({ name: "workspace-settings" as const, params: { id }, query: { section } });
  const pages: Item[] = [
    { label: PAGES.library.title, icon: PAGES.library.icon, onSelect: () => go({ name: "library" }) },
    { label: "New workspace", icon: "i-lucide-plus", onSelect: () => go({ name: "wizard" }) },
    ...(store.activeWorkspace
      ? [
          ...WORKSPACE_TOOLS.filter((tool) => settings.visibleTools.includes(tool.name)).map((tool) => ({
            label: tool.label,
            icon: tool.icon,
            onSelect: () => go({ name: tool.name, params: { id } }),
          })),
          { label: PAGES["workspace-settings"].title, icon: PAGES["workspace-settings"].icon, onSelect: () => go(settingsAt("general")) },
        ]
      : []),
  ];
  const others: Item[] = (workspaces.value ?? []).map((ws) => ({
    label: ws.name,
    suffix: store.game(ws.gameId)?.shortName,
    icon: ws.id === id ? "i-lucide-check" : "i-lucide-folder",
    onSelect: () => go(workspaceHomeRoute(ws, settings.visibleTools)),
  }));
  const switches: Item[] = [
    ...SETTINGS.flatMap((setting) => {
      const state = setting.flips && flip(setting.flips);
      if (!state) return [];
      const scope = setting.section === "ide" ? "IDE" : SETTINGS_SECTIONS[setting.section].scope;
      return [switchItem(setting.label, scope, state.on, state.set, setting.words)];
    }),
    ...WORKSPACE_TOOLS.map((tool) =>
      switchItem(`Show the ${tool.label} page`, "App", settings.visibleTools.includes(tool.name), (on) => void settings.showTool(tool.name, on)),
    ),
    ...(codes.value ?? []).map((check) =>
      switchItem(`Check: ${check.summary}`, "IDE", settings.checkOn(check.code, check.default), (on) =>
        void settings.setCheck(check.code, on), check.code),
    ),
  ];
  // a value opens where it is set: the IDE's settings card, or the settings page at its section
  const values: Item[] = !id ? [] : SETTINGS.filter((setting) => !setting.flips).map((setting) => {
    const section = setting.section === "ide" ? undefined : SETTINGS_SECTIONS[setting.section];
    return {
      label: setting.label,
      words: setting.words,
      icon: "i-lucide-settings",
      slot: "value",
      opens: !section ? "opens IDE settings" : section.scope === "App" ? `opens ${section.label}` : "opens workspace settings",
      onSelect: () => {
        if (section) return go(settingsAt(setting.section));
        go({ name: "workspace-ide", params: { id } });
        ideSettingsOpen.value = true;
      },
    };
  });
  return [
    { id: "pages", label: "Pages", items: pages },
    { id: "workspaces", label: "Workspaces", items: others },
    { id: "switches", label: "Settings you can switch", items: switches },
    { id: "values", label: "Settings with a value", items: values },
  ];
});

const FUSE = { fuseOptions: { keys: ["label", "prefix", "suffix", "words"] }, resultLimit: 40 };
</script>

<template>
  <UModal :ui="{ content: 'top-14 translate-y-0 sm:max-w-160' }">
    <template #content>
      <UCommandPalette
        :groups="groups"
        :fuse="FUSE"
        placeholder="Search pages, workspaces and settings"
        :close="true"
        class="max-h-120"
        @update:open="emit('close')"
      >
        <template #close>
          <UKbd value="Esc" />
        </template>
        <template #switch-trailing="{ item }">
          <UBadge :label="item.scope" size="sm" />
          <USwitch :model-value="item.on" size="sm" class="pointer-events-none" />
        </template>
        <template #value-trailing="{ item }">
          <span class="text-11 text-dimmed">{{ item.opens }}</span>
        </template>
        <template #footer>
          <p class="text-11 text-dimmed">
            Enter flips a switch where you are. A setting with a value opens the page or card that sets it.
          </p>
        </template>
      </UCommandPalette>
    </template>
  </UModal>
</template>
