<!--
  The detail of a graph node: its head, then an overview of how it triggers, the rule it reads, the game's copy it
  replaces and its properties, its texts, its firings both ways, and its script as written.
  Below the canvas, the overview lays its parts out in columns.
-->
<script setup lang="ts">
import { computed, shallowRef, watch } from "vue";
import { useQuery } from "@pinia/colada";
import { createReusableTemplate, useClipboard } from "@vueuse/core";
import { useRoute, useRouter } from "vue-router";
import type { Condition, Edge, Graph, Node } from "@services/graph/models";
import type { Place } from "@services/workspace/models";
import { compare } from "../../app/commands";
import { detailQuery, loadQuery } from "../../queries";
import { useEditorStore } from "../../stores/editor";
import { useWorkspaceStore } from "../../stores/workspace";
import LocText from "../../ui/LocText.vue";
import OriginChip from "../../ui/OriginChip.vue";
import ScriptSnippet from "../../ui/ScriptSnippet.vue";
import SectionHead from "../../ui/SectionHead.vue";
import SourceLink from "../../ui/SourceLink.vue";
import ConditionList, { describeStep, groupConditions } from "./ConditionList.vue";
import { listRoute, lookupLook } from "./layout";

const { id, graph = undefined, below = false } = defineProps<{ id: string; graph?: Graph; below?: boolean }>();
const emit = defineEmits<{ close: []; select: [id: string] }>();

const route = useRoute();
const router = useRouter();
const editor = useEditorStore();
const workspace = useWorkspaceStore();
const { copy, copied } = useClipboard();
const [DefineWay, Way] = createReusableTemplate();
const [DefineRule, Rule] = createReusableTemplate();
const [DefineCopy, Copy] = createReusableTemplate();
// one firing on the Links tab, by the node at its given end
const [DefineLink, Link] = createReusableTemplate<{ edge: Edge; end: "from" | "to" }>();

const { data: detail, error } = useQuery(() => ({ ...detailQuery(id), enabled: workspace.isOpen() }));
const node = computed(() => detail.value?.node);
const look = computed(() => lookupLook(node.value?.database ?? ""));
// each node opens on its overview
const tab = shallowRef("overview");
watch(
  () => id,
  () => (tab.value = "overview"),
);
const versions = computed(() =>
  [...(detail.value?.sections ?? []), ...(detail.value?.options ?? [])].flatMap((section) => section.texts ?? []),
);
// the statements at the top of the definition that write one value and show no text; its blocks are in its script
const properties = computed(() =>
  (detail.value?.sections ?? []).filter((section) => section.value && !section.texts?.length),
);
const fires = computed(() => node.value?.fires ?? []);
const firedBy = computed(() => detail.value?.firedBy ?? []);
const lines = computed(() => detail.value?.source.split("\n").length ?? 0);
const tabs = computed(() => [
  { label: "Overview", value: "overview" },
  { label: "Text", value: "text", badge: versions.value.length || undefined },
  { label: "Links", value: "links", badge: fires.value.length + firedBy.value.length || undefined },
  { label: "Script", value: "script" },
]);

/** The nodes the selected one comes up through, the first cause first: the root's caller, the route, and the node. */
const way = computed(() => {
  if (!graph) return [];
  const nodes = new Map((graph.nodes ?? []).map((one) => [one.id, one]));
  const chain = listRoute(graph, id).flatMap((one) => nodes.get(one) ?? []);
  if ((nodes.get(id)?.step ?? 0) >= 0) {
    chain.reverse();
    const callers = (graph?.nodes ?? []).filter((one) => one.step === -1);
    const caller = callers.find((one) => one.hardcoded) ?? callers[0];
    if (caller && chain[0]?.id === graph?.root && id !== caller.id) chain.unshift(caller);
  }
  return chain.map((one, position) => ({ node: one, edge: findEdge(one, chain[position + 1]) }));
});
// the own trigger of each node on the way before the selected one, read from its detail
const callerTriggers = shallowRef<Record<string, Condition[]>>({});
watch(
  () => way.value.map(({ node: one }) => one.id).filter((one) => one !== id),
  async (ids, _previous, onCleanup) => {
    let stale = false;
    onCleanup(() => (stale = true));
    const pairs = await Promise.all(ids.map(async (one) => [one, await loadTrigger(one)] as const));
    if (!stale) callerTriggers.value = Object.fromEntries(pairs);
  },
  { immediate: true },
);
// every node's own trigger on the way, the selected one's from its own detail
const triggers = computed(() => ({ ...callerTriggers.value, [id]: detail.value?.trigger ?? [] }));
// what the way says under each of its nodes: its own trigger under "when", then the blocks its next firing sits in
// under "then"
const wayConditions = computed(() =>
  way.value.map(({ node: one, edge }) => {
    const trigger = groupConditions(triggers.value[one.id] ?? []);
    const steps = (edge?.steps ?? []).flatMap(describeStep);
    return [
      ...(trigger.length ? [{ text: "when", conditions: trigger, line: 0 }] : []),
      ...(steps.length ? [{ text: "then", conditions: steps, line: 0 }] : []),
    ];
  }),
);
/** Finds the drawn edge from one node to the next on the way. It finds none for the last. */
function findEdge(firer: Node, next: Node | undefined): Edge | undefined {
  return next && (graph?.edges ?? []).find((edge) => edge.from === firer.id && edge.to === next.id);
}

