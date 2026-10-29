<!-- One section of the settings catalog as fields, as Settings shows it. -->
<script lang="ts">
/** SETTING_ROWS holds the classes of a list of settings, each row with room above and below and a faint line under. */
export const SETTING_ROWS = "flex flex-col *:border-b *:border-accented *:py-2.5";
</script>

<script setup lang="ts">
import { computed } from "vue";
import { useQuery } from "@pinia/colada";
import { range } from "es-toolkit";

import { TOOL_CHOICES } from "../app/pages";
import { TOPICS } from "../lib/format";
import { codesQuery, settingsCatalogQuery } from "../queries";
import { useSettingsStore } from "../stores/settings";
import ValueField from "./ValueField.vue";
import type { Setting } from "@services/app/models";

const CONTROLS = ["switch", "choice", "number", "text"] as const;

const { section } = defineProps<{ section: string }>();

const settings = useSettingsStore();
const { data: catalog } = useQuery(settingsCatalogQuery);
const { data: codes } = useQuery(codesQuery);
// the Appearance page draws the theme as tiles
const rows = computed(() => catalog.value?.filter((row) => row.section === section && row.key !== "appearance.theme"));
// the checks turned off, by id
const off = computed(() => settings.value("ide.checks").split(",").filter(Boolean));
/** The checks' switches, grouped by topic. */
const groups = computed(() =>
  Object.entries(TOPICS).map(([topic, title]) => ({
    title,
    codes: (codes.value ?? []).filter((code) => code.topic === topic),
  })),
);

/** classifyControl returns the control that edits a setting of the given type. A type with no control is text. */
function classifyControl(type: string) {
  return CONTROLS.find((control) => control === type) ?? "text";
}

/** describeScales returns one tab for each size of the interface the given setting allows, ten percent apart. */
function describeScales(row: Setting) {
  return range(row.min ?? 0, (row.max ?? 0) + 1, 10).map((size) => ({ label: `${size}%`, value: `${size}` }));
}
</script>

<template>
  <div :class="SETTING_ROWS">
    <template v-for="row in rows" :key="row.key">
      <UFormField
        v-if="row.key === 'appearance.scale' || row.key === 'appearance.pages'"
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
      <UFormField v-else-if="row.key === 'ide.checks'" :label="row.label" :description="row.help" class="ps-3.5 pt-3">
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
        :kind="classifyControl(row.type)"
        :value="settings.value(row.key)"
        :label="row.label"
        :help="row.help"
        :choices="row.choices ?? undefined"
        :min="row.min"
        :max="row.max"
        @update:value="settings.write(row.key, $event)"
      />
    </template>
  </div>
</template>
