<!--
  The detail of one node of the Graph: its head under the game's art, then
  what it is, what it says and what it links to, and its script.
-->
<script setup lang="ts">
import { computed, shallowRef, watch } from "vue";
import { useQuery } from "@pinia/colada";
import { createReusableTemplate, useClipboard } from "@vueuse/core";
import { Type } from "@internal/lang/models";
import { Markup } from "@internal/loc/models";
import type { Edge, Fact, LineRow, Node, Version } from "@services/graph/models";
import type { Site } from "@services/lsp/models";
import { compare } from "../../app/commands";
import { describeCount, list, plural, toRoman, toSentence } from "../../lib/format";
import { detailQuery, graphDefaultsQuery, graphLineQuery } from "../../queries";
import { useEditorStore } from "../../stores/editor";
import { useWorkspaceStore } from "../../stores/workspace";
import OriginChip from "../../ui/OriginChip.vue";
import ScriptSnippet from "../../ui/ScriptSnippet.vue";
import SourceLink from "../../ui/SourceLink.vue";
import ConditionList from "./ConditionList.vue";
import { conditionOf, hasView, labelOf, SAYS, TONES, whatOf } from "./nodes";
import NodeIcon from "./NodeIcon.vue";
import PaintingBand from "./PaintingBand.vue";

const { id } = defineProps<{
  /** The id of the node shown. */
  id: string;
}>();
const emit = defineEmits<{ ask: [id: string]; close: [] }>();

/** End is one end of a link: the one it starts at, or the one it ends at. */
type End = "from" | "to";
/** Group is the links of one role, with the end the other node is at. */
type Group = { label: string; end: End; edges: Edge[]; isAbove: boolean };
/**
 * Drawn is one row of the pane. Its view is the id the Graph opens on a
 * click, where it has a view of it, and its site the place the IDE opens.
 */
type Drawn = { node?: Node; label: string; note: string; amount: string; tone: string; view?: string; site?: Site };

const OTHER_END = { from: "to", to: "from" } as const;
/**
 * ROLES says, for each relation of a link, what the node at each end is to
 * the other. A relation with no row is shown by its own word.
 */
const ROLES: Record<string, Record<End, string>> = {
  group: { from: "Part of", to: "Holds" },
  era: { from: "Era", to: "In it" },
  level: { from: "Replaced by", to: "Replaces" },
  requires: { from: "Needs", to: "Leads to" },
  unlock: { from: "Unlocks", to: "Unlocked by" },
  input: { from: "Takes", to: "Taken by" },
  output: { from: "Makes", to: "Made by" },
  boost: { from: "Boosts", to: "Boosted by" },
  need: { from: "Needs", to: "Needed by" },
  gives: { from: "Gives when its need is met", to: "Given by" },
  employment: { from: "Employs", to: "Employed by" },
  resource: { from: "Found in", to: "Found here" },
  trigger: { from: "Checks", to: "Checked by" },
};
/**
 * ABOVE are the roles whose links the Overview lists too, each a relation
 * and the end the node is at.
 */
const ABOVE = ["requires:from", "requires:to", "unlock:from", "unlock:to", "trigger:to", "group:to"];
/** CAP is how many links of a role the Overview lists before the rest. */
const CAP = 4;
const OVERVIEW = "overview";
const LINKS = "links";
const SECTION = "flex flex-col border-b border-default py-2.5 last:border-b-0";
const HEADING = "flex min-h-5.5 items-baseline justify-between gap-3 text-12 font-semibold text-muted";
const QUIET = "text-11.5 font-normal text-dimmed tabular-nums";
const MORE = "ms-9.5 self-start p-0 text-dimmed";

const editor = useEditorStore();
const workspace = useWorkspaceStore();
const { copy, copied } = useClipboard();
const [DefineRow, Row] = createReusableTemplate<Drawn>();
const [DefineLinks, Links] = createReusableTemplate<{ group: Group; cap?: number }>();
const [DefineSays, Says] = createReusableTemplate<{ texts: Version[]; strong?: boolean }>();

