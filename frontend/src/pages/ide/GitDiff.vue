<script setup lang="ts">
/**
 * A file's diff in an editor tab (the IdeDiff board's): its changes (the staged copy against the file, which is
 * edited in place), its staged changes (the last commit against the staged copy), a conflict's two sides, or what a
 * commit changed in it (O-R60); split or stacked. A block is staged or unstaged from the gutter's + or −, and a change discarded by Monaco's revert
 * arrow (O-R59); the header stages, unstages or discards the whole file.
 */
import { computed, onBeforeUnmount, shallowRef, useTemplateRef, watch } from "vue";
import { useQuery } from "@pinia/colada";
import { Stage, StageText, Unstage } from "@services/gitservice";
import { openFile } from "../../editor/monacoEditor";
import { useIdeRoots } from "../../composables/useIdeRoots";
import { compareStyle, diffView, languageOf, monaco } from "../../lib/monaco";
import CompareStyle from "../../ui/CompareStyle.vue";
import { capital } from "../../lib/format";
import { gitTextQuery, textQuery } from "../../queries";
import { useGitStore } from "../../stores/git";
import { tabOf, type Diff } from "../../editor/tabs";
import { useSettingsStore } from "../../stores/settings";
import { useWorkspaceStore } from "../../stores/workspace";
import OriginDot from "../../ui/OriginDot.vue";

const { workspaceId, tab } = defineProps<{ workspaceId: string; tab: string }>();

const git = useGitStore();
const settings = useSettingsStore();
const store = useWorkspaceStore();
const { locate } = useIdeRoots(() => workspaceId);
const file = computed(() => tabOf(tab).file);
const diff = computed<Diff>(() => tabOf(tab).diff ?? "changes");
const commit = computed(() => tabOf(tab).commit ?? "");
const root = computed(() => git.repoOf(file.value)?.root ?? "");

// the sides: changes are the staged copy against the file; staged ones the last commit against the staged copy; a
// conflict git's stage 2 (the current side) against its stage 3 (the incoming one); a commit its first parent's
// against its own
const revs = computed(() =>
  diff.value === "commit"
    ? [`${commit.value}^`, commit.value]
    : { changes: ["", ""], staged: ["HEAD", ""], conflict: [":2", ":3"] }[diff.value],
);
const { data: left } = useQuery(() => gitTextQuery({ root: root.value, rev: revs.value[0] ?? "", path: file.value }));
const { data: right } = useQuery(() => ({
  ...gitTextQuery({ root: root.value, rev: revs.value[1] ?? "", path: file.value }),
  enabled: !!root.value && diff.value !== "changes",
}));
const { data: working } = useQuery(() => ({ ...textQuery(file.value), enabled: diff.value === "changes" }));
// a file deleted from the working tree is staged whole
const deleted = computed(() => diff.value === "changes" && !!working.value?.missing);
// a binary or too large file has no text to compare: git or the reader says why
const unreadable = computed(() =>
  left.value?.binary || right.value?.binary
    ? "A binary file: it has no text to compare."
    : diff.value === "changes" && working.value && !working.value.missing && working.value.reject
      ? capital(working.value.reject)
      : "",
);
// only changes and staged changes are staged or unstaged by the block
const blockwise = computed(() => (diff.value === "changes" || diff.value === "staged") && !deleted.value);

const style = compareStyle;

const host = useTemplateRef("host");
const view = shallowRef<ReturnType<typeof diffView>>();
// the staged copy, the last commit's or a conflict side's text is a model of the view's own; the working file is its tab's
let own: monaco.editor.ITextModel[] = [];
const counts = shallowRef({ added: 0, removed: 0 });
const blocks = shallowRef<monaco.editor.ILineChange[]>([]);

watch(
  [host, left, right, working],
  ([el, before, after, now]) => {
    const text = diff.value === "changes" ? (deleted.value ? "" : now?.text) : after?.text;
    if (!el || !before || text === undefined || unreadable.value) return;
    if (!view.value) {
      view.value = diffView(el, settings.codeFont, style.value === "split", false);
      const live = diff.value === "changes" && !deleted.value;
      view.value.view.updateOptions({ readOnly: !live, renderMarginRevertIcon: live, glyphMargin: blockwise.value });
      const make = (value: string) => monaco.editor.createModel(value, languageOf(file.value, before.format));
      const original = make(before.text);
      const modified = live ? openFile(file.value, { text, bom: now?.bom ?? false, legacy: now?.legacy }) : make(text);
      own = live ? [original] : [original, modified];
      view.value.view.setModel({ original, modified });
      view.value.view.onDidUpdateDiff(mark);
      view.value.view.getModifiedEditor().onMouseDown(onGutter);
      return;
    }
    // read again (a block staged, the file saved): the sides keep their place
    const { original, modified } = view.value.view.getModel() ?? {};
    if (original && original.getValue() !== before.text) original.setValue(before.text);
    if (modified && own.includes(modified) && modified.getValue() !== text) modified.setValue(text);
  },
  { flush: "post" },
);
watch(style, (next) => view.value?.view.updateOptions({ renderSideBySide: next === "split" }));
onBeforeUnmount(() => {
  view.value?.dispose();
  own.forEach((model) => model.dispose());
});

