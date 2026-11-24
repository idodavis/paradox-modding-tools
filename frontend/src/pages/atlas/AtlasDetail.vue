<!-- The detail pane: what the selected node is, what it makes, gives and says, its links both ways, and its script. -->
<script setup lang="ts">
import { computed, shallowRef, watch } from "vue";
import { useQuery } from "@pinia/colada";
import { createReusableTemplate } from "@vueuse/core";

import { Type } from "@internal/lang/models";
import { Markup } from "@internal/loc/models";
import {
  Tone,
  type Amount,
  type Edge,
  type Fact,
  type LineRow,
  type Method,
  type Node,
  type Version,
} from "@services/atlas/models";
import type { Site } from "@services/lsp/models";
import { compare } from "../../app/commands";
import { copyText } from "../../lib/feedback";
import { describeCount, toSentence } from "../../lib/format";
import { defaultsQuery, detailQuery } from "../../queries";
import { useEditorStore } from "../../stores/editor";
import { useWorkspaceStore } from "../../stores/workspace";
import OriginChip from "../../ui/OriginChip.vue";
import ScriptSnippet from "../../ui/ScriptSnippet.vue";
import SourceLink from "../../ui/SourceLink.vue";
import { GOODS, RELATIONS } from "./AtlasNode.vue";
import ConditionList from "./ConditionList.vue";
import NodePicture, { labelOf } from "./NodePicture.vue";
import PaintingBand from "./PaintingBand.vue";

const {
  id = "",
  group = undefined,
  isSelection = false,
} = defineProps<{
  /** The id of the node shown, "" for none. */
  id?: string;
  /** The group selected on the canvas, whose members the pane lists first. */
  group?: Node;
  /** Whether the pane shows the canvas's selection, in gold, and not its subject. */
  isSelection?: boolean;
}>();
const emit = defineEmits<{
  /** A node was asked for as the subject. */
  ask: [id: string];
  close: [];
}>();

/** A Row is one line of a list: art, a name, a quiet line, an amount, and where a click leads. */
type Row = { node?: Node; label: string; note?: string; amount?: string; tone?: string; view?: string; site?: Site };

const OVERVIEW = "overview";
const LINKS = "links";
const SCRIPT = "script";
/** The most rows of one group of links Overview lists before "Show all", which opens Links. */
const CAP = 4;
const SECTION = "flex flex-col gap-0.5 border-b border-default py-3 last:border-b-0";
const HEADING = "flex min-h-5.5 items-baseline justify-between gap-3 pb-1 text-11 font-semibold text-muted";
const QUIET = "text-11.5 font-normal text-dimmed tabular-nums";
const MORE = "self-start p-0 pt-1 text-dimmed";
const TONES: Record<string, string> = { [Tone.ToneGain]: "text-(--green)", [Tone.ToneLoss]: "text-(--red)" };
const FIRED = "When it triggers";
const FIRES = "Fires";
/** ROLES holds what a link of each relation is called: at the end it runs from, then at the end it runs to. */
const ROLES: Record<string, [own: string, other: string]> = {
  part: ["Part of", "Holds"],
  era: ["Era", "In it"],
  level: ["Replaced by", "Replaces"],
  requires: ["Needs", "Leads to"],
  unlock: ["Unlocks", "Unlocked by"],
  input: ["Takes", "Taken by"],
  output: ["Makes", "Made by"],
  boost: ["Boosts", "Boosted by"],
  gives: ["Gives when its need is met", "Given by"],
  employment: ["Employs", "Employed by"],
  resource: ["Found in", "Found here"],
  checks: ["Checks", "Checked by"],
  access: ["Needs access to", "Access needed by"],
};
/** The order the links' groups stand in. A group of what a script names stands after them, under its type's word. */
const ORDER = [FIRED, FIRES, ...Object.values(ROLES).flat()];
/** The types of definition Atlas has a canvas of, beside whatever fires. */
const DRAWN: (Type | undefined)[] = [...GOODS, Type.TypeBuilding, Type.TypeProductionMethod, Type.TypeTechnology];

const [DefineRow, ShowRow] = createReusableTemplate<Row>();
const [DefineSays, Says] = createReusableTemplate<{ texts: Version[]; strong?: boolean }>();

