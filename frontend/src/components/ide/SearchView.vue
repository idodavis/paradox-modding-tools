<script setup lang="ts">
/**
 * Find in files (the Ide board's sidebar Search): a text, or a regular expression, matching case and whole words
 * when asked, in the folders picked and the files the include and exclude globs keep, searched as it is typed. Its
 * hits stream in under it as Go finds them (search:hits), drawn once a frame, as a peek lists references: by file,
 * named with its origin and folder, each line with its match in bold, the files past the first 500 hits folded; Find
 * all references lists there too. Replace all writes the mods' files (Go), and changes the
 * files open in the editor there, unsaved; the game's files are never changed.
 */
import { computed, onScopeDispose, shallowRef } from "vue";
import { watchDebounced } from "@vueuse/core";
import { storeToRefs } from "pinia";
import { useQuery } from "@pinia/colada";
import { Events } from "@wailsio/runtime";
import type { IdeRoot, TextSearchHit } from "@services/models";
import { Replace, TextSearch } from "@services/searchservice";
import { editorRange } from "../../ide/editorCore";
import { editText, openText } from "../../ide/monacoEditor";
import { fileName, plural } from "../../lib/format";
import { linesQuery } from "../../queries";
import { useIdeStore } from "../../stores/ide";
import { useWorkspaceStore } from "../../stores/workspace";
import ConfirmModal from "../ui/ConfirmModal.vue";

const { roots, hideBinaries } = defineProps<{ roots: IdeRoot[]; hideBinaries: boolean }>();

const ide = useIdeStore();
const { results } = storeToRefs(ide);
const store = useWorkspaceStore();
const toast = useToast();

const pattern = shallowRef("");
const replacement = shallowRef("");
const regex = shallowRef(false);
const matchCase = shallowRef(false);
const word = shallowRef(false);
const include = shallowRef("");
const exclude = shallowRef("");
// the folders searched: every root unless unticked
const skipped = shallowRef(new Set<string>());
const folders = computed(() => roots.filter((root) => !skipped.value.has(root.path)));
function pick(root: IdeRoot, on: boolean): void {
  const next = new Set(skipped.value);
  if (on) next.delete(root.path);
  else next.add(root.path);
  skipped.value = next;
}

const TOGGLES = [
  { label: "Regular expression", icon: "i-lucide-regex", on: regex },
  { label: "Match case", icon: "i-lucide-case-sensitive", on: matchCase },
  { label: "Whole word", icon: "i-lucide-whole-word", on: word },
];
const GLOBS = [
  { label: "Files to include", text: include, placeholder: "common/**, *.txt" },
  { label: "Files to exclude", text: exclude, placeholder: "gfx/**" },
];
const globs = (text: string) =>
  text
    .split(",")
    .map((glob) => glob.trim())
    .filter(Boolean);
// hidden binaries are searched no more than they are listed
const BINARY_GLOBS = ["*.{dll,exe,pdb,hash,hex,bin,dat}"];

/** The search asked, over some of the folders picked. */
function query(id: string, searched: IdeRoot[]) {
  return {
    id,
    pattern: pattern.value,
    isRegexp: regex.value,
    isCaseSensitive: matchCase.value,
    isWordMatch: word.value,
    maxResults: 0,
    folders: searched.map((root) => ({
      path: root.path,
      origin: root.origin,
      includes: globs(include.value),
      excludes: [...globs(exclude.value), ...(hideBinaries ? BINARY_GLOBS : [])],
    })),
  };
}

// the search shown, and its call
let searching = "";
let running: ReturnType<typeof TextSearch> | undefined;
async function run(): Promise<void> {
  running?.cancel();
  if (!pattern.value.trim()) return void (results.value = null);
  toggled.value = new Set();
  const id = crypto.randomUUID();
  const searched = folders.value;
  searching = id;
  results.value = { title: `“${pattern.value}”`, rows: [], done: false, capped: false };
  running = TextSearch(query(id, searched));
  const end = await running.catch((error: unknown) => {
    if (searching === id) toast.add({ title: "The search could not run", description: String(error), color: "error" });
    return null;
  });
  if (searching === id && results.value) results.value = { ...results.value, done: true, capped: !!end?.limitHit };
}

