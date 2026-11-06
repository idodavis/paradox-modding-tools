<!-- The detail of a graph node: a head, then the Overview, Text, Links and Script tabs. -->
<script setup lang="ts">
import { computed, shallowRef, watch } from "vue";
import { useQuery } from "@pinia/colada";
import { createReusableTemplate, useClipboard } from "@vueuse/core";
import { useRoute, useRouter } from "vue-router";
import { Markup } from "@internal/loc/models";
import type { Edge, Graph, Node } from "@services/graph/models";
import type { Place } from "@services/workspace/models";
import { compare } from "../../app/commands";
import { detailQuery } from "../../queries";
import { useEditorStore } from "../../stores/editor";
import { useWorkspaceStore } from "../../stores/workspace";
import OriginChip from "../../ui/OriginChip.vue";
import ScriptSnippet from "../../ui/ScriptSnippet.vue";
import SourceLink from "../../ui/SourceLink.vue";
import ConditionList, { describeStep, groupConditions } from "./ConditionList.vue";
import { listRoute, lookupLook } from "./layout";

// CHIP is the classes of the chip a data call or a key in a text is drawn as.
const CHIP =
  "inline-block h-4.5 max-w-[18em] truncate rounded-chip bg-(--bg-4) px-1.5 align-[-3px] text-[11.5px] leading-4.5";

const { id, graph = undefined, below = false } = defineProps<{ id: string; graph?: Graph; below?: boolean }>();
const emit = defineEmits<{ close: []; select: [id: string] }>();

const route = useRoute();
const router = useRouter();
const editor = useEditorStore();
const workspace = useWorkspaceStore();
const { copy, copied } = useClipboard();
// SectionHead is a section's head.
const [DefineSectionHead, SectionHead] = createReusableTemplate<{ title: string; count?: number }>();
// Tabs are the head's tabs.
const [DefineTabs, Tabs] = createReusableTemplate();
// Link is one firing on the Links tab.
const [DefineLink, Link] = createReusableTemplate<{ edge: Edge; end: "from" | "to" }>();

const { data: detail, error } = useQuery(() => ({ ...detailQuery(id), enabled: workspace.isOpen() }));
const node = computed(() => detail.value?.node);
const look = computed(() => lookupLook(node.value?.database ?? ""));
const tab = shallowRef("overview");
watch(
  () => id,
  () => (tab.value = "overview"),
);
const versions = computed(() =>
  [...(detail.value?.sections ?? []), ...(detail.value?.options ?? [])].flatMap((section) => section.texts ?? []),
);
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

/** The chain of nodes that leads to the selected one, the first cause first. */
const way = computed(() => {
  if (!graph) return [];
  const nodes = new Map((graph.nodes ?? []).map((entry) => [entry.id, entry]));
  const chain = listRoute(graph, id).flatMap((routeId) => nodes.get(routeId) ?? []);
  if ((nodes.get(id)?.step ?? 0) >= 0) {
    chain.reverse();
    const callers = (graph.nodes ?? []).filter((entry) => entry.step === -1);
    const caller = callers.find((entry) => entry.hardcoded) ?? callers[0];
    if (caller && chain[0]?.id === graph.root && id !== caller.id) chain.unshift(caller);
  }
  const edges = graph.edges ?? [];
  return chain.map((stop, position) => ({
    node: stop,
    edge: edges.find((edge) => edge.from === stop.id && edge.to === chain[position + 1]?.id),
  }));
});
const wayConditions = computed(() =>
  way.value.map(({ node: stop, edge }) => {
    const trigger = groupConditions(stop.trigger ?? []);
    const steps = (edge?.steps ?? []).flatMap(describeStep);
    return [
      ...(trigger.length ? [{ text: "when", conditions: trigger, line: 0 }] : []),
      ...(steps.length ? [{ text: "then", conditions: steps, line: 0 }] : []),
    ];
  }),
);
/** describeHow describes a firing for a row, such as "option · trigger_event, days = 30". */
function describeHow(edge: Edge): string {
  const blocks = (edge.steps ?? []).filter((step) => step.kind !== "block").map((step) => step.weight ?? step.key);
  return [...blocks, [edge.effect, edge.delay].filter(Boolean).join(", ")].filter(Boolean).join(" · ");
}