const editor = useEditorStore();
const workspace = useWorkspaceStore();
const { data: detail, error } = useQuery(() => ({ ...detailQuery(id), enabled: workspace.isOpen() && !!id }));
const { data: defaults } = useQuery(() => ({ ...defaultsQuery, enabled: workspace.isOpen() }));
const tab = shallowRef(OVERVIEW);
const isEveryText = shallowRef(false);
const isEveryMethod = shallowRef(false);
watch(
  () => id,
  () => {
    tab.value = OVERVIEW;
    isEveryText.value = false;
    isEveryMethod.value = false;
  },
);

const node = computed(() => (id ? detail.value?.node : undefined));
const linkCount = computed(
  () => (detail.value?.links?.length ?? 0) + (detail.value?.firedBy?.length ?? 0) + (node.value?.fires?.length ?? 0),
);
const tabs = computed(() => [
  { label: "Overview", value: OVERVIEW },
  { label: "Links", value: LINKS, badge: linkCount.value || undefined },
  { label: "Script", value: SCRIPT, badge: detail.value?.source ? detail.value.source.split("\n").length : undefined },
]);
const methods = computed(() => (node.value?.line?.groups ?? []).flatMap((methodGroup) => methodGroup.methods ?? []));
/** What the node gives, under what each fact applies to, in the order first found. */
const gives = computed(() => Map.groupBy(detail.value?.gives ?? [], (fact) => fact.where || "Gives"));
/** The node's links under what each is to it, in the order of ORDER. */
const groups = computed(() => {
  const own = node.value;
  if (!own || !detail.value) return [];
  const byLabel = new Map<string, Edge[]>();
  const add = (label: string, edge: Edge) => byLabel.set(label, [...(byLabel.get(label) ?? []), edge]);
  for (const edge of detail.value.firedBy ?? []) add(FIRED, edge);
  for (const edge of own.fires ?? []) add(FIRES, edge);
  for (const edge of detail.value.links ?? []) {
    const role = ROLES[edge.relation ?? ""];
    add(role ? role[edge.from === own.id ? 0 : 1] : toSentence(findOther(edge)?.what ?? ""), edge);
  }
  const place = (label: string) => (ORDER.includes(label) ? ORDER.indexOf(label) : ORDER.length);
  return [...byLabel].map(([label, edges]) => ({ label, edges })).sort((a, b) => place(a.label) - place(b.label));
});

/** findOther returns the node at a link's other end, as the detail lists it. */
function findOther(edge: Edge): Node | undefined {
  return detail.value?.linked?.[edge.from === id ? edge.to : edge.from];
}

/** hasView reports whether Atlas has a canvas of a node: a drawn type, or a definition that fires. */
function hasView(shown: Node): boolean {
  return DRAWN.includes(shown.type) || !!defaults.value?.databases?.includes(shown.database);
}

/** drawEdge returns a link's row: the node at its other end, what it is and how the link is made, and its amount. */
function drawEdge(edge: Edge): Row {
  const other = findOther(edge);
  const block = edge.steps?.at(-1)?.conditions?.[0];
  return {
    node: other,
    label: other ? labelOf(other) : edge.from === id ? edge.to : edge.from,
    note: [
      other?.what,
      edge.via?.length ? `through ${edge.via.join(", ")}` : "",
      [block?.text, block?.value].filter(Boolean).join(" "),
      edge.delay && `after ${edge.delay}`,
    ]
      .filter(Boolean)
      .join(" · "),
    amount: edge.amount,
    tone: RELATIONS[edge.relation as keyof typeof RELATIONS]?.color,
    view: other && hasView(other) ? other.id : undefined,
    site: edge.site.place.path ? edge.site : other?.site,
  };
}

/** drawFact returns a fact's row: its label, its condition as the quiet line, and its value in its tone. */
function drawFact(fact: Fact): Row {
  return { label: fact.label, note: fact.when, amount: fact.text, tone: TONES[fact.tone ?? ""] };
}

/** drawLevel returns the row of one thing a building's level says: the good or pop type, or the fact, and how much. */
function drawLevel(row: LineRow): Row {
  const amount = row.cells?.[0] ?? "";
  if (!row.subject) return { label: row.label, amount, tone: TONES[row.tone ?? ""] };
  return { node: row.subject, label: labelOf(row.subject), note: row.label, amount, view: viewOf(row.subject) };
}

