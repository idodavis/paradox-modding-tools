<script setup lang="ts">
/**
 * The selected definition: its kind, origin, name and file,
 * Set as root and Open in IDE; then three tabs. The first shows it part by part: its type and one-word
 * properties, its title, portraits, texts and options, a part with versions a version at a time; Links, what it
 * fires and what fires it by kind, and the names it reads; Script, as written, in Health's site card.
 */
import { computed, shallowRef, watch } from "vue";
import { createReusableTemplate, useClipboard } from "@vueuse/core";
import type { Cited, Detail, Version } from "@services/internal/pages/models";
import type { Fire } from "@services/internal/workspace/models";
import { kindOf } from "../../composables/useEventGraph";
import { useEditorStore } from "../../stores/editor";
import { useWorkspaceStore } from "../../stores/workspace";
import { capital, countKinds, plural } from "../../lib/format";
import LocQuote from "../../ui/LocQuote.vue";
import OriginChip from "../../ui/OriginChip.vue";
import OriginDot from "../../ui/OriginDot.vue";
import ScriptSnippet from "../../ui/ScriptSnippet.vue";
import SourceLink from "../../ui/SourceLink.vue";

const { detail, isRoot } = defineProps<{ detail: Detail; isRoot: boolean }>();
const emit = defineEmits<{ close: []; root: [id: string] }>();
const store = useWorkspaceStore();
const editor = useEditorStore();
const { copy, copied } = useClipboard();

const kind = computed(() => kindOf(detail.kind));
const language = computed(() => store.activeWorkspace?.defaultLocLang ?? "");

/** A key or word as a label: its first letter capital, its underscores spaces. */
const sentence = (text: string) => capital(text).replaceAll("_", " ");
/** Where a fire sits and what fires it, as written: "option a · trigger_event, days = 30". */
const how = (fire: Fire) =>
  [fire.block, [fire.command, fire.args].filter(Boolean).join(", ")].filter(Boolean).join(" · ");

// the version each stepper shows, by its id; each starts at the first
const indexes = shallowRef<Record<string, number>>({});
type Current = Partial<Version> & { id: string; index: number; count: number };
function currentOf(id: string, versions: Version[] | null | undefined): Current {
  const count = versions?.length ?? 0;
  const index = (indexes.value[id] ?? 0) % Math.max(count, 1);
  return { ...versions?.[index], id, index, count };
}
function step({ id, index, count }: Current, by: number): void {
  indexes.value = { ...indexes.value, [id]: (index + by + count) % count };
}
const [DefineStepper, Stepper] = createReusableTemplate<{ current: Current }>();
const [DefineQuote, Quote] = createReusableTemplate<{ current: Current; random?: boolean; title?: boolean }>();

// its parts: the type, the texts (the title first), the portraits, the options, and its properties: the
// one-word ones, the blocks of one-word keys (a background) and the blocks that apply only when their
// trigger holds (a conditional background)
const sections = computed(() => detail.sections ?? []);
const type = computed(() => sections.value.find((section) => section.key === "type")?.value ?? detail.kind);
const texts = computed(() =>
  sections.value
    .filter((section) => section.parts?.length)
    .sort((a, b) => +(b.key === "title") - +(a.key === "title"))
    .map((section) => ({
      ...section,
      parts: (section.parts ?? []).map((part, p) => ({
        ...part,
        current: currentOf(`${section.line}:${p}`, part.versions),
      })),
    })),
);
const portraits = computed(() => sections.value.filter((section) => section.portrait));
const options = computed(() =>
  (detail.options ?? []).map((option) => ({ ...option, current: currentOf(`option:${option.line}`, option.names) })),
);
const properties = computed(() =>
  sections.value.filter(
    (section) =>
      !section.parts?.length &&
      !section.portrait &&
      !["type", "option"].includes(section.key) &&
      (section.when || !section.value?.includes("{ … }")),
  ),
);

// what it fires and what fires it, the first few at a time, each kind counted
const allFires = shallowRef(false);
const allFiredBy = shallowRef(false);
const fires = computed(() => (allFires.value ? detail.fires : detail.fires?.slice(0, 6)) ?? []);
const firedBy = computed(() => (allFiredBy.value ? detail.firedBy : detail.firedBy?.slice(0, 8)) ?? []);
// a chain every caller shares is said once
const sharedVia = computed(() => {
  const vias = new Set(detail.firedBy?.map((fire) => fire.via));
  return vias.size === 1 ? [...vias][0] : "";
});

