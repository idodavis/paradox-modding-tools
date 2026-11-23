<!--
  A building with production method groups: a row of methods for each group,
  one on at a time, and beside them what the methods that are on come to.
-->
<script setup lang="ts">
import { computed, ref } from "vue";
import type { SplitterItem } from "@nuxt/ui";
import { useQuery } from "@pinia/colada";
import type { Line, Method, MethodGroup, Total } from "@services/graph/models";
import { graphErasQuery } from "../../queries";
import { useWorkspaceStore } from "../../stores/workspace";
import { labelOf } from "./nodes";
import NodeIcon from "./NodeIcon.vue";
import SubjectHead from "./SubjectHead.vue";
import UnlockLine from "./UnlockLine.vue";

const { line } = defineProps<{ line: Line }>();
const emit = defineEmits<{ pick: [ids: string[]]; ask: [id: string] }>();

const PANES: SplitterItem[] = [
  { id: "groups", slot: "groups", order: 1, minSize: 40 },
  {
    id: "totals",
    slot: "totals",
    order: 2,
    defaultSize: 26,
    minSize: 18,
    maxSize: 40,
    collapsible: true,
    collapsedSize: 0,
  },
];
/**
 * LEDGER is what a method and the methods together take, make and employ, in
 * the order they are listed: an amount of a good is blue.
 */
const LEDGER = [
  { key: "takes", title: "Takes", tone: "text-(--blue)" },
  { key: "makes", title: "Makes", tone: "text-(--blue)" },
  { key: "employs", title: "Employs", tone: "" },
] as const;
const LINE = "grid grid-cols-[3.5rem_minmax(0,1fr)] gap-1.5 border-t border-default px-3 py-2";

const workspace = useWorkspaceStore();
const { data: eras } = useQuery(() => ({ ...graphErasQuery, enabled: workspace.isOpen() }));
const chosen = ref<string>();

const era = computed({
  get: () => chosen.value ?? eras.value?.at(-1)?.id,
  set: (id: string | number) => (chosen.value = String(id)),
});
const eraIds = computed(() => (eras.value ?? []).map((definition) => definition.id));
const eraItems = computed(() =>
  (eras.value ?? []).map((definition) => ({ label: labelOf(definition), value: definition.id })),
);
const first = computed(() => line.levels?.[0]);
const totals = computed(() => line.totals);
const picked = computed(() => (line.groups ?? []).flatMap((group) => findPicked(group) ?? []));
const jobsLine = computed(() =>
  totals.value?.jobs === totals.value?.jobsBefore
    ? "jobs in all"
    : `jobs in all, against ${totals.value?.jobsBefore} with the first methods`,
);

/** findPicked returns the method a group runs. */
function findPicked(group: MethodGroup): Method | undefined {
  return group.methods?.find((method) => method.method.id === group.picked);
}

/** isLate reports whether a method is in an era after the chosen one. */
function isLate(method: Method): boolean {
  return !!era.value && !!method.era && eraIds.value.indexOf(method.era.id) > eraIds.value.indexOf(era.value);
}

/** isRemoved reports whether an amount of jobs is negative. */
function isRemoved(text: string): boolean {
  return text.startsWith("-");
}

/** describeChange says how a total stands against the first methods. */
function describeChange(total: Total): string {
  if (!total.before) return "new";

  return total.before === total.text ? "no change" : `${total.before} with the first methods`;
}

/** pick makes a method the one its group runs. */
function pick(group: MethodGroup, method: Method): void {
  if (method.method.id === group.picked) return;

  emit(
    "pick",
    (line.groups ?? []).map((other) => (other.group.id === group.group.id ? method.method.id : other.picked)),
  );
}
</script>