// each file's hits as they come, columns in bytes as the IDE's places are
let frame = 0;
const stop = Events.On("search:hits", ({ data }) => {
  const shown = results.value;
  if (data.id !== searching || !shown) return;
  (shown.rows ??= []).push(...(data.hits ?? []));
  // the rows are drawn once a frame, however many files' hits came in it
  frame ||= requestAnimationFrame(() => {
    frame = 0;
    if (results.value) results.value = { ...results.value };
  });
});
onScopeDispose(() => {
  stop();
  running?.cancel();
  cancelAnimationFrame(frame);
});

// a search runs as it is typed or changed, once the typing stops
watchDebounced([pattern, regex, matchCase, word, include, exclude, folders], () => void run(), { debounce: 300 });

// the results by file, in the order they came; a file is open until 500 hits show, folded after, either way until
// it is clicked (toggled)
const toggled = shallowRef(new Set<string>());
const files = computed(() => {
  const out = new Map<string, { path: string; rel: string; origin: string; rows: TextSearchHit[]; open: boolean }>();
  for (const row of results.value?.rows ?? []) {
    const file =
      out.get(row.path) ??
      out.set(row.path, { path: row.path, rel: row.rel, origin: row.origin, rows: [], open: false }).get(row.path)!;
    file.rows.push(row);
  }
  let shown = 0;
  for (const file of out.values()) {
    file.open = shown < 500 !== toggled.value.has(file.path);
    if (file.open) shown += file.rows.length;
  }
  return [...out.values()];
});
function toggle(path: string): void {
  const next = new Set(toggled.value);
  if (!next.delete(path)) next.add(path);
  toggled.value = next;
}
/** A file's folder, named by its origin as a peek names it. */
const folderOf = (file: { rel: string; origin: string }) =>
  [store.originName(file.origin), file.rel.slice(0, Math.max(0, file.rel.lastIndexOf("/")))]
    .filter(Boolean)
    .join(" › ");
// a reference comes without its line; the lines are read for those
const missing = computed(() => (results.value?.rows ?? []).filter((row) => !row.preview));
const { data: lines } = useQuery(() =>
  linesQuery({
    id: store.activeWorkspaceId,
    places: missing.value.map((row) => ({ path: row.path, line: row.line, col: row.col, definition: false })),
    live: true,
  }),
);
/** A result's line split around its match. */
function parts(row: TextSearchHit): [string, string, string] {
  const i = row.preview ? -1 : missing.value.indexOf(row);
  const text = row.preview || (lines.value?.[i]?.[0] ?? "");
  const { start, end } = editorRange(
    { start: { line: 0, character: row.col }, end: { line: 0, character: row.end } },
    () => text,
  );
  return [
    text.slice(0, start.character).trimStart(),
    text.slice(start.character, end.character),
    text.slice(end.character),
  ];
}