// what it reads: each database's names once, with how often and where first
const READ_COLORS: Record<string, string> = {
  scope: "var(--syn-ref)",
  localization: "var(--syn-str)",
  scripted_effects: "var(--syn-fn)",
  scripted_triggers: "var(--syn-fn)",
  script_values: "var(--syn-num)",
};
const reads = computed(() =>
  (detail.refs ?? []).map((group) => {
    const names = new Map<string, { name: string; count: number; at: Cited }>();
    for (const cited of group.refs ?? []) {
      const first = names.get(cited.name);
      if (first) first.count++;
      else names.set(cited.name, { name: cited.name, count: 1, at: cited });
    }
    return {
      ...group,
      uses: group.refs?.length ?? 0,
      names: [...names.values()],
      color: READ_COLORS[group.database] ?? "var(--syn-kw)",
    };
  }),
);
const uses = computed(() => reads.value.reduce((sum, group) => sum + group.uses, 0));
const nameCount = computed(() => reads.value.reduce((sum, group) => sum + group.names.length, 0));
const openRead = shallowRef("");

watch(
  () => detail.name,
  () => {
    indexes.value = {};
    allFires.value = allFiredBy.value = false;
    openRead.value = "";
  },
);

const tabs = computed(() => [
  { label: sentence(detail.kind), slot: "main" as const },
  {
    label: "Links",
    slot: "links" as const,
    badge: { label: String((detail.fires?.length ?? 0) + (detail.firedBy?.length ?? 0)), size: "sm" as const },
  },
  { label: "Script", slot: "script" as const },
]);

/** Open a line of a file in the IDE. */
function open(path: string, line: number): void {
  editor.open(path, { line });
}
</script>