<template>
  <USplitter :items="PANES" auto-save-id="pmt.graph.methods" class="h-full min-h-0 bg-default">
    <template #groups>
      <div class="flex h-full min-h-0 w-full min-w-0 flex-col gap-4 overflow-y-auto p-4">
        <SubjectHead :node="line.head" :group="line.group">
          <span class="flex items-center gap-1.5">
            <template v-if="first?.unlock">Unlocked by</template>
            <UnlockLine :unlock="first?.unlock" :era="first?.era" @ask="emit('ask', $event)" />
          </span>
          <template #aside>
            <span class="flex flex-col items-end gap-1.5 text-12 text-muted">
              Available by
              <UTabs v-model="era" :items="eraItems" :content="false" size="xs" variant="pill" color="neutral" />
            </span>
          </template>
        </SubjectHead>

        <section v-for="group in line.groups" :key="group.group.id" class="grid grid-cols-[7rem_minmax(0,1fr)] gap-x-3">
          <span class="flex flex-col items-start gap-1 pt-3 text-12 text-muted">
            <NodeIcon :node="group.group" class="size-6" />
            <b class="text-16 text-highlighted">{{ labelOf(group.group) }}</b>
            Runs
            <b class="font-medium text-highlighted">{{ labelOf(findPicked(group)?.method ?? group.group) }}</b>
          </span>
          <div class="grid grid-cols-[repeat(auto-fit,minmax(15rem,1fr))] gap-x-2.5">
            <button
              v-for="method in group.methods"
              :key="method.method.id"
              type="button"
              :aria-pressed="method.method.id === group.picked"
              class="relative row-span-5 mb-2.5 grid cursor-pointer grid-rows-subgrid overflow-hidden rounded-xl border bg-muted text-start"
              :class="[
                method.method.id === group.picked ? 'border-secondary' : 'border-accented',
                isLate(method) && 'opacity-45',
              ]"
              @click="pick(group, method)"
            >
              <span
                class="flex items-center gap-2.5 px-3 py-3"
                :class="method.method.id === group.picked && 'bg-(--link-soft)'"
              >
                <NodeIcon :node="method.method" class="size-12" />
                <span class="flex min-w-0 flex-col items-start gap-1 pe-6">
                  <b class="text-13 text-highlighted">{{ labelOf(method.method) }}</b>
                  <UnlockLine :unlock="method.unlock" :era="method.era" @ask="emit('ask', $event)" />
                </span>
                <span
                  class="absolute end-2.5 top-2.5 flex size-4.5 items-center justify-center rounded-full border"
                  :class="method.method.id === group.picked ? 'border-secondary text-(--link)' : 'border-accented'"
                >
                  <UIcon v-if="method.method.id === group.picked" name="i-lucide-check" class="size-3" />
                </span>
              </span>
              <span v-for="row in LEDGER" :key="row.key" :class="LINE">
                <small class="pt-1.5 text-11.5 text-dimmed">{{ row.title }}</small>
                <span class="flex flex-col gap-0.5">
                  <span
                    v-for="amount in method[row.key] ?? []"
                    :key="amount.good.id"
                    class="flex h-8 items-center gap-2 rounded-md px-1.5 text-13"
                    :class="isRemoved(amount.text) ? 'bg-(--red-soft)' : 'bg-default'"
                    @dblclick.stop="emit('ask', amount.good.id)"
                  >
                    <NodeIcon :node="amount.good" class="size-5.5" />
                    <span class="truncate">{{ labelOf(amount.good) }}</span>
                    <b class="ms-auto tabular-nums" :class="isRemoved(amount.text) ? 'text-(--red)' : row.tone">
                      {{ amount.text }}
                    </b>
                  </span>
                  <span v-if="!method[row.key]?.length" class="text-12 leading-8 text-dimmed">Nothing</span>
                </span>
              </span>
              <span :class="method.gives?.length ? [LINE, 'pb-3'] : ''">
                <small v-if="method.gives?.length" class="pt-0.5 text-11.5 text-dimmed">Gives</small>
                <span v-if="method.gives?.length" class="flex flex-col gap-1">
                  <span v-for="fact in method.gives" :key="`${fact.label}|${fact.when}`" class="flex flex-col text-12">
                    <span class="flex justify-between gap-2">
                      <span class="text-muted">{{ fact.label }}</span>
                      <b class="font-medium text-highlighted tabular-nums">{{ fact.text }}</b>
                    </span>
                    <small v-if="fact.when" class="text-11.5 text-dimmed">{{ fact.when }}</small>
                  </span>
                </span>
              </span>
            </button>
          </div>
        </section>

        <p class="flex flex-wrap items-center gap-x-5 gap-y-1 text-11.5 text-dimmed">
          <span class="flex items-center gap-1.5">
            <i class="h-3 w-5.5 rounded-xs border border-secondary bg-(--link-soft)" />On
          </span>
          <span class="flex items-center gap-1.5">
            <i class="h-3 w-5.5 rounded-xs bg-(--red-soft)" />Removes jobs
          </span>
          <span class="flex items-center gap-1.5">
            <i class="h-3 w-5.5 rounded-xs border border-accented opacity-45" />Unlocked after the era you chose
          </span>
          <span class="ms-auto">Every amount is for one level of the building</span>
        </p>
      </div>
    </template>

    <template #totals>
      <aside
        v-if="totals"
        class="flex h-full min-h-0 w-full min-w-0 flex-col overflow-y-auto border-s border-default bg-muted"
      >
        <header class="flex flex-col gap-2.5 border-b border-default bg-elevated px-4 py-3">
          <h2 class="text-16 text-highlighted">With these methods</h2>
          <span class="flex flex-wrap gap-1.5">
            <span
              v-for="method in picked"
              :key="method.method.id"
              class="inline-flex h-7 items-center gap-1.5 rounded-full border border-accented bg-default ps-1 pe-2.5 text-12 font-medium text-highlighted"
            >
              <NodeIcon :node="method.method" class="size-5" />{{ labelOf(method.method) }}
            </span>
          </span>
        </header>
        <template v-for="row in LEDGER" :key="row.key">
          <section v-if="totals[row.key]?.length" class="flex flex-col gap-2 border-b border-default px-4 py-3.5">
            <h3 class="text-12 font-semibold text-muted">{{ row.title }}</h3>
            <div class="grid grid-cols-2 gap-2">
              <div
                v-for="total in totals[row.key] ?? []"
                :key="total.good.id"
                class="flex flex-col gap-0.5 rounded-lg border border-default bg-default p-3"
                @dblclick="emit('ask', total.good.id)"
              >
                <NodeIcon :node="total.good" class="mb-1 size-8" />
                <b class="text-20 tabular-nums" :class="isRemoved(total.text) ? 'text-(--red)' : row.tone">
                  {{ total.text }}
                </b>
                <span class="text-12 text-muted">{{ labelOf(total.good) }}</span>
                <small class="text-11 text-dimmed">{{ describeChange(total) }}</small>
              </div>
            </div>
          </section>
        </template>
        <footer class="mt-auto flex items-center gap-3.5 border-t border-default bg-elevated px-4 py-3.5">
          <b class="text-20 tabular-nums">{{ totals.jobs }}</b>
          <span class="text-12 text-muted">{{ jobsLine }}</span>
        </footer>
      </aside>
    </template>
  </USplitter>
</template>
