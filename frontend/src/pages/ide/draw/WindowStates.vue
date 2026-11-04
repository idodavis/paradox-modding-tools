<!--
  The States tab of a window's panel. It shows the conditions on the way down to the selected widget, with a Show it
  button, then the states that change what is drawn under it. The states are grouped by what they are about: the window
  itself, each object, what an object has, and the rows of each list.
-->
<script setup lang="ts">
import { useMutation, useQuery } from "@pinia/colada";
import { createReusableTemplate, refDebounced } from "@vueuse/core";
import { computed, shallowRef, watch } from "vue";

import * as draw from "@services/draw/service";
import type { Group, Window } from "@services/draw/models";

import { matchQuery, toSentence } from "../../../lib/format";
import { statesQuery } from "../../../queries";
import { useDrawStore } from "../../../stores/draw";
import { useWorkspaceStore } from "../../../stores/workspace";
import ScriptSnippet from "../../../ui/ScriptSnippet.vue";
import { findTypeIcon } from "./properties";

const { path, window } = defineProps<{ path: string; window: Window }>();

/** A Box is one card of the tab: the box Go gives a group, with the groups it shows. */
type Box = Group["box"] & { groups: Group[] };

// EVERY and NONE are Go's picks for every value of a group at once and for none.
const EVERY = "*every";
const NONE = "none";
// FIRST is the rows a box shows before More. SEGMENTS is the most values a segmented control holds before a select.
const FIRST = 6;
const SEGMENTS = 5;

const store = useDrawStore();
const workspace = useWorkspaceStore();
const [DefineControl, Control] = createReusableTemplate<{ group: Group }>();
const [DefineChip, Chip] = createReusableTemplate<{ group: Group; label: string }>();

const file = computed(() => store.file(path));
const widgets = computed(() => window.widgets ?? []);
const { data: states } = useQuery(() => ({
  ...statesQuery({ path, name: window.name, picks: store.picks }),
  enabled: workspace.isOpen(),
}));
const groups = computed(() => window.groups ?? []);
// The picked value of each group, by group id.
const values = computed(() => states.value?.values ?? {});
const hidden = computed(() => new Set(states.value?.hidden ?? []));
const changes = computed(() => groups.value.filter((group) => group.id in store.picks).map(({ id }) => id));

// The selected widget and its ancestors, the outermost first, without the window's own widget.
const chain = computed(() => {
  const ids: number[] = [];
  for (let id = file.value.selected ?? 0; id > 0; id = widgets.value[id].parent) ids.unshift(id);
  return ids;
});
const way = computed(() =>
  chain.value.flatMap((id) => {
    const conditions = window.conditions?.[`${id}`];
    const reads = groups.value.filter((group) => group.readers?.includes(id));
    return conditions ? [{ widget: widgets.value[id], conditions, reads, met: !hidden.value.has(id) }] : [];
  }),
);
const isHidden = computed(() => chain.value.some((id) => hidden.value.has(id)));

const search = shallowRef("");
const query = refDebounced(search, 150);
// changedOnly limits the boxes to the states changed from the window's opening, wherever they apply.
const changedOnly = shallowRef(false);
// The groups the search finds, the widest reach first. They change what is drawn under the selection, or anywhere if
// nothing is selected. A list of pictures is left out, as it is picked in Properties.
const candidates = computed(() => {
  const selected = file.value.selected ?? 0;
  const applies = (group: Group) =>
    changedOnly.value && changes.value.length
      ? group.id in store.picks
      : (states.value?.applies?.[group.id] ?? []).some((id) => isUnder(id, selected));
  return groups.value
    .filter((group) => !group.pictures && applies(group))
    .filter((group) => matchQuery(query.value, group.subject, group.key, group.label, group.id))
    .toSorted((first, second) => (second.reach ?? 0) - (first.reach ?? 0));
});
// The candidates in the boxes Go puts them in: the window's own first, then the objects' with their kinds merged and
// the widest reach first, then the rows of each list.
const boxes = computed<Box[]>(() => {
  const grouping = Map.groupBy(candidates.value, (group) => group.box.id);
  const shown = [...grouping.values()].map((members): Box => ({ ...members[0].box, groups: members }));
  return [
    ...shown.filter((box) => box.look === "window"),
    ...mergeKinds(shown.filter((box) => box.look !== "window" && !isList(box))),
    ...shown.filter(isList),
  ];
});

