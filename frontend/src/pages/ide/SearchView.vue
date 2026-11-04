<!--
  The sidebar's search: the query form, the hits grouped by file, and Replace behind a confirmation. A hit opens its
  file in the editor, at the match.
-->
<script setup lang="ts">
import { computed, shallowRef } from "vue";
import { useMutation, useQuery } from "@pinia/colada";
import { watchDebounced } from "@vueuse/core";

import * as searchApi from "@services/search/service";
import type { Site } from "@services/lsp/models";
import type { Query } from "@services/search/models";

import { confirm } from "../../lib/feedback";
import { plural } from "../../lib/format";
import { rootsQuery } from "../../queries";
import { useEditorStore } from "../../stores/editor";
import { useSearchStore } from "../../stores/search";
import { useWorkspaceStore } from "../../stores/workspace";
import OriginChip from "../../ui/OriginChip.vue";

/** OPEN_HITS is how many hits are listed open before the remaining files start folded. */
const OPEN_HITS = 500;

const editor = useEditorStore();
const search = useSearchStore();
const workspace = useWorkspaceStore();
const toast = useToast();

const text = shallowRef("");
const replacement = shallowRef("");
const regex = shallowRef(false);
const matchCase = shallowRef(false);
const wholeWord = shallowRef(false);
const include = shallowRef("");
const exclude = shallowRef("");
const disabled = shallowRef(new Set<string>());
// the files the user clicked; each shows the opposite of its default fold
const flips = shallowRef(new Set<string>());

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
// a folded file's lines are not split around their match until it opens
const files = computed(() => {
  let lineCount = 0;
  return search.current().files.map(({ place, hits }) => {
    const count = hits?.length ?? 0;
    const open = lineCount < OPEN_HITS !== flips.value.has(place.key);
    if (open) lineCount += count;
    const folder = workspace.describeFolder(place);
    return { place, count, lines: open ? (hits ?? []).map(describe) : [], open, folder };
  });
});
const count = computed(() => files.value.reduce((sum, file) => sum + file.count, 0));

const TOGGLES = [
  { label: "Regular expression", icon: "i-lucide-regex", state: regex },
  { label: "Match case", icon: "i-lucide-case-sensitive", state: matchCase },
  { label: "Whole word", icon: "i-lucide-whole-word", state: wholeWord },
];
const GLOBS = [
  { label: "Files to include", text: include, placeholder: "common/**, *.txt" },
  { label: "Files to exclude", text: exclude, placeholder: "gfx/**" },
];

watchDebounced([text, regex, matchCase, wholeWord, include, exclude, disabled], run, { debounce: 300 });

/** run starts the search the form describes, if it has a text. */
function run(): void {
  if (text.value.trim()) search.search(query.value);
}

/** tick includes an origin in the search if ticked, else excludes it. */
function tick(origin: string, isTicked: boolean): void {
  const next = new Set(disabled.value);
  if (isTicked) next.delete(origin);
  else next.add(origin);
  disabled.value = next;
}

/** describe splits a hit's line around its match, with the text before the match trimmed at its start. */
function describe(hit: Site): { line: number; column: number; before: string; match: string; after: string } {
  const { text: line = "", range } = hit;
  return {
    line: range.startLineNumber,
    column: range.startColumn,
    before: line.slice(0, range.startColumn - 1).trimStart(),
    match: line.slice(range.startColumn - 1, range.endColumn - 1),
    after: line.slice(range.endColumn - 1),
  };
}

/** flip folds an open file's hits, or opens a folded file's. */
function flip(key: string): void {
  const next = new Set(flips.value);
  if (!next.delete(key)) next.add(key);
  flips.value = next;
}

const { mutate: replaceAll } = useMutation({
  mutation: async () => {
    const body = `Every match of “${text.value}” in the mods' files becomes “${replacement.value}”. The game's files are not changed, and a file open in the editor is skipped.`;
    if (!(await confirm.open({ title: "Replace all?", body, confirm: "Replace all" }))) return 0;
    return searchApi.ReplaceText(query.value, replacement.value);
  },
  onSuccess: (replaced) => {
    if (replaced) toast.add({ title: `Replaced ${plural(replaced, "match", "matches")}` });
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
              :color="option.state.value ? 'primary' : 'neutral'"
              :variant="option.state.value ? 'soft' : 'ghost'"
              :aria-label="option.label"
              :aria-pressed="option.state.value"
              @click="option.state.value = !option.state.value"
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
    <section
      v-if="search.current().title || (text.trim() && search.current().query)"
      class="border-t border-default py-1"
    >
      <p class="flex flex-wrap items-center gap-x-2 px-2.5 pb-1 text-11 text-dimmed">
        <span v-if="search.current().title" class="font-medium text-toned">{{ search.current().title }}</span>
        {{ plural(count, "result") }} in {{ plural(files.length, "file") }}
        <span v-if="search.current().running">· searching…</span>
        <UBadge
          v-if="search.current().capped"
          label="Stopped here: narrow the search to see the rest"
          color="warning"
          variant="subtle"
          size="sm"
        />
      </p>
      <template v-for="file in files" :key="file.place.key">
        <button
          type="button"
          class="flex h-6 w-full items-center gap-1.5 ps-1.5 pe-2.5 text-start text-12 whitespace-nowrap hover:bg-accented"
          @click="flip(file.place.key)"
        >
          <UIcon
            :name="file.open ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'"
            class="size-3.5 shrink-0 text-dimmed"
          />
          <OriginChip :origin="file.place.origin" dot />
          <span class="shrink-0 text-highlighted">{{ file.place.name }}</span>
          <span class="min-w-0 truncate text-11 text-dimmed">{{ file.folder }}</span>
          <UBadge :label="String(file.count)" size="sm" class="ms-auto shrink-0" />
        </button>
        <template v-if="file.open">
          <button
            v-for="(hit, index) in file.lines"
            :key="index"
            type="button"
            class="flex h-5.5 w-full items-center ps-7 pe-2.5 text-start font-mono text-11 whitespace-nowrap text-toned hover:bg-accented"
            :title="`${file.place.rel}:${hit.line}`"
            @click="editor.show(file.place, { lineNumber: hit.line, column: hit.column }, 'navigate')"
          >
            <span class="truncate"
              >{{ hit.before }}<b class="rounded-xs bg-(--accent-soft) font-medium text-highlighted">{{ hit.match }}</b
              >{{ hit.after }}</span
            >
          </button>
        </template>
      </template>
      <p v-if="!files.length && !search.current().running" class="px-2.5 py-1 text-12 text-dimmed">No results.</p>
    </section>
  </div>
</template>