/** findLinked returns the node at the given end of a firing, or undefined. */
function findLinked(edge: Edge, end: "from" | "to"): Node | undefined {
  return detail.value?.linked?.[edge[end]];
}

/** setRoot draws the graph from the node with the given id. */
function setRoot(root: string): void {
  void router.replace({ query: { root } });
}

/** show opens the given file in the editor at the given line. */
function show(place: Place, line: number): void {
  editor.show(place, { lineNumber: line, column: 1 });
}
</script>

<template>
  <aside class="flex min-h-0 w-full min-w-0 flex-col overflow-hidden bg-muted">
    <UAlert v-if="error" color="error" variant="subtle" :description="error.message" class="m-3" />
    <template v-else-if="detail && node">
      <DefineSectionHead v-slot="{ title, count, $slots }">
        <div class="flex h-11 shrink-0 items-center gap-2.5 border-b border-default px-3">
          <span class="shrink-0 text-13 font-semibold">{{ title }}</span>
          <span class="grow" />
          <span v-if="count !== undefined" class="shrink-0 text-11 text-dimmed tabular-nums">{{ count }}</span>
          <component :is="$slots.default" />
        </div>
      </DefineSectionHead>
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
          <span class="min-w-0 flex-1 truncate text-11 text-dimmed" :title="describeHow(edge)">{{
            describeHow(edge)
          }}</span>
          <span class="flex max-w-48 min-w-0" :title="edge.via?.length ? `through ${edge.via.join(', ')}` : undefined">
            <UBadge
              :label="edge.via?.length ? `through ${edge.via.join(', ')}` : 'direct'"
              variant="subtle"
              :color="edge.via?.length ? 'warning' : 'neutral'"
              size="sm"
              :ui="{ base: 'min-w-0', label: 'truncate' }"
            />
          </span>
          <!-- The source link opens its file without selecting the row. -->
          <span class="flex max-w-40 min-w-0 shrink" @click.stop @dblclick.stop>
            <SourceLink :place="edge.site.place" :line="edge.site.line" class="min-w-0" />
          </span>
        </li>
      </DefineLink>
      <DefineTabs>
        <UTabs
          :model-value="tab"
          :items="tabs"
          :content="false"
          variant="link"
          @update:model-value="tab = String($event)"
        >
          <template #trailing="{ item }">
            <span v-if="item.value === 'script'" class="text-11 font-normal text-dimmed">{{ lines }} lines</span>
          </template>
        </UTabs>
      </DefineTabs>

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
            <SourceLink :place="node.site.place" :line="node.site.line" />
            <OriginChip :origin="node.site.place.origin" />
          </template>
          <span class="grow" />
          <Tabs v-if="below" :ui="{ list: 'border-0' }" />
          <UButton
            v-if="route.query.root !== id"
            label="Set as root"
            icon="i-lucide-target"
            color="primary"
            variant="solid"
            @click="setRoot(id)"
          />
          <UButton label="Open in IDE" icon="i-lucide-external-link" @click="show(node.site.place, node.site.line)" />
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
            ><SourceLink :place="node.site.place" :line="node.site.line" /><OriginChip :origin="node.site.place.origin"
          /></span>
          <p v-if="node.comment" class="font-mono text-11 leading-4.25 whitespace-pre-line text-(--syn-comment) italic">
            {{ node.comment }}
          </p>
          <Tabs class="-mx-1" />
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
          <section>
            <SectionHead title="How it triggers" />
            <ol class="flex flex-col px-4 pt-2">
              <li v-for="({ node: stop }, position) in way" :key="stop.id" class="relative flex gap-2.5 pb-3 last:pb-0">
                <span
                  v-if="position < way.length - 1"
                  class="absolute start-2.75 top-6.5 bottom-0.5 w-px bg-(--line-2)"
                />
                <span
                  class="grid size-5.75 shrink-0 place-items-center rounded-[7px] border bg-(--bg-3)"
                  :class="stop.id === id ? 'border-(--link) ring-3 ring-(--link-soft)' : 'border-(--line-2)'"
                >
                  <UIcon
                    :name="stop.display === 'hidden' ? 'i-lucide-eye-off' : lookupLook(stop.database).icon"
                    class="size-3.25"
                    :style="{ color: lookupLook(stop.database).color }"
                  />
                </span>
                <div class="flex min-w-0 flex-1 flex-col gap-1 pt-0.5">
                  <span class="flex items-center gap-1.5 text-12 leading-4.5 whitespace-nowrap text-dimmed">
                    <code class="font-mono font-semibold text-highlighted">{{ stop.name }}</code>
                    {{ stop.id === id ? "here" : stop.display === "hidden" ? "hidden event" : stop.what }}
                    <span
                      v-if="stop.hardcoded"
                      class="inline-flex h-4 items-center gap-0.75 rounded-sm bg-(--bg-4) px-1.25 text-[10px]"
                      ><UIcon name="i-lucide-cog" class="size-2.5" />the game calls it</span
                    >
                  </span>
                  <ConditionList
                    v-if="wayConditions[position]?.length"
                    :conditions="wayConditions[position] ?? []"
                    class="text-12 leading-4.5"
                  />
                  <span v-if="stop.id === id && !stop.trigger?.length" class="text-12 leading-4.5 text-dimmed"
                    >with no trigger of its own</span
                  >
                </div>
              </li>
            </ol>
            <p v-if="!firedBy.length && !node.hardcoded" class="px-4 pt-2 text-12 leading-4.5 text-dimmed">
              Nothing fires this yet. An event runs when an on_action, a decision, a character interaction or another
              event fires it.
            </p>
          </section>
          <section v-if="detail.rules?.length">
            <template v-for="rule in detail.rules" :key="rule.id">
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
          </section>
          <section v-if="detail.copy">
            <SectionHead title="It replaces the game's">
              <UButton
                label="Compare"
                icon="i-lucide-git-compare"
                variant="ghost"
                size="xs"
                @click="compare(detail.copy.site.place, node.site.place)"
              />
            </SectionHead>
            <div class="px-4 pt-2"><SourceLink :place="detail.copy.site.place" :line="detail.copy.site.line" /></div>
          </section>
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
              @click="show(node.site.place, version.line)"
            />
            <p class="text-13 whitespace-pre-line text-highlighted">
              <template v-for="(segment, index) in version.segments ?? []" :key="index">
                <span
                  v-if="segment.markup === Markup.MarkupCall"
                  :class="CHIP"
                  class="text-(--syn-command)"
                  :title="segment.text"
                  >{{ segment.text }}</span
                >
                <span
                  v-else-if="segment.markup === Markup.MarkupKey"
                  :class="CHIP"
                  class="font-mono text-(--syn-macro)"
                  >{{ segment.text }}</span
                >
                <br v-else-if="segment.markup === Markup.MarkupBreak" />
                <span v-else-if="segment.markup === Markup.MarkupIcon" class="text-dimmed">{{ segment.text }}</span>
                <template v-else-if="segment.markup === Markup.MarkupText">{{ segment.text }}</template>
              </template>
            </p>
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
              <UButton
                icon="i-lucide-external-link"
                variant="ghost"
                size="xs"
                square
                aria-label="Open in IDE"
                @click="show(node.site.place, node.site.line)"
              />
            </div>
            <ScriptSnippet :text="detail.source" :line="node.site.line" language="paradox-script" class="px-3 pb-1.5" />
          </div>
        </div>
      </div>
    </template>
  </aside>
</template>