const tab = shallowRef<string | number>(OVERVIEW);
const isEveryText = shallowRef(false);

const { data: defaults } = useQuery(() => ({ ...graphDefaultsQuery, enabled: workspace.isOpen() }));
const { data: detail, error } = useQuery(() => ({ ...detailQuery(id), enabled: workspace.isOpen() && !!id }));
const node = computed(() => detail.value?.node);
const isBuilding = computed(() => node.value?.type === Type.TypeBuilding);
/** An event shows a window or is hidden; a message or decision does neither. */
const isEvent = computed(() => ["window", "hidden"].includes(node.value?.display ?? ""));
const { data: line } = useQuery(() => ({
  ...graphLineQuery({ id, picked: [] }),
  enabled: workspace.isOpen() && isBuilding.value,
}));
const firing = computed(() => defaults.value?.databases ?? []);
const lines = computed(() => detail.value?.source.split("\n").length ?? 0);
const listed = computed(() => detail.value?.linked?.[node.value?.group ?? ""]);
/** The head's second line: "Building in Forestry District". */
const subtitle = computed(() => {
  const { group = "", era = "" } = node.value ?? {};
  const age = era === group ? undefined : detail.value?.linked?.[era];
  const words = [
    node.value && toSentence(whatOf(node.value)),
    listed.value && `in ${labelOf(listed.value)}`,
    age && `· ${labelOf(age)}`,
  ];

  return words.filter(Boolean).join(" ");
});
/**
 * The painting behind the head: the node's own, else its group's, else the
 * scene of an event.
 */
const painting = computed(
  () => node.value?.painting || listed.value?.painting || (node.value?.type ? "" : node.value?.picture),
);
/**
 * The rows of the building's line. A level past the first keeps only what it
 * says itself, each row with that level's one cell.
 */
const lineRows = computed(() => {
  if (!isBuilding.value) return [];

  const rows = [...(line.value?.rows ?? []), ...(line.value?.same ?? [])];
  const position = (line.value?.levels ?? []).findIndex((each) => each.level.id === id);
  if (position <= 0) return rows;

  return rows.flatMap((row) => {
    const cell = row.cells?.length === 1 ? row.cells[0] : row.cells?.[position];

    return cell?.text ? [{ ...row, cells: [cell] }] : [];
  });
});
const parts = computed(() =>
  Object.entries(SAYS)
    .map(([says, { title }]) => ({ title, rows: lineRows.value.filter((row) => row.says === says) }))
    .filter((part) => part.rows.length),
);
const says = computed(() =>
  (detail.value?.sections ?? []).filter(
    (section) => section.texts?.length && section.texts[0].text !== node.value?.title,
  ),
);
const groups = computed((): Group[] => {
  const firings: Group[] = [
    { label: "When it triggers", end: "from", edges: detail.value?.firedBy ?? [], isAbove: true },
    { label: "Fires", end: "to", edges: node.value?.fires ?? [], isAbove: false },
  ];
  const byRole = Map.groupBy(
    detail.value?.links ?? [],
    (edge) => `${edge.relation}:${edge.from === id ? "from" : "to"}`,
  );
  const related = [...byRole].map(([role, edges]): Group => {
    const [relation, own] = role.split(":") as [string, End];

    return {
      label: ROLES[relation]?.[own] ?? toSentence(relation),
      end: OTHER_END[own],
      edges,
      isAbove: ABOVE.includes(role),
    };
  });

  return [...firings, ...related].filter((group) => group.edges.length);
});
const linkCount = computed(() => groups.value.reduce((sum, group) => sum + group.edges.length, 0));
const tabs = computed(() => [
  { label: "Overview", value: OVERVIEW },
  { label: "Links", value: LINKS, badge: linkCount.value ? describeCount(linkCount.value) : undefined },
  { label: "Script", value: "script", badge: plural(lines.value, "line") },
]);

watch(
  () => id,
  () => {
    tab.value = OVERVIEW;
    isEveryText.value = false;
  },
);

/**
 * describeHow says how a firing is made: the weight, list or scope it is
 * written in, its effect and its delay.
 */
