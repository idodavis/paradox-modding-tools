<script setup lang="ts">
/**
 * The IDE's settings: a card opened in the middle of the window
 * from the gear at the end of the sidebar's row. Editor, Language, Checks, Explorer and Wiki; each applies
 * to every workspace and its tab resets to the defaults.
 */
import { computed, shallowRef } from "vue";
import { useQuery } from "@pinia/colada";
import { watchDebounced } from "@vueuse/core";
import { codesQuery } from "../../queries";
import { useEditorStore } from "../../stores/editor";
import { switchesOf, useSettingsStore } from "../../stores/settings";
import { TOAST_PREFS, useWikiPrefs } from "../../composables/useWikiPrefs";
import SettingRow from "../../ui/SettingRow.vue";

const TABS = ["editor", "language", "checks", "explorer", "wiki"] as const;
type Tab = (typeof TABS)[number];
const isTab = (value: string | number): value is Tab => TABS.some((name) => name === value);

const settings = useSettingsStore();
const editor = useEditorStore();
const { toastPref, setToastPref } = useWikiPrefs();
const tab = shallowRef<Tab>("editor");

// the list is the backend's (lsp.Codes); Diagnose reads ide.diag.<code> on every call
const { data: codes } = useQuery(codesQuery);

const tabs = computed(() => [
  { label: "Editor", value: "editor", slot: "editor" as const },
  { label: "Explorer", value: "explorer", slot: "explorer" as const },
  { label: "Language", value: "language", slot: "language" as const },
  { label: "Checks", value: "checks", slot: "checks" as const, badge: codes.value?.length },
  { label: "Wiki", value: "wiki", slot: "wiki" as const },
]);

const INDENT = [
  { label: "Tabs", value: "tabs" },
  { label: "Spaces", value: "spaces" },
];
const DENSITY = [
  { label: "Compact", value: "compact" },
  { label: "Default", value: "default" },
  { label: "Relaxed", value: "relaxed" },
];
const LINE_HEIGHT = [
  { label: "Compact", value: "compact" },
  { label: "Roomy", value: "roomy" },
];
// fonts to pick from; any font installed can be typed
const FONTS = [
  "Geist Mono",
  "Cascadia Mono",
  "Cascadia Code",
  "Consolas",
  "JetBrains Mono",
  "Fira Code",
  "DejaVu Sans Mono",
];
const font = shallowRef(settings.editorFont);
watchDebounced(font, (name) => settings.set("ui.editorFont", name?.trim() ?? ""), { debounce: 500 });

function setNumber(key: "ui.editorFontSize" | "ui.ideUiFontSize" | "ide.indentSize", value: string | number): void {
  settings.set(key, String(value));
}

/** Every setting of the open tab back to its default. */
function resetTab(): void {
  const keys: Record<Tab, string[]> = {
    editor: [
      "ui.editorFont",
      "ide.lineHeight",
      "ui.editorFontSize",
      "ide.indent",
      "ide.indentSize",
      ...switchesOf("editor").map((one) => one.key),
    ],
    language: switchesOf("language").map((one) => one.key),
    checks: (codes.value ?? []).map((check) => `ide.diag.${check.code}`),
    explorer: ["ui.ideUiFontSize", "ide.density", ...switchesOf("explorer").map((one) => one.key)],
    wiki: switchesOf("wiki").map((one) => one.key),
  };
  if (tab.value === "wiki") setToastPref("show");
  if (tab.value === "editor") font.value = "Geist Mono";
  settings.reset(keys[tab.value]);
}
</script>

