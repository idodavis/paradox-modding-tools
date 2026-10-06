<script setup lang="ts">
/**
 * Find in files (the Ide board's sidebar Search): a text, or a regular expression, matching case and whole words
 * when asked, in the folders picked and the files the include and exclude globs keep. Its hits stream into the
 * panel's Search results as Go finds them (search:hits). Replace all writes the mods' files (Go), and changes the
 * files open in the editor there, unsaved; the game's files are never changed.
 */
import { computed, onScopeDispose, shallowRef } from "vue";
import { storeToRefs } from "pinia";
import { Events } from "@wailsio/runtime";
import type { IdeRoot } from "@services/models";
import { Replace, TextSearch } from "@services/searchservice";
import { pathKey } from "../../composables/useIdeRoots";
import { toBytes } from "../../ide/editorCore";
import { editText } from "../../ide/monacoEditor";
import { plural } from "../../lib/format";
import { useIdeStore } from "../../stores/ide";
import { useWorkspaceStore } from "../../stores/workspace";

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
const globs = (text: string) => text.split(",").map((glob) => glob.trim()).filter(Boolean);
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
      includes: globs(include.value),
      excludes: [...globs(exclude.value), ...(hideBinaries ? BINARY_GLOBS : [])],
    })),
  };
}

// the search shown, its folders' origins (a hit names its folder by index), and its call
let searching = "";
let origins: string[] = [];
let running: ReturnType<typeof TextSearch> | undefined;
async function run(): Promise<void> {
  if (!pattern.value.trim()) return;
  running?.cancel();
  const id = crypto.randomUUID();
  const searched = folders.value;
  searching = id;
  results.value = { title: `“${pattern.value}”`, rows: [], done: false, capped: false };
  ide.panel = "search";
  running = TextSearch(query(id, searched));
  origins = searched.map((root) => root.origin ?? "");
  const end = await running.catch((error: unknown) => {
    if (searching === id) toast.add({ title: "The search could not run", description: String(error), color: "error" });
    return null;
  });
  if (searching === id && results.value) results.value = { ...results.value, done: true, capped: !!end?.limitHit };
}

// each file's hits as they come; a hit's columns are characters (code points), the IDE's places bytes
const stop = Events.On("search:hits", ({ data }) => {
  const shown = results.value;
  if (data.id !== searching || !shown) return;
  const bytes = (text: string, chars: number) => toBytes(text, [...text].slice(0, chars).join("").length);
  for (const hit of data.hits ?? []) {
    shown.rows.push({
      path: hit.path,
      rel: hit.rel,
      origin: origins[hit.folder] ?? "",
      line: hit.line,
      col: bytes(hit.preview, hit.col),
      end: bytes(hit.preview, hit.endCol),
      preview: hit.preview,
    });
  }
  results.value = { ...shown };
});
onScopeDispose(() => {
  stop();
  running?.cancel();
});

// Replace all: in the mods' folders picked; Go writes the files, the editor changes the ones it has open
const writable = computed(() => folders.value.filter((root) => !root.readOnly));
const confirming = shallowRef(false);
async function replaceAll(): Promise<void> {
  confirming.value = false;
  const roots = writable.value.map((root) => `${pathKey(root.path)}/`);
  const open = ide.openFiles.filter((path) => roots.some((root) => pathKey(path).startsWith(root)));
  // the search's matcher as JavaScript reads it, a line at a time as Go matches
  const source = regex.value ? pattern.value : pattern.value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const re = new RegExp(word.value ? `\\b(?:${source})\\b` : source, matchCase.value ? "g" : "gi");
  let inEditor = 0;
  for (const path of open) {
    editText(path, (text) =>
      text
        .split("\n")
        .map((line) => {
          const cr = line.endsWith("\r");
          const body = cr ? line.slice(0, -1) : line;
          const found = body.match(re)?.length ?? 0;
          inEditor += found;
          if (!found) return line;
          // a plain search's replacement is written as it is; a regular expression's names its groups ($1)
          const replaced = regex.value ? body.replace(re, replacement.value) : body.replace(re, () => replacement.value);
          return replaced + (cr ? "\r" : "");
        })
        .join("\n"),
    );
  }
  const done = await Replace({ search: query("", writable.value), text: replacement.value, skip: ide.openFiles }).catch((error: unknown) => {
    toast.add({ title: "Replace all could not run", description: String(error), color: "error" });
    return null;
  });
  if (!done) return;
  toast.add({
    title: plural(done.replaced + inEditor, "replacement"),
    description: `${plural(done.files, "file")} written${inEditor ? `; ${inEditor} in open files, not saved yet` : ""}.`,
    color: "success",
  });
  void run();
}
</script>

<template>
  <!-- Enter in any field submits the form, which runs the search -->
  <form class="flex min-h-0 flex-1 flex-col gap-2.5 overflow-y-auto p-2.5" @submit.prevent="run">
    <UInput v-model="pattern" placeholder="Search" size="sm" autofocus :ui="{ trailing: 'pe-1' }">
      <template #trailing>
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
        <UTooltip text="Replace all">
          <UButton
            icon="i-lucide-replace-all"
            size="xs"
            square
            variant="ghost"
            aria-label="Replace all"
            :disabled="!pattern.trim() || !writable.length"
            @click="confirming = true"
          />
        </UTooltip>
      </template>
    </UInput>
    <UModal v-model:open="confirming" title="Replace all?">
      <template #body>
        <p class="text-13 text-toned">
          Every match of “{{ pattern }}” in the mods' files becomes “{{ replacement }}”. The game's files are not changed; a file open
          in the editor changes there, unsaved.
        </p>
      </template>
      <template #footer>
        <div class="flex w-full justify-end gap-2">
          <UButton label="Cancel" @click="confirming = false" />
          <UButton label="Replace all" color="primary" variant="solid" @click="replaceAll" />
        </div>
      </template>
    </UModal>
    <UFormField label="Files to include">
      <UInput v-model="include" placeholder="common/**, *.txt" size="sm" class="w-full font-mono" />
    </UFormField>
    <UFormField label="Files to exclude">
      <UInput v-model="exclude" placeholder="gfx/**" size="sm" class="w-full font-mono" />
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
    <UButton type="submit" label="Search" icon="i-lucide-search" size="sm" class="self-start" :disabled="!pattern.trim() || !folders.length" />
  </form>
</template>