/** drawMethod returns a production method's row: what unlocks it, and what it takes and makes. */
function drawMethod(method: Method): Row {
  const list = (amounts: Amount[] | null) =>
    (amounts ?? []).map((amount) => `${amount.text} ${labelOf(amount.good)}`).join(", ");
  const unlock = method.unlocks?.length ? method.unlocks.map(labelOf).join(", ") : "from the start";
  return {
    node: method.method,
    label: labelOf(method.method),
    note: [unlock, [list(method.takes), list(method.makes)].filter(Boolean).join(" → ")].filter(Boolean).join(" · "),
    view: method.method.id,
  };
}

/** drawMember returns the row of a group's member: its name, its quiet line and its first fact. */
function drawMember(member: Node): Row {
  const fact = member.facts?.[0];
  return { node: member, label: labelOf(member), note: member.summary, amount: fact?.text, view: member.id };
}

function viewOf(shown: Node): string | undefined {
  return hasView(shown) ? shown.id : undefined;
}

function open(site: Site): void {
  editor.show(site.place, { lineNumber: site.line, column: 1 });
}

/** openText opens a text's line in its localization file, or the line that names a key with no text. */
function openText(version: Version): void {
  if (version.message.place.path) open(version.message);
  else if (node.value) editor.show(node.value.site.place, { lineNumber: version.line, column: 1 });
}
</script>