// each block's + or − in the modified side's gutter, and the header's counts
let gutter: monaco.editor.IEditorDecorationsCollection | undefined;
function mark(): void {
  const changes = view.value?.view.getLineChanges() ?? [];
  const lines = (from: number, to: number) => (to ? to - from + 1 : 0);
  counts.value = {
    added: changes.reduce((sum, c) => sum + lines(c.modifiedStartLineNumber, c.modifiedEndLineNumber), 0),
    removed: changes.reduce((sum, c) => sum + lines(c.originalStartLineNumber, c.originalEndLineNumber), 0),
  };
  blocks.value = changes;
  const editor = view.value?.view.getModifiedEditor();
  if (!editor || !blockwise.value) return;
  gutter ??= editor.createDecorationsCollection();
  gutter.set(
    changes.map((c) => ({
      range: new monaco.Range(blockLine(c), 1, blockLine(c), 1),
      options: {
        glyphMarginClassName: diff.value === "changes" ? "codicon codicon-add" : "codicon codicon-remove",
        glyphMarginHoverMessage: { value: diff.value === "changes" ? "Stage this block" : "Unstage this block" },
      },
    })),
  );
}
// a block's line on the modified side: its first, or for lines removed the one they were under
const blockLine = (c: monaco.editor.ILineChange) => Math.max(c.modifiedStartLineNumber, 1);

/** Lines with a stretch replaced: from..to (1-based), or after `from` when `to` is 0 (an insertion). */
function replaced(lines: string[], from: number, to: number, put: string[]): string[] {
  const out = [...lines];
  out.splice(to ? from - 1 : from, to ? to - from + 1 : 0, ...put);
  return out;
}
const span = (lines: string[], from: number, to: number) => (to ? lines.slice(from - 1, to) : []);

// a + stages its block (the staged copy takes the file's lines), a − unstages it (the staged copy takes the last
// commit's back); git gets the staged copy whole
function onGutter(e: monaco.editor.IEditorMouseEvent): void {
  if (e.target.type !== monaco.editor.MouseTargetType.GUTTER_GLYPH_MARGIN || !e.target.position) return;
  const line = e.target.position.lineNumber;
  const c = blocks.value.find((one) => blockLine(one) === line);
  const model = view.value?.view.getModel();
  if (!c || !model) return;
  const before = model.original.getLinesContent();
  const after = model.modified.getLinesContent();
  const staging = diff.value === "changes";
  const lines = staging
    ? replaced(
        before,
        c.originalStartLineNumber,
        c.originalEndLineNumber,
        span(after, c.modifiedStartLineNumber, c.modifiedEndLineNumber),
      )
    : replaced(
        after,
        c.modifiedStartLineNumber,
        c.modifiedEndLineNumber,
        span(before, c.originalStartLineNumber, c.originalEndLineNumber),
      );
  const eol = (staging ? model.original : model.modified).getEOL();
  const side = staging ? left.value : right.value;
  git.run((at) => StageText(at, file.value, lines.join(eol), side?.bom ?? false, side?.legacy ?? false));
}

/** Discard the file's changes in its editor: its text becomes the staged copy's, unsaved and undoable. */
function discard(): void {
  const modified = view.value?.view.getModifiedEditor().getModel();
  if (!modified || !left.value) return;
  modified.pushStackElement();
  modified.pushEditOperations([], [{ range: modified.getFullModelRange(), text: left.value.text }], () => null);
  modified.pushStackElement();
}

const located = computed(() => locate(file.value));
const staged = computed(() => (git.status?.staged ?? []).some((one) => one.path === file.value));
const SIDES = computed<Record<Diff, [string, string]>>(() => ({
  changes: [staged.value ? "Staged" : `Last commit, ${git.status?.commit ?? ""}`, "Working tree"],
  staged: [`Last commit, ${git.status?.commit ?? ""}`, "Staged"],
  conflict: ["Current (HEAD)", "Incoming"],
  commit: [`Before ${commit.value.slice(0, 7)}`, `Commit ${commit.value.slice(0, 7)}`],
}));
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div
      class="flex h-7.5 shrink-0 items-center gap-2 border-b border-default bg-elevated ps-3.5 pe-2.5 text-12 whitespace-nowrap text-dimmed"
    >
      <OriginDot :color="store.originColor(located?.root.origin ?? '')" small />
      <span class="truncate text-toned">{{ located?.rel ?? file }}</span>
      <span class="grow" />
      <UBadge :label="`+${counts.added}`" color="success" size="sm" />
      <UBadge :label="`-${counts.removed}`" color="error" size="sm" />
      <CompareStyle size="xs" />
      <template v-if="diff === 'changes'">
        <UButton
          label="Stage file"
          icon="i-lucide-plus"
          variant="ghost"
          size="xs"
          @click="git.run((at) => Stage(at, [file]))"
        />
        <UButton v-if="!deleted" label="Discard" color="error" variant="ghost" size="xs" @click="discard" />
      </template>
      <UButton
        v-else-if="diff === 'staged'"
        label="Unstage file"
        icon="i-lucide-minus"
        variant="ghost"
        size="xs"
        @click="git.run((at) => Unstage(at, [file]))"
      />
    </div>
    <div class="flex h-6.5 shrink-0 border-b border-default bg-muted text-11 whitespace-nowrap text-dimmed">
      <span
        v-for="(side, i) in style === 'split' ? SIDES[diff] : [SIDES[diff].join(' → ')]"
        :key="i"
        class="flex flex-1 items-center gap-1.5 px-3"
        :class="{ 'border-e border-default': i === 0 && style === 'split' }"
      >
        <UIcon :name="i === 0 ? 'i-lucide-git-commit-horizontal' : 'i-lucide-pencil'" class="size-3.5" />{{ side }}
      </span>
    </div>
    <p v-if="unreadable" class="flex flex-1 items-center justify-center bg-(--bg-0) text-12 text-dimmed">
      {{ unreadable }}
    </p>
    <div v-show="!unreadable" ref="host" class="min-h-0 flex-1 border-s-12 border-(--bg-0) bg-(--bg-0)" />
  </div>
</template>