function describeHow(edge: Edge): string {
  const blocks = (edge.steps ?? [])
    .filter((step) => !["block", "branch"].includes(step.block))
    .map((step) => (step.weight ? `weight ${step.weight}` : step.key));

  return [...blocks, [edge.effect, edge.delay].filter(Boolean).join(", ")].filter(Boolean).join(" · ");
}

/**
 * drawEdge returns how a link is drawn, by the node at one of its ends. The
 * IDE opens where that node writes the link, else the node itself.
 */
function drawEdge(edge: Edge, end: End): Drawn {
  const far = detail.value?.linked?.[edge[end]];
  const how = [far && toSentence(whatOf(far)), edge.via?.length && `through ${list(edge.via)}`, describeHow(edge)];

  return {
    node: far,
    label: far ? labelOf(far) : edge[end],
    note: how.filter(Boolean).join(" · "),
    amount: (edge.relation && edge.amount) || "",
    tone: "text-(--blue)",
    view: far && hasView(far, firing.value) ? far.id : undefined,
    site: end === "from" || !far ? edge.site : far.site,
  };
}

/**
 * drawRow returns how a row of the building's line is drawn. Where its
 * levels say the same, that is its amount; where they differ, its note says
 * how it runs, as "60 at I, up to 465 at X".
 */
function drawRow(row: LineRow): Drawn {
  const texts = (row.cells ?? []).flatMap((cell, position) =>
    cell.text ? [{ text: cell.text, level: toRoman(position + 1) }] : [],
  );
  const [first, last] = [texts[0], texts[texts.length - 1]];
  const isSame = first?.text === last?.text;
  let span = "";
  if (first && !isSame) span = `${first.text} at ${first.level}, up to ${last.text} at ${last.level}`;
  else if (first && texts.length < (row.cells?.length ?? 0)) {
    span = texts.length > 1 ? `levels ${first.level} to ${last.level}` : `level ${first.level}`;
  }

  return {
    node: row.subject ?? undefined,
    label: row.label,
    note: [span, findMethod(row), conditionOf(lineRows.value, row)].filter(Boolean).join(" · "),
    amount: isSame ? (first?.text ?? "") : "",
    tone: SAYS[row.says]?.tone || TONES[row.tone ?? ""],
    view: row.subject && hasView(row.subject, firing.value) ? row.subject.id : undefined,
  };
}

/** LEDGERS is where a method lists the good of a row, by what the row says. */
const LEDGERS = { output: "makes", input: "takes" } as const;

/**
 * findMethod names the one production method of several that makes or takes
 * a row's good. It is "" where more than one does, or the building has none.
 */
function findMethod(row: LineRow): string {
  const ledger = LEDGERS[row.says as keyof typeof LEDGERS];
  const methods = (line.value?.groups ?? []).flatMap((group) => group.methods ?? []);
  const users = methods.filter((method) => method[ledger]?.some((amount) => amount.good.id === row.subject?.id));

  return ledger && methods.length > 1 && users.length === 1 ? labelOf(users[0].method) : "";
}

function drawFact(fact: Fact): Drawn {
  const note = [fact.where, fact.when].filter(Boolean).join(" · ");

  return { label: fact.label, note, amount: fact.text, tone: TONES[fact.tone ?? ""] };
}

function open(site: Site): void {
  editor.show(site.place, { lineNumber: site.line, column: 1 });
}

/**
 * openText opens a text's line in its localization file, or the line of the
 * definition that names the key where no language has the text.
 */
function openText(version: Version): void {
  if (version.message.place.path) open(version.message);
  else if (version.missing && node.value) open({ ...node.value.site, line: version.line });
}
</script>