// Replace all: in the mods' folders picked; Go writes the files, the editor changes the ones it has open
const writable = computed(() => folders.value.filter((root) => !root.readOnly));
const confirm = useOverlay().create(ConfirmModal);
async function replaceAll(): Promise<void> {
  const text = `Every match of “${pattern.value}” in the mods' files becomes “${replacement.value}”. The game's files are not changed; a file open in the editor changes there, unsaved.`;
  if (!(await confirm.open({ title: "Replace all?", text, action: "Replace all" }))) return;
  // the open files go with their text in the editor, unsaved edits and all; Go replaces there as on disk, and
  // hands their new text back for the editor
  const open = Object.fromEntries(
    ide.openFiles.flatMap((path) => (openText(path) === undefined ? [] : [[path, openText(path) ?? ""]])),
  );
  const done = await Replace({ search: query("", writable.value), text: replacement.value, open }).catch(
    (error: unknown) => {
      toast.add({ title: "Replace all could not run", description: String(error), color: "error" });
      return null;
    },
  );
  if (!done) return;
  const texts = Object.entries(done.texts ?? {});
  for (const [path, text = ""] of texts) editText(path, () => text);
  toast.add({
    title: plural(done.replaced, "replacement"),
    description: `${plural(done.files - texts.length, "file")} written${texts.length ? `; ${plural(texts.length, "open file")} changed, not saved yet` : ""}.`,
    color: "success",
  });
  void run();
}
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col overflow-y-auto">
    <!-- it searches as it is typed; Enter in any field searches at once -->
    <div class="flex flex-col gap-2.5 p-2.5" @keydown.enter="run">
      <UInput v-model="pattern" placeholder="Search" size="sm" autofocus :ui="{ trailing: 'pe-1' }">
        <template #trailing>
          <UButton
            v-if="pattern"
            icon="i-lucide-x"
            size="xs"
            square
            variant="ghost"
            aria-label="Clear"
            @click="pattern = ''"
          />
          <UTooltip v-for="toggle in TOGGLES" :key="toggle.label" :text="toggle.label">
            <UButton
              :icon="toggle.icon"
              size="xs"
              square
              :color="toggle.on.value ? 'primary' : 'neutral'"
              :variant="toggle.on.value ? 'soft' : 'ghost'"
              :aria-label="toggle.label"
              :aria-pressed="toggle.on.value"
              @click="toggle.on.value = !toggle.on.value"
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
              :disabled="!pattern.trim() || !writable.length"
              @click="replaceAll"
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
            v-for="root in roots"
            :key="root.path"
            :model-value="!skipped.has(root.path)"
            @update:model-value="pick(root, !!$event)"
          >
            <template #label>
              <span class="flex items-center gap-1.5 text-12 text-toned">
                <OriginDot :color="store.originColor(root.origin ?? '')" small />{{ root.label }}
              </span>
            </template>
          </UCheckbox>
        </div>
      </UFormField>
    </div>
    <section v-if="results" class="border-t border-default py-1">
      <p class="flex flex-wrap items-center gap-x-2 px-2.5 pb-1 text-11 text-dimmed">
        {{ results.title }}: {{ plural(results.rows?.length ?? 0, "result") }} in {{ plural(files.length, "file") }}
        <span v-if="!results.done">· searching…</span>
        <UBadge v-if="results.capped" label="Stopped at the cap" color="warning" size="sm" />
      </p>
      <template v-for="file in files" :key="file.path">
        <button
          class="flex h-6 w-full items-center gap-1.5 ps-1.5 pe-2.5 text-start text-12 whitespace-nowrap hover:bg-accented"
          @click="toggle(file.path)"
        >
          <UIcon
            :name="file.open ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'"
            class="size-3.5 shrink-0 text-dimmed"
          />
          <OriginDot :color="store.originColor(file.origin)" small />
          <span class="shrink-0 text-highlighted">{{ fileName(file.path) }}</span>
          <span class="min-w-0 truncate text-11 text-dimmed">{{ folderOf(file) }}</span>
          <UBadge :label="String(file.rows.length)" size="sm" class="ms-auto shrink-0" />
        </button>
        <template v-if="file.open">
          <button
            v-for="(row, i) in file.rows"
            :key="i"
            class="flex h-5.5 w-full items-center ps-7 pe-2.5 text-start font-mono text-11 whitespace-nowrap text-toned hover:bg-accented"
            :title="`${file.rel}:${row.line + 1}`"
            @click="ide.open(row.path, { line: row.line, col: row.col })"
          >
            <span v-for="[before, match, after] in [parts(row)]" :key="0" class="truncate"
              >{{ before }}<b class="rounded-xs bg-(--accent-soft) font-medium text-highlighted">{{ match }}</b
              >{{ after }}</span
            >
          </button>
        </template>
      </template>
    </section>
  </div>
</template>