<template>
  <aside class="flex h-full min-h-0 w-full min-w-0 flex-col overflow-hidden bg-muted">
    <DefineRow v-slot="{ node: shown, label, note, amount, tone, view, site }">
      <div
        class="group -mx-1.5 grid min-h-7.5 items-center gap-x-2.5 rounded-md px-1.5 py-0.5"
        :class="[
          shown ? 'grid-cols-[1.5rem_minmax(0,1fr)_auto_auto]' : 'grid-cols-[minmax(0,1fr)_auto_auto]',
          view && 'cursor-pointer hover:bg-accented focus-visible:outline-2 focus-visible:outline-primary',
        ]"
        :role="view ? 'button' : undefined"
        :tabindex="view ? 0 : undefined"
        :title="view ? 'Open it in Atlas' : undefined"
        @click="view && emit('ask', view)"
        @keydown.enter="view && emit('ask', view)"
      >
        <NodePicture v-if="shown" :node="shown" class="size-6" />
        <span class="flex min-w-0 flex-col">
          <span class="truncate text-13 leading-4.5 text-highlighted" :title="label">{{ label }}</span>
          <small v-if="note" class="line-clamp-2 text-11.5 leading-4 text-dimmed" :title="note">{{ note }}</small>
        </span>
        <span class="text-end text-12 font-medium whitespace-nowrap tabular-nums" :class="tone">{{ amount }}</span>
        <span class="flex items-center gap-0.5">
          <UTooltip v-if="site?.place.path" :text="`Open ${site.place.name}:${site.line} in the IDE`">
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
    <DefineSays v-slot="{ texts, strong }">
      <template v-for="version in isEveryText ? texts : texts.slice(0, 1)" :key="`${version.key}|${version.line}`">
        <p
          class="text-13 leading-5 whitespace-pre-line text-toned"
          :class="[strong && 'font-medium text-highlighted', version.message.place.path && 'cursor-pointer']"
          :title="version.key"
          @click="openText(version)"
        >
          <template v-for="(segment, position) in version.segments ?? []" :key="position">
            <br v-if="segment.markup === Markup.MarkupBreak" />
            <code v-else-if="segment.markup === Markup.MarkupKey" class="font-mono text-12 text-(--syn-macro)">{{
              segment.text
            }}</code>
            <template v-else-if="segment.markup === Markup.MarkupText">{{ segment.text }}</template>
            <span
              v-else-if="segment.markup === Markup.MarkupCall"
              class="rounded-sm bg-elevated px-1 text-11 text-dimmed"
              :title="segment.text"
              >…</span
            >
          </template>
        </p>
        <UButton
          v-if="version.missing"
          :label="`Missing in ${detail?.language}`"
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
        :class="MORE"
        @click="isEveryText = !isEveryText"
      />
    </DefineSays>

    <UAlert v-if="error" color="error" variant="subtle" :description="error.message" class="m-3" />
    <p v-else-if="!id && !group" class="grid flex-1 place-items-center px-6 text-center text-13 text-dimmed">
      Pick something on the left to see what it is, what it says and what it links to.
    </p>
    <template v-else>
      <template v-if="detail && node">
        <div class="relative shrink-0">
          <PaintingBand
            :painting="node.painting || (node.display && node.picture) || ''"
            class="h-23 bg-linear-to-br from-(--blue)/15 to-default to-70%"
          />
          <span
            class="absolute start-4 -bottom-9.5 z-10 grid size-19 place-items-center overflow-hidden rounded-xl border-2 bg-default shadow-lg"
            :class="isSelection ? 'border-primary' : 'border-secondary'"
          >
            <NodePicture :node="node" :class="node.display ? 'size-full' : 'size-14.5'" />
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
          <h2
            class="line-clamp-3 text-18 leading-6 font-semibold wrap-anywhere text-highlighted"
            :title="labelOf(node)"
          >
            {{ labelOf(node) }}
          </h2>
          <p class="flex flex-wrap items-center gap-x-2 text-12 leading-4.5 text-dimmed">
            <UBadge
              :label="isSelection ? 'Selected' : 'Subject'"
              :color="isSelection ? 'primary' : 'secondary'"
              variant="subtle"
              size="sm"
            />
            {{ node.summary }}
          </p>
        </div>
        <div class="flex shrink-0 flex-col px-4 pt-2.5">
          <span class="flex min-h-5.5 items-center gap-1 font-mono text-12 break-all text-muted">
            {{ node.name }}
            <UButton
              icon="i-lucide-copy"
              color="neutral"
              variant="ghost"
              size="xs"
              square
              aria-label="Copy the name"
              @click="copyText(node.name, 'The name')"
            />
          </span>
          <span class="flex min-h-5.5 items-center gap-2">
            <SourceLink :place="node.site.place" :line="node.site.line" short plain />
            <OriginChip :origin="node.origin" />
          </span>
          <div class="flex items-end gap-2 border-b border-default">
            <UTabs
              :model-value="tab"
              :items="tabs"
              :content="false"
              variant="link"
              size="sm"
              :ui="{ list: 'border-0' }"
              @update:model-value="tab = String($event)"
            />
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
      </template>
      <UButton
        v-else
        icon="i-lucide-x"
        color="neutral"
        variant="soft"
        size="xs"
        square
        aria-label="Close the detail"
        class="m-2.5 self-end"
        @click="emit('close')"
      />

      <div class="min-h-0 flex-1 overflow-auto px-4">
        <section v-if="group?.members?.length && tab === OVERVIEW" :class="SECTION">
          <h3 :class="HEADING">
            {{ labelOf(group) }}
            <small :class="QUIET">{{ describeCount(group.members.length) }}</small>
          </h3>
          <ShowRow v-for="member in group.members" :key="member.id" v-bind="drawMember(member)" />
        </section>
        <template v-if="detail && node && tab === OVERVIEW">
          <section v-if="detail.about?.length" :class="SECTION">
            <h3 :class="HEADING">About</h3>
            <dl class="grid grid-cols-[7.5rem_minmax(0,1fr)] items-baseline gap-x-3 text-12 leading-5.5">
              <template v-for="fact in detail.about" :key="`${fact.label}|${fact.text}`">
                <dt class="text-12 text-dimmed">{{ toSentence(fact.label) }}</dt>
                <dd class="min-w-0 wrap-anywhere text-highlighted">
                  {{ fact.text }}
                  <small v-if="fact.when" class="ms-1.5 text-11.5 text-dimmed">{{ fact.when }}</small>
                </dd>
              </template>
            </dl>
          </section>
          <section v-if="detail.sections?.length" :class="SECTION" class="gap-1.5">
            <h3 :class="HEADING">What it says</h3>
            <Says
              v-for="section in detail.sections"
              :key="`${section.key}|${section.line}`"
              :texts="section.texts ?? []"
            />
          </section>
          <section v-for="band in node.line?.bands" :key="band.title" :class="SECTION">
            <h3 :class="HEADING">
              {{ band.title }}
              <small :class="QUIET">{{ describeCount(band.rows?.length ?? 0) }}</small>
            </h3>
            <ShowRow v-for="row in band.rows" :key="`${row.subject?.id}|${row.label}`" v-bind="drawLevel(row)" />
          </section>
          <section v-if="methods.length" :class="SECTION">
            <h3 :class="HEADING">
              Production methods
              <small :class="QUIET">{{ describeCount(methods.length) }}</small>
            </h3>
            <ShowRow
              v-for="method in isEveryMethod ? methods : methods.slice(0, CAP)"
              :key="method.method.id"
              v-bind="drawMethod(method)"
            />
            <UButton
              v-if="methods.length > CAP"
              :label="isEveryMethod ? 'Only the first' : `Show all ${describeCount(methods.length)}`"
              variant="link"
              color="neutral"
              size="xs"
              :class="MORE"
              @click="isEveryMethod = !isEveryMethod"
            />
          </section>
          <section v-if="node.display" :class="SECTION">
            <h3 :class="HEADING">Only when</h3>
            <ConditionList v-if="node.trigger?.length" :conditions="node.trigger" class="text-12 leading-5" />
            <p v-else class="text-12 text-dimmed">It has no trigger of its own.</p>
            <p
              v-if="node.type === Type.TypeEvent && !detail.firedBy?.length"
              class="pt-1 text-12 leading-4.5 text-dimmed"
            >
              Nothing fires this yet. An event runs when an on_action, a decision, a character interaction or another
              event fires it.
            </p>
          </section>
          <section v-for="links in groups" :key="links.label" :class="SECTION">
            <h3 :class="HEADING">
              {{ links.label }}
              <small :class="QUIET">{{ describeCount(links.edges.length) }}</small>
            </h3>
            <ShowRow v-for="(edge, position) in links.edges.slice(0, CAP)" :key="position" v-bind="drawEdge(edge)" />
            <UButton
              v-if="links.edges.length > CAP"
              :label="`Show all ${describeCount(links.edges.length)}`"
              trailing-icon="i-lucide-chevron-right"
              variant="link"
              color="neutral"
              size="xs"
              :class="MORE"
              @click="tab = LINKS"
            />
          </section>
          <section v-for="[where, facts] in gives" :key="where" :class="SECTION">
            <h3 :class="HEADING">
              {{ where }}
              <small :class="QUIET">{{ describeCount(facts.length) }}</small>
            </h3>
            <ShowRow v-for="(fact, position) in facts" :key="position" v-bind="drawFact(fact)" />
          </section>
          <section v-if="detail.options?.length" :class="SECTION" class="gap-2">
            <h3 :class="HEADING">
              Options
              <small :class="QUIET">{{ describeCount(detail.options.length) }}</small>
            </h3>
            <div v-for="option in detail.options" :key="option.line" class="flex flex-col gap-0.5">
              <Says :texts="option.texts ?? []" strong />
              <details v-if="option.when?.length" class="text-11.5 text-dimmed">
                <summary class="cursor-pointer select-none">Only when</summary>
                <ConditionList :conditions="option.when" class="pt-1" />
              </details>
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
        <template v-else-if="detail && node && tab === LINKS">
          <section v-for="links in groups" :key="links.label" :class="SECTION">
            <h3 :class="HEADING">
              {{ links.label }}
              <small :class="QUIET">{{ describeCount(links.edges.length) }}</small>
            </h3>
            <ShowRow v-for="(edge, position) in links.edges" :key="position" v-bind="drawEdge(edge)" />
          </section>
          <p v-if="!linkCount" class="py-3 text-12 text-dimmed">It links to nothing, and nothing links to it.</p>
        </template>
        <div v-else-if="detail && node" class="my-3 overflow-x-auto rounded-lg border border-default">
          <div class="flex h-8 items-center gap-2 border-b border-default bg-elevated px-2.5 text-12 whitespace-nowrap">
            <SourceLink :place="node.site.place" :line="node.site.line" />
            <span class="grow" />
            <UButton
              icon="i-lucide-copy"
              variant="ghost"
              size="xs"
              square
              aria-label="Copy the definition"
              @click="copyText(detail.source, 'The definition')"
            />
          </div>
          <ScriptSnippet
            :text="detail.source"
            :line="detail.sourceLine"
            :path="node.site.place.path"
            class="px-3 pb-1.5"
          />
        </div>
      </div>
    </template>
  </aside>
</template>
