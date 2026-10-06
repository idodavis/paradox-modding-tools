<script lang="ts">
import { shallowRef } from "vue";

/** Whether the card is open; the palette opens it. */
export const ideSettingsOpen = shallowRef(false);
</script>

<script setup lang="ts">
/**
 * The IDE's settings (the IdeSettings board): a card opened from the gear on the IDE page, its tabs the
 * text sizes, the checks and the Wiki's toast. They apply to every workspace.
 */
import { computed } from "vue";
import { useQuery } from "@pinia/colada";
import { codesQuery } from "../queries";
import { useSettingsStore } from "../stores/settings";
import { GUIDE_PREF_ITEMS, useGuidePrefs } from "../composables/useGuidePrefs";

const settings = useSettingsStore();
const { toastPref, setToastPref } = useGuidePrefs();
const tab = shallowRef<string | number>("editor");

// the list is the backend's (lsp.Codes); Diagnose reads ide.diag.<code> on every call
const { data: codes } = useQuery(codesQuery);

const tabs = computed(() => [
  { label: "Editor", value: "editor", slot: "editor" as const },
  { label: "Checks", value: "checks", slot: "checks" as const, badge: codes.value?.length },
  { label: "Wiki", value: "wiki", slot: "wiki" as const },
]);

function setSize(key: "ui.editorFontSize" | "ui.ideUiFontSize", value: string | number): void {
  void settings.set(key, String(value));
}

/** Every setting of the open tab back to its default. */
function resetTab(): void {
  if (tab.value === "editor") {
    void settings.reset(["ui.editorFontSize", "ui.ideUiFontSize"]);
  } else if (tab.value === "checks") {
    void settings.reset((codes.value ?? []).map((check) => `ide.diag.${check.code}`));
  } else {
    void setToastPref("show");
  }
}
</script>

<template>
  <UPopover v-model:open="ideSettingsOpen" :content="{ align: 'end', side: 'bottom' }">
    <UButton icon="i-lucide-settings" size="sm" square :variant="ideSettingsOpen ? 'soft' : 'ghost'" aria-label="IDE settings" />
    <template #content>
      <div class="flex w-105 flex-col overflow-hidden">
        <div class="flex h-10 shrink-0 items-center gap-2 ps-3.5 pe-1.5">
          <UIcon name="i-lucide-settings" class="size-3.5 text-dimmed" />
          <span class="text-13 font-semibold">IDE settings</span>
          <span class="grow" />
          <UButton icon="i-lucide-x" size="sm" square variant="ghost" aria-label="Close" @click="ideSettingsOpen = false" />
        </div>
        <UTabs v-model="tab" :items="tabs" variant="link" :ui="{ list: 'border-t' }">
          <template #editor>
            <div class="divide-y divide-default">
              <SettingRow label="Editor text size" hint="Code in the editor, peek and compare">
                <UInput
                  type="number"
                  size="sm"
                  class="w-19"
                  :model-value="settings.editorFontSize"
                  @update:model-value="setSize('ui.editorFontSize', $event)"
                >
                  <template #trailing><span class="text-11 text-dimmed">px</span></template>
                </UInput>
              </SettingRow>
              <SettingRow label="Explorer and panel text size" hint="The explorer, Problems, Search and the Wiki">
                <UInput
                  type="number"
                  size="sm"
                  class="w-19"
                  :model-value="settings.ideUiFontSize"
                  @update:model-value="setSize('ui.ideUiFontSize', $event)"
                >
                  <template #trailing><span class="text-11 text-dimmed">px</span></template>
                </UInput>
              </SettingRow>
            </div>
          </template>
          <template #checks>
            <div class="flex items-center gap-1.5 border-b border-default bg-muted px-3.5 py-2 text-11 text-dimmed">
              <UIcon name="i-lucide-info" class="size-3.5" />
              Hides a check in the editor only. Health's counts do not change.
            </div>
            <div class="max-h-120 divide-y divide-default overflow-y-auto">
              <SettingRow v-for="check in codes ?? []" :key="check.code" :label="check.summary" :hint="check.code" mono>
                <USwitch
                  :model-value="settings.checkOn(check.code, check.default)"
                  @update:model-value="settings.setCheck(check.code, $event)"
                />
              </SettingRow>
            </div>
          </template>
          <template #wiki>
            <SettingRow label="Suggestion toast" hint="A note when the Wiki has pages for a new file">
              <USelect
                :model-value="toastPref"
                :items="[...GUIDE_PREF_ITEMS]"
                value-key="value"
                variant="subtle"
                size="sm"
                class="w-33"
                @update:model-value="setToastPref"
              />
            </SettingRow>
          </template>
        </UTabs>
        <div class="flex h-9 shrink-0 items-center gap-1.5 border-t border-default bg-muted ps-3.5 pe-1.5 text-11 text-dimmed">
          Applies to every workspace
          <span class="grow" />
          <UButton :label="tab === 'checks' ? 'Reset the checks' : 'Reset this tab'" variant="ghost" size="xs" @click="resetTab" />
        </div>
      </div>
    </template>
  </UPopover>
</template>
