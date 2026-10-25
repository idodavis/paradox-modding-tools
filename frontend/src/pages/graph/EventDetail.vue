<!--
  The detail of a graph node, in tabs: its type, properties, texts, portraits and options; each of its texts with its
  key; what it fires, what fires it and what it reads; and its script as written.
-->
<script setup lang="ts">
import { computed, shallowRef, watch } from "vue";
import { useQuery } from "@pinia/colada";
import { createReusableTemplate, useClipboard } from "@vueuse/core";
import { capitalize, groupBy, xor } from "es-toolkit";
import { useRoute, useRouter } from "vue-router";
import type { Edge, Version } from "@services/graph/models";
import type { Place } from "@services/workspace/models";
import { plural } from "../../lib/format";
import { findPlaceholder } from "../../lib/placeholders";
import { detailQuery, fileUrl } from "../../queries";
import { useEditorStore } from "../../stores/editor";
import { useWorkspaceStore } from "../../stores/workspace";
import OriginChip from "../../ui/OriginChip.vue";
import ScriptSnippet from "../../ui/ScriptSnippet.vue";
import SectionHead from "../../ui/SectionHead.vue";
import SourceLink from "../../ui/SourceLink.vue";
import { lookupLook } from "./layout";

const { id } = defineProps<{ id: string }>();
const emit = defineEmits<{ close: [] }>();

/**
 * The two lists of firings: what the node fires and what fires it, by the detail's key, the end of an edge each row
 * names, and how many rows show before the rest fold.
 */
const FIRINGS = [
  { title: "Fires", key: "fires", end: "to", fold: 6 },
  { title: "Fired by", key: "firedBy", end: "from", fold: 8 },
] as const;

const route = useRoute();
const router = useRouter();
const editor = useEditorStore();
const workspace = useWorkspaceStore();
const { copy, copied } = useClipboard();

const { data: detail, error } = useQuery(() => ({ ...detailQuery(id), enabled: workspace.isOpen() }));
const silhouette = computed(() => findPlaceholder(detail.value?.silhouette ?? ""));
const look = computed(() => lookupLook(detail.value?.node.database ?? ""));

// the version each stepper shows, by its id; each starts at the first
const indexes = shallowRef<Record<string, number>>({});
// the lists of firings shown whole, by their key
const full = shallowRef<string[]>([]);
const [DefineStepper, Stepper] = createReusableTemplate<{ current: ReturnType<typeof buildCurrent> }>();
const [DefineQuote, Quote] = createReusableTemplate<{
  current: Partial<Version> & { index?: number };
  random?: boolean;
  title?: boolean;
}>();

const sections = computed(() => detail.value?.sections ?? []);
const type = computed(() => sections.value.find((section) => section.key === "type")?.value ?? detail.value?.node.what);
const texts = computed(() =>
  sections.value
    .filter((section) => section.texts?.length)
    .map((section) => ({ ...section, current: buildCurrent(`text:${section.line}`, section.texts) })),
);
const options = computed(() =>
  (detail.value?.options ?? []).map((option) => ({
    ...option,
    current: buildCurrent(`option:${option.line}`, option.texts),
  })),
);
const properties = computed(() =>
  sections.value.filter(
    (section) =>
      !section.texts?.length &&
      !["type", "option"].includes(section.key) &&
      (section.when || !section.value.includes("{ … }")),
  ),
);
// every text the node shows, its sections' and then its options', and how many the detail's language lacks
const versions = computed(() =>
  [...sections.value, ...(detail.value?.options ?? [])].flatMap((section) => section.texts ?? []),
);
const missingCount = computed(() => versions.value.filter((version) => version.missing).length);
const tabs = computed(() => [
  { label: capitalize(detail.value?.node.what ?? ""), slot: "main" },
  { label: "Text", slot: "text", badge: versions.value.length || undefined },
  { label: "Links", slot: "links" },
  { label: "Script", slot: "script" },
]);
const firings = computed(() =>
  FIRINGS.map((list) => {
    const edges = detail.value?.[list.key] ?? [];
    return { ...list, edges, rows: full.value.includes(list.key) ? edges : edges.slice(0, list.fold) };
  }),
);
const references = computed(() => Object.entries(groupBy(detail.value?.references ?? [], (node) => node.what)));

