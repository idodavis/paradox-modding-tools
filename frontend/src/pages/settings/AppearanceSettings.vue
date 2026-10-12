<script setup lang="ts">
/**
 * Settings › Appearance (the Themes board): the theme family and the game switch, following Windows'
 * light or dark (the top bar switches them), the interface scale, the pages a workspace shows, and the
 * active workspace's mod colors.
 */
import { computed } from "vue";
import { useMutation } from "@pinia/colada";
import { SetWorkspaceModSlot } from "@services/workspaceservice";
import { FONT_SCALES, THEMES, useSettingsStore, type ThemeFamily, type ThemeId } from "../../stores/settings";
import { modColor, updatePrefs, useWorkspaceStore } from "../../stores/workspace";
import { WORKSPACE_TOOLS, type WorkspaceToolName } from "../../app/pages";
import OriginChip from "../../ui/OriginChip.vue";
import SettingRow from "../../ui/SettingRow.vue";
import SettingSection from "./SettingSection.vue";
import ThemePreview from "./ThemePreview.vue";

const settings = useSettingsStore();
const workspace = useWorkspaceStore();

const SCALES = FONT_SCALES.map((percent) => ({ label: `${percent}%`, value: percent }));

function setScale(value: string | number): void {
  void settings.set("ui.fontScale", String(value));
}

function setToolShown(name: WorkspaceToolName, shown: boolean | "indeterminate"): void {
  void settings.showTool(name, shown === true);
}

/** A family's half in the mode showing now, which its tile previews. */
function halfOf(family: ThemeFamily): ThemeId {
  return `${family.id}-${settings.mode}`;
}

/** What a tile says beside its name. */
function tileMark(family: ThemeFamily): { badge?: { label: string; color: "primary" | "secondary" }; note?: string } {
  if (family.id === settings.theme.id) return { badge: { label: "Your pick", color: "primary" } };
  if (family.id === settings.activeFamily.id) return { badge: { label: "In use here", color: "secondary" } };
  return { note: family.note };
}

/** The game, then each mod, in the active workspace. */
const origins = computed(() => {
  if (!workspace.activeWorkspace) return [];
  const game = workspace.originVanilla;
  return [
    { id: game, label: workspace.originName(game), color: workspace.originColor(game) },
    ...workspace.workspaceMods.map((mod) => ({ id: mod.id, label: mod.name, color: modColor(mod) })),
  ];
});
const anyPicked = computed(
  () => !!workspace.activeWorkspace?.gameColor || workspace.workspaceMods.some((mod) => mod.color),
);

const { mutate: resetColors, isLoading: resetting } = useMutation({
  mutation: async () => {
    const ws = workspace.activeWorkspace;
    if (!ws) return;
    for (const mod of workspace.workspaceMods) {
      if (mod.color) await SetWorkspaceModSlot(ws.id, mod.id, mod.slot);
    }
    if (ws.gameColor) await updatePrefs({ ws, gameColor: "" });
  },
});
</script>

<template>
  <div class="flex flex-col">
    <div class="flex flex-col gap-0.5 pb-1">
      <h1 class="text-20">Appearance</h1>
      <p class="text-13 text-dimmed">Applies to every workspace and every PMT window.</p>
    </div>

    <SettingSection
      title="Theme"
      hint="Every theme is the same set of tokens with different values, so all pages and the editor change together. Each comes dark and light: switch with the sun and moon in the top bar."
    >
      <SettingRow
        label="Match the workspace's game"
        hint="Crusader Kings III workspaces use Crusader Kings. Turn off to use your pick everywhere."
      >
        <USwitch
          :model-value="settings.matchGameTheme"
          @update:model-value="settings.setOn('ui.matchGameTheme', $event)"
        />
      </SettingRow>
      <div class="grid grid-cols-3 gap-3.5">
        <button
          v-for="family in THEMES"
          :key="family.id"
          type="button"
          class="flex flex-col gap-2 rounded-tile border bg-muted p-2 text-start"
          :class="family.id === settings.theme.id ? 'border-primary ring-2 ring-primary/15' : 'border-default'"
          @click="settings.pickTheme(family.id)"
        >
          <ThemePreview :theme="halfOf(family)" />
          <span class="flex min-w-0 items-center gap-1.5 px-1 pb-0.5 text-12 font-medium whitespace-nowrap">
            {{ family.label }}
            <span class="grow" />
            <UBadge v-if="tileMark(family).badge" size="sm" v-bind="tileMark(family).badge" />
            <span v-else class="truncate text-11 font-normal text-dimmed">{{ tileMark(family).note }}</span>
          </span>
        </button>
      </div>
    </SettingSection>

    <SettingSection
      title="Light and dark"
      hint="The sun and moon in the top bar switch them on every page; switching stops following Windows."
    >
      <SettingRow label="Follow Windows" hint="Use the light or dark mode your system is set to.">
        <USwitch :model-value="settings.appearance === 'system'" @update:model-value="settings.followSystem($event)" />
      </SettingRow>
    </SettingSection>

    <SettingSection title="Text size">
      <template #hint>
        Scales every page. The editor and panel sizes are in the IDE's own
        <ULink
          v-if="workspace.activeWorkspaceId"
          :to="{ name: 'workspace-ide', params: { id: workspace.activeWorkspaceId } }"
          class="text-secondary"
          >settings</ULink
        >
        <template v-else>settings</template>.
      </template>
      <div class="flex flex-col gap-1.5">
        <span class="text-12 font-medium text-toned">Interface scale</span>
        <div>
          <UTabs :items="SCALES" :model-value="settings.fontScale" :content="false" @update:model-value="setScale" />
        </div>
      </div>
    </SettingSection>

    <SettingSection
      title="Pages"
      hint="The pages a workspace shows in the top bar and the palette. With none chosen, the IDE stays."
    >
      <div class="flex flex-wrap items-center gap-x-5 gap-y-2">
        <UCheckbox
          v-for="tool in WORKSPACE_TOOLS"
          :key="tool.name"
          :label="tool.label"
          :model-value="settings.visibleTools.includes(tool.name)"
          @update:model-value="setToolShown(tool.name, $event)"
        />
      </div>
    </SettingSection>

    <SettingSection
      title="Mod colors"
      hint="Each mod gets a color when it joins a workspace and keeps it. Pick one by hand in Mods and load order."
    >
      <div class="flex flex-wrap items-center gap-3">
        <OriginChip v-for="origin in origins" :key="origin.id" :label="origin.label" :color="origin.color" />
        <span class="grow" />
        <UButton
          label="Reset all to automatic"
          size="sm"
          :disabled="!anyPicked"
          :loading="resetting"
          @click="resetColors()"
        />
      </div>
    </SettingSection>
  </div>
</template>
