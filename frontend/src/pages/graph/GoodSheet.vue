<!--
  The view of a good as a sheet: its head with where it is found and what it
  costs, how it is made on one side and what uses it on the other.
-->
<script setup lang="ts">
import { computed, reactive } from "vue";
import { useQuery } from "@pinia/colada";
import { partition, uniqBy } from "es-toolkit";
import { useRouter } from "vue-router";
import type { Filter, Line, LineRow, Pile, User } from "@services/graph/models";
import { describeCount, plural, toRoman } from "../../lib/format";
import { detailQuery, graphChainQuery, graphErasQuery } from "../../queries";
import { useWorkspaceStore } from "../../stores/workspace";
import LineTable from "./LineTable.vue";
import { conditionOf, keyOf, labelOf, NO_REACH, SAYS } from "./nodes";
import NodeIcon from "./NodeIcon.vue";
import OriginsMenu from "./OriginsMenu.vue";
import SelectedTab from "./SelectedTab.vue";
import SubjectHead from "./SubjectHead.vue";

const {
  subject,
  defaults,
  conditions = "",
  selected = "",
} = defineProps<{
  /** The id of the good. */
  subject: string;
  /** The filter of a new graph, whose origins are all the workspace's. */
  defaults: Filter;
  /** Changes to the filter, as JSON from the route. */
  conditions?: string;
  /** The id of the selected node, as the route names it. */
  selected?: string;
}>();
const emit = defineEmits<{ ask: [id: string] }>();

/** WAYS is the word for how a line makes the good, by what its row says. */
const WAYS: Record<string, string> = { output: "produces", boost: "boosts", gives: "gives" };
/** ERA_ANY stands for no era in the menu, which holds no empty choice. */
const ERA_ANY = "any";
/** FIRST_LISTED is how many lines an open group lists before Show all. */
const FIRST_LISTED = 12;
const CARD = "flex min-w-0 flex-col gap-3 rounded-xl border border-default bg-default p-4";
const HEADING = "flex flex-col gap-0.5 text-14 font-semibold text-highlighted";
const PICKABLE =
  "relative cursor-pointer text-start hover:bg-elevated data-selected:bg-(--accent-soft) data-selected:ring-1 data-selected:ring-primary";

const router = useRouter();
const workspace = useWorkspaceStore();
const openPiles = reactive(new Set<string>());
const fullPiles = reactive(new Set<string>());

const edits = computed<Partial<Filter>>(() => (conditions ? JSON.parse(conditions) : {}));
const filter = computed<Filter>(() => ({
  ...NO_REACH,
  origins: edits.value.origins ?? defaults.origins,
  changed: edits.value.changed,
  era: edits.value.era,
}));
const { data: chain, error } = useQuery(() => ({
  ...graphChainQuery({ id: subject, filter: filter.value }),
  enabled: workspace.isOpen(),
  placeholderData: (previous) => previous,
}));
const { data: detail } = useQuery(() => ({ ...detailQuery(subject), enabled: workspace.isOpen() }));
const { data: eras } = useQuery(() => ({ ...graphErasQuery, enabled: workspace.isOpen() }));

const era = computed({
  get: () => edits.value.era || ERA_ANY,
  set: (id: string) => update({ era: id === ERA_ANY ? undefined : id }),
});
const origins = computed({
  get: () => filter.value.origins ?? [],
  set: (selection: string[]) => update({ origins: selection }),
});
const eraChoices = computed(() => [
  { label: `Any ${eras.value?.[0]?.what}`, value: ERA_ANY },
  ...(eras.value ?? []).map((definition) => ({ label: labelOf(definition), value: definition.id })),
]);
/** The goods a category holds, none for a good. */
const members = computed(() =>
  (detail.value?.links ?? []).flatMap((edge) => {
    const member = edge.relation === "group" && edge.to === subject ? detail.value?.linked?.[edge.from] : undefined;

    return member ? [member] : [];
  }),
);
/**
 * The lines that make the good, each once: first those of several levels
 * that take goods, drawn as tables, then the rest, drawn as rows.
 */