/** Reads a node's own trigger in words from its detail. It reads none for a node that writes none. */
async function loadTrigger(nodeId: string): Promise<Condition[]> {
  const other = await loadQuery(detailQuery(nodeId)).catch(() => undefined);
  return other?.trigger ?? [];
}

/** Describes the blocks a firing is written in, as a row reads them: "option · trigger_event, days = 30". */
function describeHow(edge: Edge): string {
  const blocks = (edge.steps ?? []).filter((step) => step.kind !== "block").map((step) => step.weight ?? step.key);
  return [...blocks, [edge.effect, edge.delay].filter(Boolean).join(", ")].filter(Boolean).join(" · ");
}

/** Returns the node at the other end of a firing, as the detail names it. */
function findLinked(edge: Edge, end: "from" | "to"): Node | undefined {
  return detail.value?.linked?.[edge[end]];
}

/** Draws the graph from the node with the given id. */
function setRoot(root: string): void {
  void router.replace({ query: { root } });
}

/** Shows a file at a line. */
function show(place: Place, line: number): void {
  editor.show(place, { lineNumber: line, column: 1 }, "graph");
}
</script>

<template>
  <aside class="flex min-h-0 w-full min-w-0 flex-col overflow-hidden bg-muted">
    <UAlert v-if="error" color="error" variant="subtle" :description="error.message" class="m-3" />
    <template v-else-if="detail && node">
      <DefineWay>
        <SectionHead title="How it triggers" />
        <ol class="flex flex-col px-4 pt-2">
          <li v-for="({ node: one }, position) in way" :key="one.id" class="relative flex gap-2.5 pb-3 last:pb-0">
            <span v-if="position < way.length - 1" class="absolute start-2.75 top-6.5 bottom-0.5 w-px bg-(--line-2)" />
            <span
              class="grid size-5.75 shrink-0 place-items-center rounded-[7px] border bg-(--bg-3)"
              :class="one.id === id ? 'border-(--link) ring-3 ring-(--link-soft)' : 'border-(--line-2)'"
            >
              <UIcon
                :name="one.display === 'hidden' ? 'i-lucide-eye-off' : lookupLook(one.database).icon"
                class="size-3.25"
                :style="{ color: lookupLook(one.database).color }"
              />
            </span>
            <div class="flex min-w-0 flex-1 flex-col gap-1 pt-0.5">
              <span class="flex items-center gap-1.5 text-12 leading-4.5 whitespace-nowrap text-dimmed">
                <code class="font-mono font-semibold text-highlighted">{{ one.name }}</code>
                {{ one.id === id ? "here" : one.display === "hidden" ? "hidden event" : one.what }}
                <span
                  v-if="one.hardcoded"
                  class="inline-flex h-4 items-center gap-0.75 rounded-sm bg-(--bg-4) px-1.25 text-[10px]"
                  ><UIcon name="i-lucide-cog" class="size-2.5" />the game calls it</span
                >
              </span>
              <ConditionList
                v-if="wayConditions[position]?.length"
                :conditions="wayConditions[position] ?? []"
                class="text-12 leading-4.5"
              />
              <span v-if="one.id === id && !triggers[one.id]?.length" class="text-12 leading-4.5 text-dimmed"
                >with no trigger of its own</span
              >
            </div>
          </li>
        </ol>
      </DefineWay>
      <DefineRule>
        <template v-for="rule in detail.rules ?? []" :key="rule.id">
          <SectionHead title="The rule it reads"
            ><span class="font-mono text-11 font-normal text-dimmed">{{ rule.id }}</span></SectionHead
          >
          <div class="mx-4 mt-2 flex flex-col gap-1 rounded-lg border border-(--line-1) bg-(--bg-2) px-2.5 py-2">
            <div v-for="option in rule.options ?? []" :key="option.key" class="flex items-baseline gap-2.5 text-11">
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
          </div>
        </template>
      </DefineRule>
      <DefineCopy>
        <template v-if="detail.copy">
          <SectionHead title="It replaces the game's">
            <UButton
              label="Compare"
              icon="i-lucide-git-compare"
              variant="ghost"
              size="xs"
              @click="detail.copy && compare(detail.copy.place, node.place)"
            />
          </SectionHead>
          <div class="px-4 pt-2"><SourceLink :place="detail.copy.place" :line="detail.copy.line" /></div>
        </template>
      </DefineCopy>
      <DefineLink v-slot="{ edge, end }">
        <li
          role="button"
          tabindex="0"
          title="Select it; double-click to draw the graph from it"
          class="flex min-h-7 cursor-pointer items-center gap-2 rounded-chip px-2 py-1 even:bg-accented/40 hover:bg-accented focus-visible:outline-2 focus-visible:outline-primary"
          @click="emit('select', edge[end])"
          @dblclick="setRoot(edge[end])"
          @keydown.enter="emit('select', edge[end])"
          @keydown.space.prevent="emit('select', edge[end])"
        >
          <UIcon
            :name="lookupLook(findLinked(edge, end)?.database ?? '').icon"
            class="size-3.5 shrink-0"
            :style="{ color: lookupLook(findLinked(edge, end)?.database ?? '').color }"
          />
          <span class="min-w-0 shrink-0 truncate font-mono text-12 text-highlighted">{{
            findLinked(edge, end)?.name ?? edge[end]
          }}</span>
          <span class="min-w-0 flex-1 text-11 wrap-anywhere text-dimmed">{{ describeHow(edge) }}</span>
          <UBadge
            :label="edge.via?.length ? `through ${edge.via.join(', ')}` : 'direct'"
            variant="subtle"
            :color="edge.via?.length ? 'warning' : 'neutral'"
            size="sm"
          />
          <!-- the source link opens its file without selecting the row -->
          <span class="flex max-w-40 min-w-0 shrink" @click.stop @dblclick.stop>
            <SourceLink :place="edge.place" :line="edge.line" class="min-w-0" />
          </span>
        </li>
      </DefineLink>

      <div class="flex shrink-0 flex-col gap-1.5 border-b border-default px-4 pt-3 pb-2.5" :class="below && 'pb-0'">
        <div class="flex flex-wrap items-center gap-2">
          <UBadge
            :label="`${node.what}${node.display === 'hidden' ? ', hidden' : ''}`"
            :icon="node.display === 'hidden' ? 'i-lucide-eye-off' : look.icon"
            size="sm"
            class="bg-current/16"
            :style="{ color: look.color }"
          />
          <template v-if="below">
            <span class="font-mono text-14 font-semibold text-highlighted">{{ node.name }}</span>
            <SourceLink :place="node.place" :line="node.line" />
            <OriginChip :origin="node.place.origin" />
          </template>
          <span class="grow" />
          <UTabs
            v-if="below"
            :model-value="tab"
            :items="tabs"
            :content="false"
            variant="link"
            :ui="{ list: 'border-0' }"
            @update:model-value="tab = String($event)"
          >
            <template #trailing="{ item }">
              <span v-if="item.value === 'script'" class="text-11 font-normal text-dimmed">{{ lines }} lines</span>
            </template>
          </UTabs>
          <UButton
            label="Set as root"
            icon="i-lucide-target"
            color="primary"
            variant="solid"
            :disabled="route.query.root === id"
            @click="setRoot(id)"
          />
          <UButton label="Open in IDE" icon="i-lucide-external-link" @click="show(node.place, node.line)" />
          <UButton
            icon="i-lucide-x"
            variant="ghost"
            size="sm"
            square
            aria-label="Close detail"
            @click="emit('close')"
          />
        </div>
        <template v-if="!below">
          <span class="font-mono text-16 leading-5.5 font-semibold break-all text-highlighted">{{ node.name }}</span>
          <span class="flex items-center gap-2"
            ><SourceLink :place="node.place" :line="node.line" /><OriginChip :origin="node.place.origin"
          /></span>
          <p v-if="node.comment" class="font-mono text-11 leading-4.25 whitespace-pre-line text-(--syn-comment) italic">
            {{ node.comment }}
          </p>
          <UTabs
            :model-value="tab"
            :items="tabs"
            :content="false"
            variant="link"
            class="-mx-1"
            @update:model-value="tab = String($event)"
          >
            <template #trailing="{ item }">
              <span v-if="item.value === 'script'" class="text-11 font-normal text-dimmed">{{ lines }} lines</span>
            </template>
          </UTabs>
        </template>
      </div>

      <div class="min-h-0 flex-1 overflow-auto">
        <div
          v-if="tab === 'overview'"
          :class="
            below
              ? 'grid min-h-full auto-cols-[minmax(0,1fr)] grid-flow-col divide-x divide-default *:py-3.5'
              : 'flex flex-col gap-4 py-3.5'
          "
        >
          <section><Way /></section>
          <section v-if="detail.rules?.length"><Rule /></section>
          <section v-if="detail.copy"><Copy /></section>
          <section v-if="properties.length">
            <SectionHead title="Properties" />
            <dl class="grid grid-cols-[max-content_minmax(0,1fr)] items-baseline gap-x-3 gap-y-1 px-4 pt-2 text-12">
              <template v-for="section in properties" :key="section.line">
                <dt class="text-dimmed">{{ section.key }}</dt>
                <dd class="min-w-0 wrap-anywhere">
                  <ScriptSnippet :text="section.value" language="paradox-script" inline />
                </dd>
              </template>
            </dl>
          </section>
        </div>
        <ul v-else-if="tab === 'text'" class="flex flex-col divide-y divide-default px-4 py-1.5">
          <li v-for="(version, position) in versions" :key="position" class="flex flex-col gap-0.5 py-2">
            <UButton
              :label="version.key"
              variant="link"
              color="neutral"
              size="xs"
              class="min-w-0 self-start p-0 font-mono text-dimmed"
              :ui="{ label: 'truncate' }"
              @click="show(node.place, version.line)"
            />
            <p class="text-13 whitespace-pre-line text-highlighted"><LocText :segments="version.segments" /></p>
            <p v-if="version.missing" class="text-12 text-warning">Missing in {{ detail.language }}</p>
            <ConditionList
              v-if="version.when?.length"
              :conditions="[{ text: 'when', conditions: groupConditions(version.when), line: version.line }]"
              class="text-11"
            />
          </li>
          <li v-if="!versions.length" class="py-2 text-12 text-dimmed">It shows no text.</li>
        </ul>
        <div v-else-if="tab === 'links'" class="flex flex-col gap-4 py-3.5">
          <section v-if="fires.length">
            <SectionHead title="Fires" :count="fires.length" />
            <ul class="flex flex-col px-2 pt-1.5">
              <Link v-for="(edge, position) in fires" :key="position" :edge="edge" end="to" />
            </ul>
          </section>
          <section v-if="firedBy.length">
            <SectionHead title="Fired by" :count="firedBy.length" />
            <ul class="flex flex-col px-2 pt-1.5">
              <Link v-for="(edge, position) in firedBy" :key="position" :edge="edge" end="from" />
            </ul>
          </section>
          <p v-if="!fires.length && !firedBy.length" class="px-4 text-12 text-dimmed">
            It fires nothing, and nothing fires it.
          </p>
        </div>
        <div v-else class="px-4 py-3.5">
          <div class="overflow-hidden rounded-lg border border-default">
            <div
              class="flex h-8 items-center gap-2 border-b border-default bg-elevated px-2.5 text-12 whitespace-nowrap"
            >
              <UIcon name="i-lucide-code" class="size-3.5 shrink-0 text-dimmed" />
              <SourceLink :place="node.place" :line="node.line" />
              <span class="grow" />
              <UButton
                :icon="copied ? 'i-lucide-check' : 'i-lucide-copy'"
                variant="ghost"
                size="xs"
                square
                aria-label="Copy the definition"
                @click="copy(detail.source)"
              />
              <UButton
                icon="i-lucide-external-link"
                variant="ghost"
                size="xs"
                square
                aria-label="Open in IDE"
                @click="show(node.place, node.line)"
              />
            </div>
            <ScriptSnippet :text="detail.source" :line="node.line" language="paradox-script" class="px-3 pb-1.5" />
          </div>
        </div>
      </div>
    </template>
  </aside>
</template>
