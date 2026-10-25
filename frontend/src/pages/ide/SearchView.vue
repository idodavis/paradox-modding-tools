<!-- The sidebar's search: the query form, the hits, and Replace behind a confirmation. -->
<script setup lang="ts">
import { computed, shallowRef } from "vue";
import { useMutation, useQuery } from "@pinia/colada";
import { watchDebounced } from "@vueuse/core";

import * as searchApi from "@services/search/service";
import type { Query } from "@services/search/models";

import { plural } from "../../lib/format";
import { rootsQuery } from "../../queries";
import { useSearchStore } from "../../stores/search";
import { useWorkspaceStore } from "../../stores/workspace";
import ConfirmModal from "../../ui/ConfirmModal.vue";
import OriginChip from "../../ui/OriginChip.vue";
import SearchHits from "./SearchHits.vue";

const search = useSearchStore();
const workspace = useWorkspaceStore();
const toast = useToast();
const confirm = useOverlay().create(ConfirmModal);

const text = shallowRef("");
const replacement = shallowRef("");
const regex = shallowRef(false);
const matchCase = shallowRef(false);
const wholeWord = shallowRef(false);
const include = shallowRef("");
const exclude = shallowRef("");
const disabled = shallowRef(new Set<string>());

const { data: roots } = useQuery(() => ({ ...rootsQuery, enabled: workspace.isOpen() }));
const origins = computed(() => [...new Set((roots.value ?? []).map((root) => root.origin))]);
const query = computed<Query>(() => ({
  text: text.value,
  regex: regex.value,
  matchCase: matchCase.value,
  wholeWord: wholeWord.value,
  include: include.value,
  exclude: exclude.value,
  origins: origins.value.filter((origin) => !disabled.value.has(origin)),
}));

const TOGGLES = [
  { label: "Regular expression", icon: "i-lucide-regex", on: regex },
  { label: "Match case", icon: "i-lucide-case-sensitive", on: matchCase },
  { label: "Whole word", icon: "i-lucide-whole-word", on: wholeWord },
];
const GLOBS = [
  { label: "Files to include", text: include, placeholder: "common/**, *.txt" },
  { label: "Files to exclude", text: exclude, placeholder: "gfx/**" },
];

watchDebounced([text, regex, matchCase, wholeWord, include, exclude, disabled], run, { debounce: 300 });

/** Starts the search the form describes, if it has a text. */
function run(): void {
  if (text.value.trim()) search.search(query.value);
}

/** Ticks or unticks an origin in the search. */
function tick(origin: string, on: boolean): void {
  const next = new Set(disabled.value);
  if (on) next.delete(origin);
  else next.add(origin);
  disabled.value = next;
}

const { mutate: replaceAll } = useMutation({
  mutation: async () => {
    const body = `Every match of “${text.value}” in the mods' files becomes “${replacement.value}”. The game's files are not changed, and a file open in the editor is skipped.`;
    if (!(await confirm.open({ title: "Replace all?", body, confirm: "Replace all" }))) return 0;
    return searchApi.ReplaceText(query.value, replacement.value);
  },
  onSuccess: (count) => {
    if (count) toast.add({ title: `Replaced ${plural(count, "match", "matches")}` });
  },
});
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col overflow-y-auto">
    <div class="flex flex-col gap-2.5 p-2.5" @keydown.enter="run">
      <UInput v-model="text" placeholder="Search" size="sm" autofocus :ui="{ trailing: 'pe-1' }">
        <template #trailing>
          <UButton
            v-if="text"
            icon="i-lucide-x"
            size="xs"
            square
            variant="ghost"
            aria-label="Clear"
            @click="text = ''"
          />
          <UTooltip v-for="option in TOGGLES" :key="option.label" :text="option.label">
            <UButton
              :icon="option.icon"
              size="xs"
              square
              :color="option.on.value ? 'primary' : 'neutral'"
              :variant="option.on.value ? 'soft' : 'ghost'"
              :aria-label="option.label"
              :aria-pressed="option.on.value"
              @click="option.on.value = !option.on.value"
            />
          </UTooltip>
        </template>
      </UInput>
      <UInput v-model="replacement" placeholder="Replace" size="sm" :ui="{ trailing: 'pe-1' }">
        <template #trailing>
          <UButton
            v-if="replacement"
            icon="i-lucide-x"
            size="xs"
            square
            variant="ghost"
            aria-label="Clear"
            @click="replacement = ''"
          />
          <UTooltip text="Replace all">
            <UButton
              icon="i-lucide-replace-all"
              size="xs"
              square
              variant="ghost"
              aria-label="Replace all"
              :disabled="!text.trim()"
              @click="replaceAll()"
            />
          </UTooltip>
        </template>
      </UInput>
      <UFormField v-for="glob in GLOBS" :key="glob.label" :label="glob.label">
        <UInput
          v-model="glob.text.value"
          :placeholder="glob.placeholder"
          size="sm"
          class="w-full font-mono"
          :ui="{ trailing: 'pe-1' }"
        >
          <template v-if="glob.text.value" #trailing>
            <UButton
              icon="i-lucide-x"
              size="xs"
              square
              variant="ghost"
              aria-label="Clear"
              @click="glob.text.value = ''"
            />
          </template>
        </UInput>
      </UFormField>
      <UFormField label="Folders">
        <div class="flex flex-col gap-1.5">
          <UCheckbox
            v-for="origin in origins"
            :key="origin"
            :model-value="!disabled.has(origin)"
            @update:model-value="tick(origin, !!$event)"
          >
            <template #label>
              <span class="flex items-center gap-1.5 text-12 text-toned">
                <OriginChip :origin="origin" dot />{{ workspace.originName(origin) }}
              </span>
            </template>
          </UCheckbox>
        </div>
      </UFormField>
    </div>
    <div v-if="text.trim() || search.current().title" class="border-t border-default">
      <SearchHits />
    </div>
  </div>
</template>