// The boxes folded or opened by hand (a list's rows start folded, the rest open), and the boxes showing all their rows.
const overrides = shallowRef(new Set<string>());
const wholeBoxes = shallowRef(new Set<string>());

const {
  mutate: reveal,
  data: revealed,
  reset,
} = useMutation({
  mutation: (id: number) => draw.Reveal(path, window.name, store.picks, id),
  onSuccess: (required) => store.pick(required ?? {}),
});
watch(() => file.value.selected, reset);

/**
 * mergeKinds merges boxes that ask the same thing of objects of one kind, told apart only by their key, into one box of
 * chips with a chip for each key, such as a character's relations to the player. Each such box holds one group of two
 * values at most. The result is sorted by reach.
 */
function mergeKinds(list: Box[]): Box[] {
  const isSingle = (box: Box) =>
    box.look === "rows" && box.groups.length === 1 && !!box.groups[0].key && listValues(box.groups[0]).length < 2;
  const kinds = Map.groupBy(list.filter(isSingle), (box) => `${box.groups[0].subject}|${box.groups[0].label}`);
  const families = [...kinds.values()].filter((kind) => kind.length > 1);
  const memberIds = new Set(families.flat().map(({ id }) => id));
  const kindBoxes = families.map((kind): Box => ({
    id: `kind:${kind[0].groups[0].subject}:${kind[0].groups[0].label}`,
    title: `${kind[0].groups[0].subject}: ${kind[0].groups[0].label}`,
    icon: "i-lucide-tags",
    look: "chips",
    groups: kind.flatMap((box) => box.groups),
  }));
  const reach = (box: Box) => box.groups.reduce((sum, group) => sum + (group.reach ?? 0), 0);
  return [...list.filter(({ id }) => !memberIds.has(id)), ...kindBoxes].toSorted(
    (first, second) => reach(second) - reach(first),
  );
}

/** isList reports whether a box holds the rows of a list. */
function isList(box: Box): boolean {
  return box.id.startsWith("item:");
}

/** isFolded reports whether a box is folded: a list's rows start folded, the rest open, and a fold flips that. */
function isFolded(box: Box): boolean {
  return isList(box) !== overrides.value.has(box.id);
}

/** fold folds a box that is open, and opens one that is folded. */
function fold(box: Box): void {
  const next = new Set(overrides.value);
  if (!next.delete(box.id)) next.add(box.id);
  overrides.value = next;
}

/** listRows lists the groups a box shows: its first rows, or every row once opened whole or while searching. */
function listRows(box: Box): Group[] {
  return wholeBoxes.value.has(box.id) || query.value ? box.groups : box.groups.slice(0, FIRST);
}

/** showWhole shows every row of a box. */
function showWhole(box: Box): void {
  wholeBoxes.value = new Set([...wholeBoxes.value, box.id]);
}

/** isUnder reports whether a widget is the given one or lies under it. */
function isUnder(id: number, holder: number): boolean {
  for (let widgetId = id; ; widgetId = widgets.value[widgetId].parent) {
    if (widgetId === holder) return true;
    if (widgetId === 0) return false;
  }
}

/** listValues lists the values a group can be picked at, without every value and none. */
function listValues(group: Group): string[] {
  return (group.options ?? []).filter((option) => option !== EVERY && option !== NONE);
}

/** toTitle converts a group's object to words: its kind and the key that picks it. */
function toTitle(group: Group): string {
  return [group.subject, group.key].filter(Boolean).join(" ");
}

/** listChoices lists a group's choices in two sections: the values the window asks of it, then all and none. */
function listChoices(group: Group): { label?: string; value: string }[][] {
  const toChoice = (value: string) => ({ label: group.captions?.[value], value });
  const lists = [listValues(group), [EVERY, NONE].filter((option) => group.options?.includes(option))];
  return lists.filter((list) => list.length).map((list) => list.map(toChoice));
}