<template>
  <aside class="flex h-full min-h-0 w-full min-w-0 flex-col overflow-hidden bg-muted">
    <UAlert v-if="error" color="error" variant="subtle" :description="error.message" class="m-3" />
    <p v-else-if="!id" class="grid flex-1 place-items-center px-6 text-center text-13 text-dimmed">
      Pick something on the left to see what it is, what it says and what it links to.
    </p>
    <template v-else-if="detail && node">
      <DefineRow v-slot="{ node: shown, label, note, amount, tone, view, site }">
        <div
          class="group -mx-1.5 grid min-h-9 items-center gap-x-2.5 rounded-md px-1.5 py-0.5"
          :class="[
            shown ? 'grid-cols-[1.75rem_minmax(0,1fr)_auto_auto]' : 'grid-cols-[minmax(0,1fr)_auto_auto]',
            view && 'cursor-pointer hover:bg-accented focus-visible:outline-2 focus-visible:outline-primary',
          ]"
          :role="view ? 'button' : undefined"
          :tabindex="view ? 0 : undefined"
          :title="view ? 'Open it in the Graph' : undefined"
          @click="view && emit('ask', view)"
          @keydown.enter="view && emit('ask', view)"
        >
          <NodeIcon v-if="shown" :node="shown" class="size-7" />
          <span class="flex min-w-0 flex-col">
            <b class="truncate text-13 leading-4.5 font-medium text-highlighted">{{ label }}</b>
            <small v-if="note" class="truncate text-11.5 leading-4 text-dimmed" :title="note">{{ note }}</small>
          </span>
          <b class="text-end text-12.5 font-medium whitespace-nowrap tabular-nums" :class="tone">{{ amount }}</b>
          <span class="flex items-center gap-0.5">
            <UTooltip v-if="site" :text="`Open ${site.place.name}:${site.line} in the IDE`">
              <UButton
                icon="i-lucide-external-link"
                color="neutral"
                variant="ghost"
                size="xs"
                square
                :aria-label="`Open ${label} in the IDE`"
                :class="view && 'opacity-0 group-hover:opacity-100 focus-visible:opacity-100'"
                @click.stop="open(site)"
              />
            </UTooltip>
            <UIcon v-if="view" name="i-lucide-chevron-right" class="size-4 text-dimmed" />
          </span>
        </div>
      </DefineRow>
      <DefineLinks v-slot="{ group, cap }">
        <section :class="SECTION">
          <h3 :class="HEADING">
            {{ group.label }}
            <small :class="QUIET">{{ describeCount(group.edges.length) }}</small>
          </h3>
          <Row
            v-for="(edge, position) in cap ? group.edges.slice(0, cap) : group.edges"
            :key="position"
            v-bind="drawEdge(edge, group.end)"
          />
          <UButton
            v-if="cap && group.edges.length > cap"
            :label="`Show all ${describeCount(group.edges.length)}`"
            trailing-icon="i-lucide-chevron-right"
            variant="link"
            color="neutral"
            size="xs"
            :class="MORE"
            @click="tab = LINKS"
          />
        </section>
      </DefineLinks>
      <DefineSays v-slot="{ texts, strong }">
        <template v-for="version in isEveryText ? texts : texts.slice(0, 1)" :key="`${version.key}|${version.line}`">
          <p
            class="text-13 leading-5 whitespace-pre-line text-highlighted"
            :class="[strong && 'font-medium', version.message.place.path && 'cursor-pointer']"
            :title="version.key"
            @click="openText(version)"
          >
            <template v-for="(segment, position) in version.segments ?? []" :key="position">
              <br v-if="segment.markup === Markup.MarkupBreak" />
              <code v-else-if="segment.markup === Markup.MarkupKey" class="font-mono text-12 text-(--syn-macro)">{{
                segment.text
              }}</code>
              <template v-else-if="segment.markup === Markup.MarkupText">{{ segment.text }}</template>
              <span v-else-if="segment.markup === Markup.MarkupCall" class="text-dimmed">{{ segment.text }}</span>
            </template>
          </p>
          <UButton
            v-if="version.missing"
            :label="`Missing in ${detail.language}`"
            variant="link"
            color="warning"
            size="xs"
            class="self-start p-0"
            @click="openText(version)"
          />
          <ConditionList
            v-if="version.when?.length && (isEveryText || texts.length === 1)"
            :conditions="[{ text: 'when', conditions: version.when, line: version.line }]"
            class="pb-1 text-11.5"
          />
        </template>
        <UButton
          v-if="texts.length > 1"
          :label="isEveryText ? 'Only the first' : `${describeCount(texts.length - 1)} more, by condition`"
          :trailing-icon="isEveryText ? undefined : 'i-lucide-chevron-right'"
          variant="link"
          color="neutral"
          size="xs"
          class="self-start p-0 text-dimmed"
          @click="isEveryText = !isEveryText"
        />
      </DefineSays>

      <div class="relative shrink-0">
        <PaintingBand :painting="painting" class="h-23 bg-linear-to-br from-(--blue)/15 to-default to-70%" />
        <span
          class="absolute start-4 -bottom-9.5 z-10 grid size-19 place-items-center overflow-hidden rounded-xl border border-accented bg-default shadow-lg"
        >
          <NodeIcon :node="node" :class="node.type ? 'size-14.5' : 'size-full'" />
        </span>
        <UButton
          icon="i-lucide-x"
          color="neutral"
          variant="soft"
          size="xs"
          square
          aria-label="Close the detail"
          class="absolute end-2.5 top-2.5"
          @click="emit('close')"
        />
      </div>
      <div class="min-h-11.5 shrink-0 ps-27 pe-4 pt-1.5">
        <h2 class="text-20 leading-6.5 font-semibold wrap-anywhere text-highlighted">{{ labelOf(node) }}</h2>
        <p class="text-12.5 leading-4.5 text-dimmed">{{ subtitle }}</p>
      </div>
      <div class="flex shrink-0 flex-col px-4 pt-2.5">
        <span class="flex min-h-5.5 items-center gap-1 font-mono text-12 break-all text-muted">
          {{ node.name }}
          <UButton
            :icon="copied ? 'i-lucide-check' : 'i-lucide-copy'"
            color="neutral"
            variant="ghost"
            size="xs"
            square
            aria-label="Copy the name"
            @click="copy(node.name)"
          />
        </span>
        <span class="flex min-h-5.5 items-center gap-2">
          <SourceLink :place="node.site.place" :line="node.site.line" short plain />
          <OriginChip :origin="node.site.place.origin" />
        </span>
        <div class="flex items-end gap-2 border-b border-default">
          <UTabs v-model="tab" :items="tabs" :content="false" variant="link" size="sm" :ui="{ list: 'border-0' }" />
          <span class="grow" />
          <UButton
            label="Open in IDE"
            icon="i-lucide-external-link"
            color="neutral"
            variant="outline"
            size="xs"
            class="mb-1.5"
            @click="open(node.site)"
          />
        </div>
      </div>

      <div class="min-h-0 flex-1 overflow-auto px-4">
        <template v-if="tab === OVERVIEW">
          <section v-if="detail.about?.length" :class="SECTION">
            <h3 :class="HEADING">About</h3>
            <dl class="grid grid-cols-[6rem_minmax(0,1fr)] items-baseline gap-x-3 text-12.5 leading-5.5">
              <template v-for="fact in detail.about" :key="`${fact.label}|${fact.text}`">
                <dt class="text-12 text-dimmed">{{ toSentence(fact.label) }}</dt>
                <dd class="min-w-0 text-highlighted">
                  {{ fact.text }}
                  <small v-if="fact.when" class="ms-1.5 text-11.5 text-dimmed">{{ fact.when }}</small>
                </dd>
              </template>
            </dl>
          </section>
          <section v-for="part in parts" :key="part.title" :class="SECTION">
            <h3 :class="HEADING">{{ part.title }}</h3>
            <template v-for="(row, position) in part.rows" :key="position">
              <Row v-bind="drawRow(row)" />
              <div
                v-if="row.says === 'output' && (row.cells?.length ?? 0) > 1"
                class="mb-1 flex h-6.5 items-end gap-0.75 ps-9.5"
                aria-hidden="true"
              >
                <i
                  v-for="(cell, level) in row.cells"
                  :key="level"
                  class="flex-1 rounded-t-xs bg-(--blue) opacity-45 last:opacity-100"
                  :style="{ height: `${cell.part * 100}%` }"
                />
              </div>
            </template>
          </section>
          <template v-for="group in groups" :key="`${group.label}|${group.end}`">
            <Links v-if="group.isAbove" :group="group" :cap="CAP" />
          </template>
          <section v-if="!node.type" :class="SECTION">
            <h3 :class="HEADING">Only when</h3>
            <ConditionList v-if="node.trigger?.length" :conditions="node.trigger" class="text-12.5 leading-5" />
            <p v-else class="text-12.5 text-dimmed">It has no trigger of its own.</p>
            <p v-if="isEvent && !detail.firedBy?.length" class="pt-1 text-12 leading-4.5 text-dimmed">
              Nothing fires this yet. An event runs when an on_action, a decision, a character interaction or another
              event fires it.
            </p>
          </section>
          <section v-if="!isBuilding && detail.gives?.length" :class="SECTION">
            <h3 :class="HEADING">Gives</h3>
            <Row v-for="(fact, position) in detail.gives" :key="position" v-bind="drawFact(fact)" />
          </section>
          <section v-if="says.length" :class="SECTION" class="gap-1.5">
            <h3 :class="HEADING">What it says</h3>
            <Says v-for="section in says" :key="section.line" :texts="section.texts ?? []" />
          </section>
          <section v-if="detail.options?.length" :class="SECTION">
            <h3 :class="HEADING">
              Options
              <small :class="QUIET">{{ describeCount(detail.options.length) }}</small>
            </h3>
            <div v-for="option in detail.options" :key="option.line" class="flex flex-col gap-0.5 py-1">
              <Says :texts="option.texts ?? []" strong />
              <ConditionList v-if="option.when?.length" :conditions="option.when" class="text-11.5" />
            </div>
          </section>
          <section v-for="rule in detail.rules" :key="rule.id" :class="SECTION" class="gap-1.5">
            <h3 :class="HEADING">
              The rule it reads
              <code class="font-mono text-11 font-normal text-dimmed">{{ rule.id }}</code>
            </h3>
            <div v-for="option in rule.options" :key="option.key" class="flex items-baseline gap-2.5 text-11.5">
              <UBadge
                :label="option.text || option.key"
                :color="option.default ? 'success' : 'neutral'"
                variant="subtle"
                size="sm"
                class="min-w-18 shrink-0 justify-center"
              />
              <span class="min-w-0 flex-1 text-toned">{{ option.description }}</span>
              <span v-if="option.default" class="text-dimmed">default</span>
            </div>
          </section>
          <section v-if="detail.copy" :class="SECTION">
            <h3 :class="HEADING">It replaces the game's</h3>
            <div class="flex items-center gap-2">
              <SourceLink :place="detail.copy.site.place" :line="detail.copy.site.line" />
              <span class="grow" />
              <UButton
                label="Compare"
                icon="i-lucide-git-compare"
                variant="ghost"
                size="xs"
                @click="compare(detail.copy.site.place, node.site.place)"
              />
            </div>
          </section>
        </template>
        <template v-else-if="tab === LINKS">
          <Links v-for="group in groups" :key="`${group.label}|${group.end}`" :group="group" />
          <p v-if="!linkCount" class="py-3 text-12.5 text-dimmed">It links to nothing, and nothing links to it.</p>
        </template>
        <div v-else class="my-3 overflow-x-auto rounded-lg border border-default">
          <div class="flex h-8 items-center gap-2 border-b border-default bg-elevated px-2.5 text-12 whitespace-nowrap">
            <SourceLink :place="node.site.place" :line="node.site.line" />
            <span class="grow" />
            <UButton
              :icon="copied ? 'i-lucide-check' : 'i-lucide-copy'"
              variant="ghost"
              size="xs"
              square
              aria-label="Copy the definition"
              @click="copy(detail.source)"
            />
          </div>
          <ScriptSnippet
            :text="detail.source"
            :line="node.site.line"
            :path="node.site.place.path"
            class="px-3 pb-1.5"
          />
        </div>
      </div>
    </template>
  </aside>
</template>
