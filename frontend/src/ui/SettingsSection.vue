<!-- One section of the settings catalog as fields. A setting the simple interface hides shows off and disabled. -->
<script lang="ts">
/** SETTING_ROWS holds the classes of a list of settings. */
export const SETTING_ROWS = "flex flex-col *:border-b *:border-accented *:py-2.5";
/** SETTING_HIGHLIGHT holds the classes of the row of the setting the palette opened Settings at. */
export const SETTING_HIGHLIGHT = "rounded-md bg-(--accent-soft)";
</script>

<script setup lang="ts">
import { computed } from "vue";
import { useQuery } from "@pinia/colada";
import { range } from "es-toolkit";

import { TOOL_CHOICES } from "../app/pages";
import { codesQuery, settingsCatalogQuery, topicsQuery } from "../queries";
import { useSettingsStore } from "../stores/settings";
import ValueField from "./ValueField.vue";
import type { Setting } from "@services/app/models";

const CONTROLS = ["switch", "choice", "number", "port", "text"] as const;
// The Appearance page draws these apart: the theme as tiles, and the simple interface's switch above them.
const DRAWN_APART = ["appearance.theme", "appearance.simple"];

const { section, highlight = "", keys = [] } = defineProps<{ section: string; highlight?: string; keys?: string[] }>();

const settings = useSettingsStore();
const { data: catalog } = useQuery(settingsCatalogQuery);
const { data: codes } = useQuery(codesQuery);
const { data: topics } = useQuery(topicsQuery);
const rows = computed(() =>
  catalog.value?.filter(
    (row) => row.section === section && (keys.length ? keys.includes(row.key) : !DRAWN_APART.includes(row.key)),
  ),
);
const off = computed(() => settings.value("ide.checks").split(",").filter(Boolean));
/** The checks' switches, grouped by topic. */
const groups = computed(() =>
  (topics.value ?? []).map((topic) => ({
    title: topic.label,
    codes: (codes.value ?? []).filter((code) => code.topic === topic.id),
  })),
);

/** markRow returns the attributes of a setting's row: the id Settings scrolls to, and any highlight. */
function markRow(key: string) {
  return { id: `setting-${key}`, class: key === highlight ? SETTING_HIGHLIGHT : undefined };
}

/** describeRow returns what a setting's row says under its label. */
function describeRow(row: Setting): string {
  return settings.isHidden(row.key) ? `Off in the simple interface. ${row.help}` : row.help;
}

/** classifyControl returns the control that edits a setting of the given type, text by default. */
function classifyControl(type: string) {
  return CONTROLS.find((control) => control === type) ?? "text";
}

/** describeScales returns one tab for each size of the interface the given setting allows. */
function describeScales(row: Setting) {
  return range(row.min ?? 0, (row.max ?? 0) + 1, 10).map((size) => ({ label: `${size}%`, value: `${size}` }));
}
</script>

<template>
  <div :class="SETTING_ROWS">
    <template v-for="row in rows" :key="row.key">
      <UFormField
        v-if="row.key === 'appearance.scale' || row.key === 'appearance.pages'"
        v-bind="markRow(row.key)"
        :label="row.label"
        :description="row.help"
        orientation="horizontal"
      >
        <UTabs
          v-if="row.key === 'appearance.scale'"
          :model-value="settings.value(row.key)"
          :items="describeScales(row)"
          :content="false"
          @update:model-value="settings.write(row.key, `${$event}`)"
        />
        <UCheckboxGroup
          v-else
          :model-value="settings.value(row.key).split(',')"
          :items="TOOL_CHOICES"
          orientation="horizontal"
          @update:model-value="settings.write(row.key, $event.join(','))"
        />
      </UFormField>
      <UFormField
        v-else-if="row.key === 'ide.checks'"
        v-bind="markRow(row.key)"
        :label="row.label"
        :description="row.help"
        class="ps-3.5 pt-3"
      >
        <div v-for="group in groups" :key="group.title" class="mt-3">
          <p class="text-13 font-medium text-highlighted">{{ group.title }}</p>
          <div class="mt-1 flex flex-col divide-y divide-accented">
            <ValueField
              v-for="code in group.codes"
              :key="code.id"
              kind="switch"
              :value="String(!off.includes(code.id))"
              :label="code.label"
              :help="code.id"
              @update:value="settings.writeItem('ide.checks', code.id, $event !== 'true')"
            />
          </div>
        </div>
      </UFormField>
      <ValueField
        v-else
        v-bind="markRow(row.key)"
        :kind="classifyControl(row.type)"
        :value="settings.isHidden(row.key) ? 'false' : settings.value(row.key)"
        :label="row.label"
        :help="describeRow(row)"
        :disabled="settings.isHidden(row.key)"
        :choices="row.choices ?? undefined"
        :min="row.min"
        :max="row.max"
        @update:value="settings.write(row.key, $event)"
      />
    </template>
  </div>
</template>