/** isOn reports whether a group of one value is on: its picked value is not none. */
function isOn(group: Group): boolean {
  return values.value[group.id] !== NONE;
}

/** yesOption returns the option that Yes picks for a group of one value: its value, else every value. */
function yesOption(group: Group): string {
  return listValues(group)[0] ?? EVERY;
}

/** turn picks a group of one value on or off. */
function turn(group: Group, isEnabled: boolean): void {
  store.pick({ [group.id]: isEnabled ? yesOption(group) : NONE });
}

/** outline outlines the widgets a group decides, or none. */
function outline(group: Group | null): void {
  store.outline(path, group ? (states.value?.applies?.[group.id] ?? []) : []);
}
</script>

<template>
  <DefineControl v-slot="{ group }">
    <UFieldGroup
      v-if="listValues(group).length > 1 && listValues(group).length <= SEGMENTS && group.clickable"
      size="xs"
      class="shrink-0"
    >
      <UButton
        v-for="value in listValues(group)"
        :key="value"
        :label="group.captions?.[value]"
        :color="values[group.id] === value ? 'primary' : 'neutral'"
        :variant="values[group.id] === value ? 'soft' : 'outline'"
        @click="store.pick({ [group.id]: value })"
      />
    </UFieldGroup>
    <USelectMenu
      v-else-if="listValues(group).length > 1"
      :model-value="values[group.id]"
      :items="listChoices(group)"
      value-key="value"
      size="xs"
      class="w-40 shrink-0"
      @update:model-value="(option) => store.pick({ [group.id]: option })"
    />
    <UFieldGroup v-else size="xs" class="shrink-0">
      <UButton
        :label="group.captions?.[yesOption(group)]"
        :color="isOn(group) ? 'primary' : 'neutral'"
        :variant="isOn(group) ? 'soft' : 'outline'"
        @click="turn(group, true)"
      />
      <UButton
        :label="group.captions?.[NONE]"
        color="neutral"
        :variant="isOn(group) ? 'outline' : 'soft'"
        @click="turn(group, false)"
      />
    </UFieldGroup>
  </DefineControl>
  <DefineChip v-slot="{ group, label }">
    <UTooltip :text="group.id" :content="{ side: 'left' }">
      <span @mouseenter="outline(group)" @mouseleave="outline(null)">
        <UButton
          :label="label"
          :icon="isOn(group) ? 'i-lucide-check' : undefined"
          :color="isOn(group) ? 'primary' : 'neutral'"
          :variant="isOn(group) ? 'soft' : 'outline'"
          size="xs"
          class="rounded-full"
          @click="turn(group, !isOn(group))"
        />
      </span>
    </UTooltip>
  </DefineChip>

  <div class="flex min-h-0 flex-1 flex-col overflow-auto">
    <section v-if="way.length" class="flex shrink-0 flex-col gap-2 border-b border-default p-3">
      <div v-for="step in way" :key="step.widget.id" class="rounded-lg border border-default bg-default p-2">
        <div class="mb-1 flex items-center gap-1.5 text-12">
          <UIcon :name="findTypeIcon(step.widget.type)" class="size-3.5 text-dimmed" />
          <span class="truncate font-mono text-highlighted">{{ step.widget.name || step.widget.type }}</span>
          <span class="grow" />
          <UBadge
            :label="step.met ? 'Shown' : 'Hidden'"
            :color="step.met ? 'success' : 'warning'"
            variant="subtle"
            size="sm"
          />
        </div>
        <ScriptSnippet
          v-for="(condition, ordinal) in step.conditions"
          :key="ordinal"
          :text="condition"
          language="paradox-gui"
          class="mb-1"
        />
        <div
          v-for="group in step.reads"
          :key="group.id"
          class="flex min-h-8 items-center gap-2 text-13"
          @mouseenter="outline(group)"
          @mouseleave="outline(null)"
        >
          <span class="min-w-0 grow truncate text-toned"> {{ toTitle(group) }}: {{ group.label }} </span>
          <Control :group="group" />
        </div>
      </div>
      <div v-if="isHidden" class="flex items-center gap-2">
        <UButton
          label="Show it"
          icon="i-lucide-eye"
          size="xs"
          color="neutral"
          variant="outline"
          @click="reveal(chain.at(-1) ?? 0)"
        />
        <span v-if="revealed === null" class="text-12 text-(--orange)">
          Still hidden: its conditions contradict each other.
        </span>
      </div>
    </section>
    <div class="mx-3 mt-3 mb-2 flex items-center gap-2">
      <UInput v-model="search" icon="i-lucide-search" size="sm" placeholder="Find a state" class="grow" />
      <UButton
        v-if="changes.length"
        :label="`${changes.length} changed`"
        icon="i-lucide-list-filter"
        :color="changedOnly ? 'primary' : 'neutral'"
        :variant="changedOnly ? 'soft' : 'outline'"
        size="sm"
        @click="changedOnly = !changedOnly"
      />
      <UButton
        label="Reset"
        icon="i-lucide-rotate-ccw"
        color="neutral"
        variant="outline"
        size="sm"
        :disabled="!changes.length"
        @click="store.unpick(changes)"
      />
    </div>
    <div
      v-for="box in boxes"
      :key="box.id"
      class="mx-3 mb-2.5 shrink-0 overflow-hidden rounded-lg border border-default bg-default"
    >
      <button
        type="button"
        class="flex h-9 w-full items-center gap-2 px-3 text-12 font-semibold text-highlighted"
        @click="fold(box)"
      >
        <UIcon :name="box.icon" class="size-3.5 text-dimmed" />
        <span class="min-w-0 truncate">{{ box.title }}</span>
        <span class="grow" />
        <span class="text-11 font-normal text-dimmed">{{ box.groups.length }}</span>
        <UIcon
          :name="isFolded(box) ? 'i-lucide-chevron-right' : 'i-lucide-chevron-down'"
          class="size-3.5 text-dimmed"
        />
      </button>
      <template v-if="!isFolded(box)">
        <div v-if="box.look === 'chips'" class="flex flex-wrap gap-1.5 px-3 pb-3">
          <Chip
            v-for="group in box.groups"
            :key="group.id"
            :group="group"
            :label="box.id.startsWith('kind:') ? (group.key ?? '') : toSentence(group.label)"
          />
        </div>
        <template v-else>
          <template v-if="box.look === 'window'">
            <div
              v-for="[title, list] in Map.groupBy(
                box.groups.filter((row) => listValues(row).length < 2),
                toTitle,
              )"
              :key="title"
              class="flex flex-wrap items-center gap-1.5 px-3 pb-2"
            >
              <span class="me-1 text-11 text-dimmed">{{ title }}</span>
              <Chip v-for="group in list" :key="group.id" :group="group" :label="toSentence(group.label)" />
            </div>
          </template>
          <div
            v-for="group in listRows(box).filter((row) => box.look === 'rows' || listValues(row).length > 1)"
            :key="group.id"
            class="flex min-h-8 items-center gap-2 px-3 text-13 hover:bg-elevated"
            @mouseenter="outline(group)"
            @mouseleave="outline(null)"
          >
            <span class="size-1.5 shrink-0 rounded-full" :class="group.id in store.picks ? 'bg-primary' : ''" />
            <UTooltip :text="group.id" :content="{ side: 'left' }">
              <span class="min-w-0 grow truncate text-toned">{{ toSentence(group.label) }}</span>
            </UTooltip>
            <Control :group="group" />
          </div>
          <button
            v-if="box.look === 'rows' && box.groups.length > FIRST && !wholeBoxes.has(box.id) && !query"
            type="button"
            class="flex h-8 w-full items-center gap-1.5 border-t border-default px-3 text-12 text-dimmed hover:text-toned"
            @click="showWhole(box)"
          >
            <UIcon name="i-lucide-chevron-down" class="size-3.5" />{{ box.groups.length - FIRST }} more
          </button>
        </template>
      </template>
    </div>
    <UEmpty
      v-if="!boxes.length"
      icon="i-lucide-braces"
      title="No state changes what is drawn here"
      variant="naked"
      size="sm"
      class="py-6"
    />
  </div>
</template>
