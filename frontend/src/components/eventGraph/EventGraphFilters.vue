<script setup lang="ts">
/**
 * The graph's Filters (the EventGraphFilters board): the kinds that become nodes, the origins whose nodes are
 * drawn, how many steps out and how many nodes. A change applies as it is picked; the button counts the
 * filters away from their defaults.
 */
import { computed } from "vue";

/** The scripted effects kind: a node only when the graph is asked to draw them. */
const SCRIPTED = "scripted effect";

const { kinds, origins, count } = defineProps<{
  /** Each kind drawn, with how many of it. */
  kinds: { kind: string; count: number }[];
  /** The game and each mod, with how many nodes each draws. */
  origins: { id: string; label: string; color: string; count: number }[];
  count: number;
}>();
const emit = defineEmits<{ reset: [] }>();
const hidden = defineModel<string[]>("hidden", { required: true });
const scripted = defineModel<boolean>("scripted", { required: true });
/** The origins drawn; none listed is every one. */
const shownOrigins = defineModel<string[] | null>("shownOrigins", { required: true });
const depth = defineModel<number>("depth", { required: true });
const maxNodes = defineModel<number>("maxNodes", { required: true });

// the scripted effects are listed whether drawn or not, since the graph draws them only when asked
const kindRows = computed(() => (kinds.some((row) => row.kind === SCRIPTED) ? kinds : [...kinds, { kind: SCRIPTED, count: 0 }]));
const kindShown = (kind: string) => (kind === SCRIPTED ? scripted.value : !hidden.value.includes(kind));
function showKind(kind: string, on: boolean): void {
  if (kind === SCRIPTED) scripted.value = on;
  else hidden.value = on ? hidden.value.filter((other) => other !== kind) : [...hidden.value, kind];
}

const drawn = computed(() => (shownOrigins.value?.length ? shownOrigins.value : origins.map((origin) => origin.id)));
function showOrigin(id: string, on: boolean): void {
  const next = on ? [...drawn.value, id] : drawn.value.filter((other) => other !== id);
  shownOrigins.value = next.length === origins.length ? [] : next;
}
</script>

<template>
  <UPopover :content="{ align: 'start' }">
    <UButton label="Filters" icon="i-lucide-filter" size="md">
      <template v-if="count" #trailing><UBadge :label="String(count)" size="sm" /></template>
    </UButton>
    <template #content>
      <div class="w-110">
        <section class="flex flex-col gap-2 border-b border-default px-3.5 py-3">
          <div class="flex items-baseline gap-2">
            <span class="text-12 font-semibold">Kinds</span>
            <span class="text-12 text-dimmed">Which kinds of object become nodes</span>
          </div>
          <div class="grid grid-cols-2 gap-x-5">
            <UCheckbox
              v-for="row in kindRows"
              :key="row.kind"
              :model-value="kindShown(row.kind)"
              :ui="{ root: 'h-6.5 items-center', wrapper: 'flex grow items-center', label: 'flex grow items-center text-12' }"
              @update:model-value="showKind(row.kind, $event === true)"
            >
              <template #label>
                {{ row.kind }}<span class="ms-auto text-11 text-dimmed tabular-nums">{{ row.count }}</span>
              </template>
            </UCheckbox>
          </div>
        </section>
        <section class="flex flex-col gap-2 border-b border-default px-3.5 py-3">
          <span class="text-12 font-semibold">Show nodes from</span>
          <p class="text-12 text-dimmed">Unchecked origins hide the nodes only they define.</p>
          <div class="flex flex-col">
            <UCheckbox
              v-for="origin in origins"
              :key="origin.id"
              :model-value="drawn.includes(origin.id)"
              :disabled="drawn.length === 1 && drawn.includes(origin.id)"
              :ui="{ root: 'h-6.5 items-center', wrapper: 'flex grow items-center gap-2', label: 'flex grow items-center gap-2 text-12' }"
              @update:model-value="showOrigin(origin.id, $event === true)"
            >
              <template #label>
                <OriginDot :color="origin.color" small />{{ origin.label }}
                <span class="ms-auto text-11 text-dimmed tabular-nums">{{ drawn.includes(origin.id) ? origin.count : "hidden" }}</span>
              </template>
            </UCheckbox>
          </div>
        </section>
        <section class="flex items-start gap-5 border-b border-default px-3.5 py-3">
          <UFormField label="Depth">
            <UInputNumber v-model="depth" :min="1" :max="10" orientation="horizontal" size="sm" class="w-24" />
          </UFormField>
          <UFormField label="Node limit">
            <UInputNumber v-model="maxNodes" :min="10" :max="400" :step="10" orientation="horizontal" size="sm" class="w-24" />
          </UFormField>
          <p class="pt-5.5 text-12 text-dimmed">Past six fires from one node, the rest fold into "+N more", which opens.</p>
        </section>
        <div class="flex items-center gap-2 bg-muted px-3.5 py-2.5">
          <UButton label="Reset to defaults" variant="ghost" @click="emit('reset')" />
          <span class="grow" />
          <span class="text-12 text-dimmed">Changes apply as you pick</span>
        </div>
      </div>
    </template>
  </UPopover>
</template>