const makers = computed(() => {
  const byLine = Map.groupBy(chain.value?.makers ?? [], (maker) => maker.line.head.id);
  const lines = [...byLine.values()].map((ways): Line => {
    const rows = ways.flatMap((maker) => [...(maker.line.rows ?? []), ...(maker.line.same ?? [])]);

    return { ...ways[0].line, rows: uniqBy(rows, keyOf), same: [] };
  });

  return partition(lines, (line) => (line.levels?.length ?? 0) > 1 && listTaken(line).length > 0);
});

/** listTaken returns the rows of the goods a line's levels take. */
function listTaken(line: Line): LineRow[] {
  return (line.rows ?? []).filter((row) => row.says === "input");
}

/**
 * listMade returns how a line makes the good: each amount as it runs from
 * the first level to the last, with the way and the condition in words.
 */
function listMade(rows: LineRow[]) {
  return rows.flatMap((row) => {
    const texts = (row.cells ?? []).flatMap((cell) => cell.text || []);
    const [first, last] = [texts[0], texts[texts.length - 1]];
    if (!(row.says in WAYS) || !first) return [];

    const condition = conditionOf(rows, row);

    return {
      key: keyOf(row),
      text: first === last ? first : `${first} to ${last}`,
      tone: SAYS[row.says].tone,
      note: [WAYS[row.says], condition].filter(Boolean).join(condition.startsWith("when") ? " " : " · "),
    };
  });
}

/** titleOf names a line by its first and last level: "Guild to Mill". */
function titleOf(line: Line): string {
  const levels = line.levels ?? [];

  return levels.length > 1
    ? `${labelOf(levels[0].level)} to ${labelOf(levels[levels.length - 1].level)}`
    : labelOf(line.head);
}

function describeLine(line: Line): string {
  const count = line.levels?.length ?? 0;

  return [line.group.id && labelOf(line.group), count > 1 && plural(count, "level")].filter(Boolean).join(" · ");
}

function isPicked(line: Line): boolean {
  return !!line.levels?.some((level) => level.level.id === selected);
}

function listUsers(pile: Pile): User[] {
  return fullPiles.has(pile.group.id) ? (pile.users ?? []) : (pile.users ?? []).slice(0, FIRST_LISTED);
}

/** describeUser says which levels of a line take the good, and as what. */
function describeUser(user: User): string {
  const levels =
    user.firstLevel === user.lastLevel
      ? `level ${toRoman(user.firstLevel)}`
      : `levels ${toRoman(user.firstLevel)} to ${toRoman(user.lastLevel)}`;

  return [user.levelCount > 1 && levels, user.through && `as ${labelOf(user.through)}`].filter(Boolean).join(", ");
}

/**
 * replaceRoute replaces the route's query: the subject, the selected node and
 * the conditions, with the given changes. A part left empty is left out.
 */
function replaceRoute(change: { node?: string; conditions?: string }): void {
  const query = { subject, node: selected, conditions, ...change };
  void router.replace({ query: Object.fromEntries(Object.entries(query).filter(([, text]) => text)) });
}

function select(id: string): void {
  replaceRoute({ node: id });
}

/** update changes parts of the filter, which the route keeps as changes. */
function update(change: Partial<Filter>): void {
  replaceRoute({ conditions: JSON.stringify({ ...edits.value, ...change }) });
}

function toggle(id: string): void {
  if (!openPiles.delete(id)) openPiles.add(id);
}
</script>