<template>
  <aside class="flex min-h-0 min-w-0 flex-col bg-muted">
    <div class="flex flex-col gap-1.5 border-b border-default px-4 pt-3.5 pb-3">
      <div class="flex items-center gap-2">
        <UBadge :label="detail.kind" :icon="kind.icon" size="sm" class="bg-current/16" :style="{ color: kind.color }" />
        <OriginChip :label="store.originName(detail.at.origin)" :color="store.originColor(detail.at.origin)" small />
        <span class="grow" />
        <UButton icon="i-lucide-x" variant="ghost" size="sm" square aria-label="Close detail" @click="emit('close')" />
      </div>
      <span class="font-mono text-16 leading-5.5 font-semibold break-all text-highlighted">{{ detail.name }}</span>
      <SourceLink :place="detail.at" :line="detail.line" class="self-start" />
      <div class="mt-1.5 flex items-center gap-2">
        <UButton
          label="Set as root"
          icon="i-lucide-target"
          color="primary"
          variant="solid"
          :disabled="isRoot"
          @click="emit('root', detail.name)"
        />
        <span class="grow" />
        <UButton label="Open in IDE" icon="i-lucide-external-link" @click="open(detail.at.path, detail.line)" />
      </div>
    </div>
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
        <DefineQuote v-slot="{ current, random, title }">
          <LocQuote
            :text="current.text || `No ${language} text`"
            bare
            :class="title ? 'text-center text-18 leading-6 font-semibold text-highlighted' : 'text-13 text-highlighted'"
          />
          <p
            v-if="current.when || (current.index && !random)"
            class="truncate text-11 text-dimmed"
            :title="current.when"
          >
            when <code v-if="current.when" class="font-mono text-muted">{{ current.when }}</code
            ><template v-else>none before it applies</template>
          </p>
        </DefineQuote>
        <div class="flex flex-col gap-2.5 px-4 py-3.5">
          <div class="flex flex-col gap-1">
            <span class="text-11 text-muted">{{ sentence(type) }}</span>
            <p
              v-for="section in properties"
              :key="section.line"
              class="truncate text-11 text-muted"
              :title="section.when"
            >
              {{ section.key }} <ScriptSnippet :text="section.value ?? ''" :name="detail.at.rel" inline />
              <template v-if="section.when">
                <span class="ms-1 text-(--orange)">if</span>
                <code class="font-mono text-dimmed">{{ section.when }}</code>
              </template>
            </p>
          </div>
          <section
            class="flex flex-col divide-y divide-default overflow-hidden rounded-lg border border-default bg-elevated"
          >
            <div
              v-for="section in texts"
              :key="section.line"
              class="flex flex-col gap-1 px-3.5 py-3"
              :class="{ 'bg-accented/40': section.key === 'title' }"
            >
              <span class="flex h-6 items-center text-11 text-muted">
                {{ sentence(section.key) }}<span class="grow" />
                <Stepper
                  v-if="section.parts.length === 1 && section.parts[0].current.count > 1"
                  :current="section.parts[0].current"
                />
              </span>
              <div
                v-for="(part, p) in section.parts"
                :key="p"
                class="flex flex-col gap-1"
                :class="{ 'mt-1 border-t border-dashed border-default pt-2': p }"
              >
                <div v-if="section.parts.length > 1" class="flex h-6 items-center gap-1.5 text-11 text-dimmed">
                  <span v-if="section.parts.length > 1"
                    >Part {{ p + 1 }}{{ part.random ? " · one at random" : "" }}</span
                  >
                  <span class="grow" />
                  <Stepper v-if="part.current.count > 1" :current="part.current" />
                </div>
                <Quote :current="part.current" :random="part.random" :title="section.key === 'title'" />
              </div>
            </div>
            <div v-if="portraits.length" class="flex flex-col gap-1.5 px-3.5 py-3">
              <span class="text-11 text-muted">Portraits</span>
              <div class="flex flex-wrap gap-1.5">
                <div
                  v-for="section in portraits"
                  :key="section.line"
                  class="flex max-w-full min-w-22 flex-col items-center gap-0.5 rounded-md border border-accented bg-muted px-2 py-1.5 text-11"
                  :title="section.when"
                >
                  <UIcon name="i-lucide-user-round" class="size-6 text-dimmed" />
                  <span class="text-dimmed">{{ section.key.replace(/_[a-z]+$/, "").replaceAll("_", " ") }}</span>
                  <span class="max-w-full truncate font-mono text-(--syn-ref)">{{ section.portrait }}</span>
                  <span v-if="section.when" class="text-(--orange)">only if …</span>
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
                    @click="open(detail.at.path, option.current.line ?? option.line)"
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
      <template #links>
        <div class="flex flex-col gap-4 px-4 py-3.5">
          <section v-if="detail.fires?.length" class="flex flex-col gap-1">
            <h4 class="flex flex-wrap items-center gap-2 px-2 pb-1 text-12 font-semibold text-highlighted">
              Fires
              <span
                v-for="row in countKinds(detail.fires ?? [])"
                :key="row.kind"
                class="flex items-center gap-1 text-11 font-normal text-muted"
              >
                <OriginDot :color="kindOf(row.kind).color" small />{{ row.kind
                }}<span class="text-dimmed">{{ row.count }}</span>
              </span>
            </h4>
            <div
              v-for="fire in fires"
              :key="`${fire.name}:${fire.via}`"
              class="flex h-7 items-center gap-2 rounded-chip px-2 even:bg-accented/40"
              :title="fire.via ? `Through ${fire.via}` : 'Directly'"
            >
              <UIcon
                :name="kindOf(fire.kind).icon"
                class="size-3.5 shrink-0"
                :style="{ color: kindOf(fire.kind).color }"
              />
              <UButton
                :label="fire.name"
                variant="link"
                color="neutral"
                size="xs"
                class="min-w-0 shrink-0 p-0 font-mono text-highlighted"
                @click="emit('root', fire.name)"
              />
              <!-- where it fires, opening its line in the IDE -->
              <button
                v-if="how(fire)"
                type="button"
                class="min-w-0 cursor-pointer truncate text-11 text-muted hover:text-highlighted hover:underline"
                :title="`${how(fire)}: open line ${fire.line + 1}`"
                @click="open(fire.path, fire.line)"
              >
                {{ how(fire) }}
              </button>
              <span class="grow" />
              <UBadge
                :label="fire.through ? plural(fire.through, 'effect') : 'direct'"
                size="sm"
                :style="fire.through ? { color: 'var(--purple)', background: 'var(--purple-soft)' } : undefined"
              />
            </div>
            <UButton
              v-if="(detail.fires?.length ?? 0) > 6"
              :label="allFires ? 'Fewer' : `${(detail.fires?.length ?? 0) - 6} more`"
              variant="link"
              size="xs"
              class="self-start px-2"
              @click="allFires = !allFires"
            />
          </section>
          <section v-if="detail.firedBy?.length" class="flex flex-col gap-1.5">
            <h4 class="flex flex-wrap items-center gap-2 px-2 text-12 font-semibold text-highlighted">
              Fired by
              <span
                v-for="row in countKinds(detail.firedBy ?? [])"
                :key="row.kind"
                class="flex items-center gap-1 text-11 font-normal text-muted"
              >
                <OriginDot :color="kindOf(row.kind).color" small />{{ row.kind
                }}<span class="text-dimmed">{{ row.count }}</span>
              </span>
              <span v-if="sharedVia" class="text-11 font-normal text-muted">
                through <span class="font-mono text-(--purple)">{{ sharedVia }}</span>
              </span>
            </h4>
            <div class="flex flex-wrap gap-1 px-2">
              <UTooltip
                v-for="fire in firedBy"
                :key="`${fire.name}:${fire.via}`"
                :text="[how(fire), fire.via ? `through ${fire.via}` : ''].filter(Boolean).join(', ') || 'Directly'"
              >
                <UButton
                  :label="fire.name"
                  variant="outline"
                  size="xs"
                  class="font-mono"
                  @click="emit('root', fire.name)"
                />
              </UTooltip>
              <UButton
                v-if="(detail.firedBy?.length ?? 0) > 8"
                :label="allFiredBy ? 'Fewer' : `${(detail.firedBy?.length ?? 0) - 8} more`"
                variant="link"
                size="xs"
                @click="allFiredBy = !allFiredBy"
              />
            </div>
          </section>
          <section v-if="reads.length" class="flex flex-col gap-1.5">
            <h4 class="flex items-center gap-2 px-2 text-12 font-semibold text-highlighted">
              Reads<span class="text-11 font-normal text-dimmed">{{ uses }} uses of {{ nameCount }} names</span>
            </h4>
            <div class="flex flex-wrap gap-1 px-2">
              <UButton
                v-for="group in reads"
                :key="group.database"
                variant="outline"
                size="xs"
                :class="{ 'bg-elevated': openRead === group.database }"
                @click="openRead = openRead === group.database ? '' : group.database"
              >
                <OriginDot :color="group.color" small />{{ group.kind
                }}<span class="text-dimmed">{{ group.uses }}</span>
              </UButton>
            </div>
            <template v-for="group in reads" :key="group.database">
              <div
                v-if="openRead === group.database"
                class="mx-2 flex flex-wrap gap-x-3 gap-y-1 rounded-md border border-default bg-elevated px-2.5 py-2"
              >
                <UButton
                  v-for="read in group.names"
                  :key="read.name"
                  variant="link"
                  size="xs"
                  class="p-0 font-mono"
                  @click="open(read.at.at.path, read.at.line)"
                >
                  {{ read.name }}<span v-if="read.count > 1" class="text-dimmed">×{{ read.count }}</span>
                </UButton>
              </div>
            </template>
          </section>
        </div>
      </template>
      <template #script>
        <div class="px-4 py-3.5">
          <div v-if="detail.lines?.length" class="overflow-hidden rounded-lg border border-default bg-(--bg-0)">
            <div
              class="flex h-8 items-center gap-2 border-b border-default bg-elevated px-2.5 text-12 whitespace-nowrap"
            >
              <UIcon name="i-lucide-code" class="size-3.5 shrink-0 text-dimmed" />
              <SourceLink :place="detail.at" :line="detail.line" />
              <span class="grow" />
              <span class="text-11 text-dimmed">{{ detail.lines.length }} lines</span>
              <UButton
                :icon="copied ? 'i-lucide-check' : 'i-lucide-copy'"
                variant="ghost"
                size="xs"
                square
                aria-label="Copy the definition"
                @click="copy(detail.lines.join('\n'))"
              />
              <UButton
                icon="i-lucide-external-link"
                variant="ghost"
                size="xs"
                square
                aria-label="Open in IDE"
                @click="open(detail.at.path, detail.line)"
              />
            </div>
            <ScriptSnippet
              :text="detail.lines.join('\n')"
              :name="detail.at.rel"
              :from-line="detail.line"
              class="py-1.5"
            />
          </div>
        </div>
      </template>
    </UTabs>
  </aside>
</template>