watch(
  () => id,
  () => {
    indexes.value = {};
    full.value = [];
  },
);

/** Builds what a stepper shows for the given versions of a text: the one it is at, its place and how many there are. */
function buildCurrent(stepperId: string, versions: Version[] | null | undefined) {
  const count = versions?.length ?? 0;
  const index = (indexes.value[stepperId] ?? 0) % Math.max(count, 1);
  return { ...versions?.[index], id: stepperId, index, count };
}

/** Moves a stepper to the next version, or the previous one for a negative step. */
function step(current: ReturnType<typeof buildCurrent>, by: number): void {
  indexes.value = { ...indexes.value, [current.id]: (current.index + by + current.count) % current.count };
}

/** Returns the node at the given end of an edge. It returns nothing for a node Go did not send. */
function findLinked(edge: Edge, end: "from" | "to") {
  return detail.value?.linked?.[edge[end]];
}

/** Counts the nodes at the given end of some edges by what they are, with the color of their database. */
function countKinds(edges: Edge[], end: "from" | "to") {
  return Object.entries(groupBy(edges, (edge) => findLinked(edge, end)?.what ?? "")).map(([what, group]) => ({
    what,
    count: group.length,
    color: lookupLook(findLinked(group[0], end)?.database ?? "").color,
  }));
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
  <aside class="flex min-h-0 min-w-0 flex-col overflow-hidden bg-muted">
    <UAlert v-if="error" color="error" variant="subtle" :description="error.message" class="m-3" />
    <template v-else-if="detail">
      <div class="flex shrink-0 flex-col gap-1.5 border-b border-default px-4 pt-3.5 pb-3">
        <div class="flex flex-wrap items-center gap-2">
          <UBadge
            :label="detail.node.what"
            :icon="look.icon"
            size="sm"
            class="bg-current/16"
            :style="{ color: look.color }"
          />
          <OriginChip :origin="detail.node.place.origin" />
          <span class="grow" />
          <UButton
            label="Set as root"
            icon="i-lucide-target"
            color="primary"
            variant="solid"
            :disabled="route.query.root === id"
            @click="setRoot(id)"
          />
          <UButton
            label="Open in IDE"
            icon="i-lucide-external-link"
            @click="show(detail.node.place, detail.node.line)"
          />
          <UButton
            icon="i-lucide-x"
            variant="ghost"
            size="sm"
            square
            aria-label="Close detail"
            @click="emit('close')"
          />
        </div>
        <span class="font-mono text-16 leading-5.5 font-semibold break-all text-highlighted">{{
          detail.node.name
        }}</span>
        <SourceLink :place="detail.node.place" :line="detail.node.line" class="self-start" />
      </div>
      <DefineQuote v-slot="{ current, random, title }">
        <p
          v-if="current.text || !current.missing"
          class="whitespace-pre-line"
          :class="title ? 'text-center text-18 leading-6 font-semibold text-highlighted' : 'text-13 text-highlighted'"
        >
          {{ current.text || "No text" }}
        </p>
        <p v-if="current.missing" class="text-12 text-warning" :class="{ 'text-center': title }">
          Missing in {{ detail.language }}
        </p>
        <p v-if="current.when || (current.index && !random)" class="truncate text-11 text-dimmed" :title="current.when">
          when <code v-if="current.when" class="font-mono text-muted">{{ current.when }}</code
          ><template v-else>none before it applies</template>
        </p>
      </DefineQuote>
      <UTabs
        :items="tabs"
        variant="link"
        class="min-h-0 flex-1 gap-0"
        :ui="{ list: 'px-3', content: 'min-h-0 overflow-auto' }"
      >
        <template #main>
          <DefineStepper v-slot="{ current }">
            <span class="flex items-center text-11 text-muted tabular-nums">
              <UButton
                icon="i-lucide-chevron-left"
                variant="ghost"
                size="xs"
                square
                aria-label="Previous version"
                @click="step(current, -1)"
              />
              {{ current.index + 1 }} of {{ current.count }}
              <UButton
                icon="i-lucide-chevron-right"
                variant="ghost"
                size="xs"
                square
                aria-label="Next version"
                @click="step(current, 1)"
              />
            </span>
          </DefineStepper>
          <div class="flex flex-col gap-2.5 px-4 py-3.5">
            <div class="flex flex-col gap-1">
              <span class="flex items-center gap-2 text-11 text-muted">
                {{ capitalize((type ?? "").replaceAll("_", " ")) }}
                <UBadge
                  v-if="missingCount"
                  :label="plural(missingCount, 'missing text')"
                  color="warning"
                  variant="subtle"
                  size="sm"
                />
              </span>
              <p
                v-for="section in properties"
                :key="section.line"
                class="truncate text-11 text-muted"
                :title="section.when"
              >
                {{ section.key }} <code class="font-mono text-toned">{{ section.value }}</code>
                <template v-if="section.when">
                  <span class="ms-1 text-(--orange)">if</span>
                  <code class="font-mono text-dimmed">{{ section.when }}</code>
                </template>
              </p>
            </div>
            <img
              v-if="detail.background"
              :src="fileUrl(detail.background)"
              alt="The event's background"
              class="max-h-40 rounded-lg border border-default object-cover"
            />
            <section
              v-if="texts.length || detail.portraits?.length || options.length"
              class="flex flex-col divide-y divide-default overflow-hidden rounded-lg border border-default bg-elevated"
            >
              <div
                v-for="section in texts"
                :key="section.line"
                class="flex flex-col gap-1 px-3.5 py-3"
                :class="{ 'bg-accented/40': section.key === 'title' }"
              >
                <span class="flex h-6 items-center text-11 text-muted">
                  {{ capitalize(section.key.replaceAll("_", " "))
                  }}<template v-if="section.random">, one at random</template>
                  <span class="grow" />
                  <Stepper v-if="section.current.count > 1" :current="section.current" />
                </span>
                <Quote :current="section.current" :random="section.random" :title="section.key === 'title'" />
              </div>
              <div v-if="detail.portraits?.length" class="flex flex-col gap-1.5 px-3.5 py-3">
                <span class="text-11 text-muted">Portraits</span>
                <div class="flex flex-wrap gap-1.5">
                  <div
                    v-for="portrait in detail.portraits"
                    :key="portrait"
                    class="flex max-w-full min-w-22 flex-col items-center gap-0.5 rounded-md border border-accented bg-muted px-2 py-1.5 text-11"
                  >
                    <div class="h-16 w-14" :style="silhouette" />
                    <span class="max-w-full truncate font-mono text-(--syn-scope)">{{ portrait }}</span>
                  </div>
                </div>
              </div>
              <div v-if="options.length" class="flex flex-col gap-1.5 px-3.5 py-3">
                <span class="text-11 text-muted">Options</span>
                <div
                  v-for="option in options"
                  :key="option.line"
                  class="flex flex-col gap-1 rounded-md border border-accented bg-muted px-3 py-2"
                >
                  <p v-if="option.when" class="truncate text-11 text-dimmed" :title="option.when">
                    shown when <code class="font-mono text-muted">{{ option.when }}</code>
                  </p>
                  <Quote v-if="option.current.count" :current="option.current" />
                  <span class="flex items-center gap-1.5">
                    <UButton
                      :label="option.current.key || 'option'"
                      variant="link"
                      color="neutral"
                      size="xs"
                      class="min-w-0 p-0 font-mono text-dimmed"
                      @click="show(detail.node.place, option.current.line ?? option.line)"
                    />
                    <UBadge v-for="flag in option.flags" :key="flag" :label="flag" size="sm" />
                    <span class="grow" />
                    <Stepper v-if="option.current.count > 1" :current="option.current" />
                  </span>
                </div>
              </div>
            </section>
          </div>
        </template>
        <template #text>
          <ul v-if="versions.length" class="flex flex-col divide-y divide-default px-4 py-1.5">
            <li v-for="(version, position) in versions" :key="position" class="flex flex-col gap-0.5 py-2">
              <UButton
                :label="version.key"
                variant="link"
                color="neutral"
                size="xs"
                class="min-w-0 self-start p-0 font-mono text-dimmed"
                :ui="{ label: 'truncate' }"
                @click="show(detail.node.place, version.line)"
              />
              <Quote :current="version" />
            </li>
          </ul>
          <p v-else class="px-4 py-3.5 text-12 text-dimmed">It shows no text.</p>
        </template>
        <template #links>
          <div class="flex flex-col gap-4 pb-4">
            <template v-for="list in firings" :key="list.key">
              <section v-if="list.edges.length">
                <SectionHead :title="list.title" :count="list.edges.length">
                  <span
                    v-for="kind in countKinds(list.edges, list.end)"
                    :key="kind.what"
                    class="flex items-center gap-1 text-11 font-normal text-muted"
                  >
                    <span class="size-1.5 rounded-full" :style="{ background: kind.color }" />{{ kind.what }}
                    <span class="text-dimmed">{{ kind.count }}</span>
                  </span>
                </SectionHead>
                <ul class="flex flex-col px-2 pt-1.5">
                  <li
                    v-for="(edge, position) in list.rows"
                    :key="position"
                    class="flex h-7 items-center gap-2 rounded-chip px-2 even:bg-accented/40"
                    :title="edge.via?.length ? `Through ${edge.via.join(', ')}` : 'Directly'"
                  >
                    <UIcon
                      :name="lookupLook(findLinked(edge, list.end)?.database ?? '').icon"
                      class="size-3.5 shrink-0"
                      :style="{ color: lookupLook(findLinked(edge, list.end)?.database ?? '').color }"
                    />
                    <UButton
                      :label="findLinked(edge, list.end)?.name ?? edge[list.end]"
                      variant="link"
                      color="neutral"
                      size="xs"
                      class="min-w-0 p-0 font-mono text-highlighted"
                      :ui="{ label: 'truncate' }"
                      @click="setRoot(edge[list.end])"
                    />
                    <SourceLink :place="edge.place" :line="edge.line" class="min-w-0 shrink" />
                    <span class="grow" />
                    <span v-if="edge.via?.length" class="truncate text-11 text-dimmed"
                      >via {{ edge.via.join(", ") }}</span
                    >
                    <UBadge v-if="edge.days" :label="edge.days" size="sm" />
                  </li>
                </ul>
                <UButton
                  v-if="list.edges.length > list.fold"
                  :label="full.includes(list.key) ? 'Fewer' : `Show ${list.edges.length - list.fold} more`"
                  variant="link"
                  size="xs"
                  class="px-4"
                  @click="full = xor(full, [list.key])"
                />
              </section>
            </template>
            <section v-if="references.length || versions.length">
              <SectionHead title="Reads" :count="(detail.references?.length ?? 0) + versions.length" />
              <dl class="grid grid-cols-[6.5rem_minmax(0,1fr)] items-baseline gap-x-2 gap-y-1 px-4 pt-2.5">
                <template v-for="[what, nodes] in references" :key="what">
                  <dt class="truncate text-11 text-muted">{{ what }}</dt>
                  <dd class="flex flex-wrap gap-x-2">
                    <UTooltip v-for="node in nodes" :key="node.id" :text="`${node.place.rel}:${node.line}`">
                      <UButton
                        :label="node.name"
                        variant="link"
                        color="neutral"
                        size="xs"
                        class="p-0 font-mono text-highlighted"
                        @click="show(node.place, node.line)"
                      />
                    </UTooltip>
                  </dd>
                </template>
                <template v-if="versions.length">
                  <dt class="text-11 text-muted">texts</dt>
                  <dd class="text-12 text-toned">
                    {{ versions.length
                    }}<span v-if="missingCount" class="text-warning"
                      >, {{ missingCount }} missing in {{ detail.language }}</span
                    >
                  </dd>
                </template>
              </dl>
            </section>
          </div>
        </template>
        <template #script>
          <div class="px-4 py-3.5">
            <div class="overflow-hidden rounded-lg border border-default">
              <div
                class="flex h-8 items-center gap-2 border-b border-default bg-elevated px-2.5 text-12 whitespace-nowrap"
              >
                <UIcon name="i-lucide-code" class="size-3.5 shrink-0 text-dimmed" />
                <SourceLink :place="detail.node.place" :line="detail.node.line" />
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
                  @click="show(detail.node.place, detail.node.line)"
                />
              </div>
              <ScriptSnippet :text="detail.source" :line="detail.node.line" language="paradox-script" class="pb-1.5" />
            </div>
          </div>
        </template>
      </UTabs>
    </template>
  </aside>
</template>