<template>
  <div class="flex h-full min-h-0 min-w-0 flex-col bg-(--bg-0)">
    <div class="flex h-10 shrink-0 items-center justify-end gap-2 border-b border-default bg-muted px-3">
      <label v-if="eras?.length" class="flex items-center gap-2 text-12 text-dimmed">
        Available by
        <USelect v-model="era" :items="eraChoices" size="xs" class="w-40" />
      </label>
      <OriginsMenu v-model="origins" :origins="defaults.origins ?? []">
        <USwitch
          label="Only what my mods add or change"
          color="secondary"
          size="sm"
          :model-value="!!filter.changed"
          @update:model-value="update({ changed: $event })"
        />
      </OriginsMenu>
    </div>
    <UAlert v-if="error" color="error" variant="subtle" :description="error.message" class="m-2" />
    <div v-if="chain" class="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-5 py-4">
      <SubjectHead :node="chain.good" :group="detail?.linked?.[chain.good.group ?? '']" :facts="detail?.about ?? []">
        <UButton
          v-for="member in members"
          :key="member.id"
          :label="labelOf(member)"
          color="neutral"
          variant="subtle"
          size="xs"
          @click="emit('ask', member.id)"
        >
          <template #leading><NodeIcon :node="member" class="size-4" /></template>
        </UButton>
      </SubjectHead>

      <div class="grid shrink-0 grid-cols-[repeat(auto-fit,minmax(24rem,1fr))] items-start gap-4">
        <section :class="CARD">
          <h2 :class="HEADING">
            How it's made
            <small class="text-12 font-normal text-dimmed">
              {{ chain.lines ? `In ${plural(chain.lines, "building line")}` : "No building makes it" }}
            </small>
          </h2>
          <div v-if="makers[0].length" class="flex flex-wrap items-start gap-3">
            <article
              v-for="line in makers[0]"
              :key="line.head.id"
              class="flex max-w-full min-w-0 flex-col gap-2.5 rounded-xl border border-default bg-muted/40 p-3"
            >
              <header class="flex items-center gap-3">
                <NodeIcon :node="line.head" class="size-8" />
                <span class="flex min-w-0 grow flex-col">
                  <b class="truncate text-13.5 font-medium text-highlighted">{{ titleOf(line) }}</b>
                  <small class="truncate text-12 text-dimmed">{{ describeLine(line) }}</small>
                </span>
                <UButton
                  icon="i-lucide-chevron-right"
                  color="neutral"
                  variant="outline"
                  size="xs"
                  square
                  :aria-label="`Open ${labelOf(line.head)}`"
                  @click="emit('ask', line.head.id)"
                />
              </header>
              <LineTable
                :line="line"
                :rows="line.rows ?? []"
                :selected="selected"
                @select="select"
                @ask="emit('ask', $event)"
              />
            </article>
          </div>
          <div v-if="makers[1].length" class="flex flex-col divide-y divide-default rounded-xl border border-default">
            <div
              v-for="line in makers[1]"
              :key="line.head.id"
              role="button"
              tabindex="0"
              :data-selected="isPicked(line) || undefined"
              :class="PICKABLE"
              class="flex min-h-13 items-center gap-3 px-3 py-2 first:rounded-t-xl last:rounded-b-xl"
              @click="select(line.head.id)"
              @dblclick="emit('ask', line.head.id)"
              @keydown.enter="select(line.head.id)"
            >
              <SelectedTab v-if="isPicked(line)" inside />
              <NodeIcon :node="line.head" class="size-8" />
              <span class="flex min-w-0 grow flex-col gap-0.5">
                <b class="truncate text-13.5 font-medium text-highlighted">{{ labelOf(line.head) }}</b>
                <small class="truncate text-12 text-dimmed">{{ describeLine(line) }}</small>
                <span v-if="listTaken(line).length" class="flex flex-wrap items-center gap-x-2.5 gap-y-0.5 text-12">
                  <span class="text-dimmed">Takes</span>
                  <span v-for="row in listTaken(line)" :key="keyOf(row)" class="flex items-center gap-1 text-muted">
                    <NodeIcon v-if="row.subject" :node="row.subject" class="size-4" />{{ row.label }}
                    <b class="font-medium text-(--blue) tabular-nums">{{ row.cells?.[0]?.text }}</b>
                  </span>
                </span>
              </span>
              <span
                v-for="amount in listMade(line.rows ?? [])"
                :key="amount.key"
                class="flex max-w-56 shrink-0 flex-col items-end text-end"
              >
                <b class="text-15 font-semibold whitespace-nowrap tabular-nums" :class="amount.tone">
                  {{ amount.text }}
                </b>
                <small class="text-11.5 leading-3.5 text-dimmed">{{ amount.note }}</small>
              </span>
              <UButton
                icon="i-lucide-chevron-right"
                color="neutral"
                variant="ghost"
                size="xs"
                square
                :aria-label="`Open ${labelOf(line.head)}`"
                @click.stop="emit('ask', line.head.id)"
              />
            </div>
          </div>
        </section>

        <section :class="CARD">
          <h2 :class="HEADING">
            What uses it
            <small class="text-12 font-normal text-dimmed">
              {{
                chain.users
                  ? `${describeCount(chain.users)} buildings and needs, in ${plural(chain.piles?.length ?? 0, "group")}`
                  : "Nothing takes it"
              }}
            </small>
          </h2>
          <div v-if="chain.piles?.length" class="flex flex-col divide-y divide-default">
            <div v-for="pile in chain.piles" :key="pile.group.id" class="flex flex-col">
              <button
                type="button"
                class="flex min-h-12 cursor-pointer items-center gap-3 rounded-md px-1.5 text-start hover:bg-elevated"
                :aria-expanded="openPiles.has(pile.group.id)"
                @click="toggle(pile.group.id)"
              >
                <NodeIcon :node="pile.group" class="size-8" />
                <span class="flex min-w-0 grow flex-col">
                  <b class="truncate text-13.5 font-medium text-highlighted">
                    {{ pile.group.id ? labelOf(pile.group) : "Needs with no building" }}
                  </b>
                  <small class="text-12 text-dimmed">
                    {{ plural(pile.group.count, pile.group.id ? "building" : "need") }}
                  </small>
                </span>
                <UIcon
                  :name="openPiles.has(pile.group.id) ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'"
                  class="size-4 shrink-0 text-dimmed"
                />
              </button>
              <div
                v-if="openPiles.has(pile.group.id)"
                class="mb-2.5 ms-11 flex flex-col divide-y divide-default rounded-lg border border-default bg-muted/40"
              >
                <button
                  v-for="user in listUsers(pile)"
                  :key="user.line.id"
                  type="button"
                  :data-selected="user.line.id === selected || undefined"
                  :class="PICKABLE"
                  class="flex min-h-8 items-center gap-2 px-3 py-1 text-12.5 first:rounded-t-lg last:rounded-b-lg"
                  @click="select(user.line.id)"
                  @dblclick="emit('ask', user.line.id)"
                >
                  <NodeIcon :node="user.line" class="size-5" />
                  <span class="truncate text-highlighted">{{ labelOf(user.line) }}</span>
                  <small class="shrink-0 text-11.5 text-dimmed">{{ describeUser(user) }}</small>
                  <b class="grow text-end font-semibold whitespace-nowrap text-(--blue) tabular-nums">
                    {{ user.amount }}
                  </b>
                </button>
                <UButton
                  v-if="pile.group.count > FIRST_LISTED && !fullPiles.has(pile.group.id)"
                  :label="`Show all ${describeCount(pile.group.count)}`"
                  color="neutral"
                  variant="link"
                  size="xs"
                  class="self-start px-3 py-1.5"
                  @click="fullPiles.add(pile.group.id)"
                />
              </div>
            </div>
          </div>
        </section>
      </div>
    </div>
    <p v-else-if="!error" class="grid min-h-0 flex-1 place-items-center text-13 text-dimmed">Reading the good</p>
  </div>
</template>