<template>
  <UButton
    icon="i-lucide-settings"
    square
    :ui="{ leadingIcon: 'size-4.5' }"
    :variant="editor.ideSettingsOpen ? 'soft' : 'ghost'"
    aria-label="IDE settings"
    @click="editor.ideSettingsOpen = true"
  />
  <!-- one size for every tab, the Editor tab's (the tallest); a longer tab scrolls -->
  <UModal v-model:open="editor.ideSettingsOpen" :ui="{ content: 'h-163 max-w-158' }">
    <template #content>
      <div class="flex min-h-0 w-158 flex-1 flex-col overflow-hidden">
        <div class="flex h-10 shrink-0 items-center gap-2 ps-3.5 pe-1.5">
          <UIcon name="i-lucide-settings" class="size-3.5 text-dimmed" />
          <span class="text-13 font-semibold">IDE settings</span>
          <span class="grow" />
          <UButton
            icon="i-lucide-x"
            size="sm"
            square
            variant="ghost"
            aria-label="Close"
            @click="editor.ideSettingsOpen = false"
          />
        </div>
        <UTabs
          :model-value="tab"
          :items="tabs"
          variant="link"
          :ui="{ root: 'min-h-0 flex-1', list: 'border-t', content: 'flex min-h-0 flex-1 flex-col overflow-y-auto' }"
          @update:model-value="tab = isTab($event) ? $event : 'editor'"
        >
          <template #editor>
            <div class="divide-y divide-default">
              <SettingRow label="Font" hint="Any font installed on this computer, by name">
                <UInputMenu v-model="font" mode="autocomplete" :items="FONTS" size="sm" class="w-40" />
              </SettingRow>
              <SettingRow label="Line height" hint="The room between the editor's lines">
                <UTabs
                  :model-value="settings.roomy ? 'roomy' : 'compact'"
                  :items="LINE_HEIGHT"
                  :content="false"
                  size="xs"
                  @update:model-value="settings.set('ide.lineHeight', String($event))"
                />
              </SettingRow>
              <SettingRow label="Editor text size" hint="Code in the editor, peek and compare">
                <UInput
                  type="number"
                  size="sm"
                  class="w-19"
                  :model-value="settings.editorFontSize"
                  @update:model-value="setNumber('ui.editorFontSize', $event)"
                >
                  <template #trailing><span class="text-11 text-dimmed">px</span></template>
                </UInput>
              </SettingRow>
              <SettingRow v-for="row in switchesOf('editor')" :key="row.key" :label="row.label" :hint="row.hint">
                <USwitch :model-value="settings.on(row.key)" @update:model-value="settings.setOn(row.key, $event)" />
              </SettingRow>
              <SettingRow label="Indentation" hint="For new lines; a file keeps its own">
                <UTabs
                  :model-value="settings.indentSpaces ? 'spaces' : 'tabs'"
                  :items="INDENT"
                  :content="false"
                  size="xs"
                  @update:model-value="settings.set('ide.indent', String($event))"
                />
                <UInput
                  type="number"
                  size="sm"
                  class="w-14"
                  :min="1"
                  :max="8"
                  :model-value="settings.indentSize"
                  @update:model-value="setNumber('ide.indentSize', $event)"
                />
              </SettingRow>
            </div>
          </template>
          <template #language>
            <div class="divide-y divide-default">
              <SettingRow v-for="row in switchesOf('language')" :key="row.key" :label="row.label" :hint="row.hint">
                <USwitch :model-value="settings.on(row.key)" @update:model-value="settings.setOn(row.key, $event)" />
              </SettingRow>
            </div>
          </template>
          <template #checks>
            <div class="flex items-center gap-1.5 border-b border-default bg-muted px-3.5 py-2 text-11 text-dimmed">
              <UIcon name="i-lucide-info" class="size-3.5" />
              Hides a check in the editor only. Health's counts do not change.
            </div>
            <div class="min-h-0 divide-y divide-default overflow-y-auto">
              <SettingRow v-for="check in codes ?? []" :key="check.code" :label="check.summary" :hint="check.code" mono>
                <USwitch
                  :model-value="settings.on(`ide.diag.${check.code}`, check.default)"
                  @update:model-value="settings.setOn(`ide.diag.${check.code}`, $event)"
                />
              </SettingRow>
            </div>
          </template>
          <template #explorer>
            <div class="divide-y divide-default">
              <SettingRow label="Explorer and panel text size" hint="The explorer, Problems, Search and the Wiki">
                <UInput
                  type="number"
                  size="sm"
                  class="w-19"
                  :model-value="settings.ideUiFontSize"
                  @update:model-value="setNumber('ui.ideUiFontSize', $event)"
                >
                  <template #trailing><span class="text-11 text-dimmed">px</span></template>
                </UInput>
              </SettingRow>
              <SettingRow label="Row density" hint="Compact fits more; Relaxed is easier to hit">
                <UTabs
                  :model-value="settings.treeDensity"
                  :items="DENSITY"
                  :content="false"
                  size="xs"
                  @update:model-value="settings.set('ide.density', String($event))"
                />
              </SettingRow>
              <SettingRow v-for="row in switchesOf('explorer')" :key="row.key" :label="row.label" :hint="row.hint">
                <USwitch :model-value="settings.on(row.key)" @update:model-value="settings.setOn(row.key, $event)" />
              </SettingRow>
            </div>
          </template>
          <template #wiki>
            <div class="divide-y divide-default">
              <SettingRow v-for="row in switchesOf('wiki')" :key="row.key" :label="row.label" :hint="row.hint">
                <USwitch :model-value="settings.on(row.key)" @update:model-value="settings.setOn(row.key, $event)" />
              </SettingRow>
              <SettingRow label="Suggestion toast" hint="A note when the Wiki has pages for a new file">
                <USelect
                  :model-value="toastPref"
                  :items="TOAST_PREFS"
                  value-key="value"
                  variant="subtle"
                  size="sm"
                  class="w-33"
                  @update:model-value="setToastPref"
                />
              </SettingRow>
            </div>
          </template>
        </UTabs>
        <div
          class="flex h-9 shrink-0 items-center gap-1.5 border-t border-default bg-muted ps-3.5 pe-1.5 text-11 text-dimmed"
        >
          Applies to every workspace
          <span class="grow" />
          <UButton
            :label="tab === 'checks' ? 'Reset the checks' : 'Reset this tab'"
            variant="ghost"
            size="xs"
            @click="resetTab"
          />
        </div>
      </div>
    </template>
  </UModal>
</template>
