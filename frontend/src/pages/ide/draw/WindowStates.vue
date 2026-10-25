<!--
  The States tab of a window's panel: the conditions on the way down to the selected widget, with Show it, then the
  states that change what is drawn under it, sorted by who they are about: the window's own, each object's, what an
  object has, and the rows of each list.
-->
<script setup lang="ts">
import { useMutation, useQuery } from "@pinia/colada";
import { createReusableTemplate, refDebounced } from "@vueuse/core";
import { computed, shallowRef, watch } from "vue";

import * as draw from "@services/draw/service";
import type { Group, Window } from "@services/draw/models";

import { toSentence } from "../../../lib/format";
import { statesQuery } from "../../../queries";
import { useDrawStore } from "../../../stores/draw";
import { useWorkspaceStore } from "../../../stores/workspace";
import ScriptSnippet from "../../../ui/ScriptSnippet.vue";
import { TYPE_ICONS } from "./picture";

const { path, window } = defineProps<{ path: string; window: Window }>();

/** A box of the tab: its groups, shown as rows with controls, as chips, or as the window's own. */
type Box = { id: string; title: string; icon: string; look: "rows" | "chips" | "window"; groups: Group[] };

// Go's picks for every value of a group at once and for none
const EVERY = "*every";
const NONE = "none";
const LABELS: Record<string, string> = { [EVERY]: "All", [NONE]: "None" };
// the rows a box shows before More, and the most values a segmented control holds before it is a select
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
const groups = computed(() => states.value?.groups ?? []);
const hidden = computed(() => new Set(states.value?.hidden ?? []));
const changes = computed(() => groups.value.filter((group) => group.id in store.picks).map(({ id }) => id));

// the selected widget and the widgets that hold it, the window's own left out
const chain = computed(() => {
  const ids: number[] = [];
  for (let id = file.value.selected ?? 0; id > 0; id = widgets.value[id].parent) ids.unshift(id);
  return ids;
});
const way = computed(() =>
  chain.value.flatMap((id) => {
    const conditions = states.value?.conditions?.[`${id}`];
    const reads = groups.value.filter((group) => group.readers?.includes(id));
    return conditions ? [{ widget: widgets.value[id], conditions, reads, met: !hidden.value.has(id) }] : [];
  }),
);
const isHidden = computed(() => chain.value.some((id) => hidden.value.has(id)));

const search = shallowRef("");
const query = refDebounced(search, 150);
// whether the boxes hold only the states changed from the window's opening, wherever they apply
const changedOnly = shallowRef(false);
// the groups that change what is drawn under the selection, or anywhere with none, that the search finds, by reach; a
// list of pictures is picked in Properties
const candidates = computed(() => {
  const selected = file.value.selected ?? 0;
  const needle = query.value.toLowerCase();
  const applies = (group: Group) =>
    changedOnly.value && changes.value.length
      ? group.id in store.picks
      : (states.value?.applies?.[group.id] ?? []).some((id) => isUnder(id, selected));
  return groups.value
    .filter((group) => !group.pictures && applies(group))
    .filter((group) =>
      [group.subject, group.key, group.label, group.id].some((text) => text?.toLowerCase().includes(needle)),
    )
    .toSorted((first, second) => (second.reach ?? 0) - (first.reach ?? 0));
});
const boxes = computed<Box[]>(() => {
  const own = candidates.value.filter((group) => !group.item);
  const windowBox: Box = {
    id: "window",
    title: "This window",
    icon: "i-lucide-app-window",
    look: "window",
    groups: own.filter((group) => group.clickable),
  };
  const objects = Map.groupBy(
    own.filter((group) => !group.clickable),
    (group) => `${group.subject}|${group.key ?? ""}|${group.exists ? "has" : ""}`,
  );
  const objectBoxes = [...objects.values()].map((list): Box => {
    const [first] = list;
    const title = toTitle(first);
    return first.exists
      ? { id: `has:${title}`, title: `${title} has`, icon: "i-lucide-check-check", look: "chips", groups: list }
      : { id: `object:${title}`, title, icon: "i-lucide-circle-dot", look: "rows", groups: list };
  });
  const rows = Map.groupBy(
    candidates.value.filter((group) => group.item),
    (group) => group.item ?? 0,
  );
  const rowBoxes = [...rows].map(([item, list]): Box => ({
    id: `item:${item}`,
    title: `Rows of ${widgets.value[item].name || widgets.value[item].type}`,
    icon: "i-lucide-list",
    look: "rows",
    groups: list,
  }));
  return [windowBox, ...mergeKinds(objectBoxes), ...rowBoxes].filter((box) => box.groups.length);
});

// the boxes folded or opened by hand, and the boxes showing all their rows: a box starts open, a list's rows folded
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
 * Merges the boxes of objects of one kind told apart only by their key into one box of chips, one chip a key, where
 * each asks one thing of two values, the same thing: the relations a character has to the player.
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

/** Whether a box is folded: a list's rows start folded, the rest open, and a fold by hand flips that. */
function isFolded(box: Box): boolean {
  return box.id.startsWith("item:") !== overrides.value.has(box.id);
}

/** Folds a box, or opens it. */
function fold(box: Box): void {
  const next = new Set(overrides.value);
  if (!next.delete(box.id)) next.add(box.id);
  overrides.value = next;
}

/** Lists the groups a box shows: its first rows, or every row once opened whole or while searching. */
function listRows(box: Box): Group[] {
  return wholeBoxes.value.has(box.id) || query.value ? box.groups : box.groups.slice(0, FIRST);
}

/** Shows every row of a box. */
function showWhole(box: Box): void {
  wholeBoxes.value = new Set([...wholeBoxes.value, box.id]);
}

/** Whether a widget is the given one or lies under it. */
function isUnder(id: number, holder: number): boolean {
  for (let at = id; ; at = widgets.value[at].parent) {
    if (at === holder) return true;
    if (at === 0) return false;
  }
}

/** Lists the values a group can be picked at, without every value and none. */
function listValues(group: Group): string[] {
  return (group.options ?? []).filter((option) => option !== EVERY && option !== NONE);
}

/**
 * Converts a value to how the panel writes it: the text of the button that picks it, else the value without its type
 * as the start of a sentence.
 */
function toCaption(group: Group, value: string): string {
  return group.captions?.[value] ?? LABELS[value] ?? toSentence(value.replace(/^\(\w+\)/, ""));
}

/** Converts a group's object to words: its kind and the key that picks it. */
function toTitle(group: Group): string {
  return [group.subject, group.key].filter(Boolean).join(" ");
}

/** Lists a group's choices in two sections: the values the window asks of it, then all and none. */
function listChoices(group: Group): { label: string; value: string }[][] {
  const toChoice = (value: string) => ({ label: toCaption(group, value), value });
  const lists = [listValues(group), [EVERY, NONE].filter((option) => group.options?.includes(option))];
  return lists.filter((list) => list.length).map((list) => list.map(toChoice));
}

/** Whether a group of one value holds: it is at that value or at every value. */
function isOn(group: Group): boolean {
  return group.picked !== NONE;
}

/** Picks a group of one value on or off. */
function turn(group: Group, on: boolean): void {
  store.pick({ [group.id]: on ? (listValues(group)[0] ?? EVERY) : NONE });
}

/** Outlines the widgets a group decides, or none. */
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
        :label="toCaption(group, value)"
        :color="group.picked === value ? 'primary' : 'neutral'"
        :variant="group.picked === value ? 'soft' : 'outline'"
        @click="store.pick({ [group.id]: value })"
      />
    </UFieldGroup>
    <USelectMenu
      v-else-if="listValues(group).length > 1"
      :model-value="group.picked"
      :items="listChoices(group)"
      value-key="value"
      size="xs"
      class="w-40 shrink-0"
      @update:model-value="(option) => store.pick({ [group.id]: option })"
    />
    <UFieldGroup v-else size="xs" class="shrink-0">
      <UButton
        label="Yes"
        :color="isOn(group) ? 'primary' : 'neutral'"
        :variant="isOn(group) ? 'soft' : 'outline'"
        @click="turn(group, true)"
      />
      <UButton label="No" color="neutral" :variant="isOn(group) ? 'outline' : 'soft'" @click="turn(group, false)" />
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
          <UIcon :name="TYPE_ICONS[step.widget.type] ?? 'i-lucide-square-dashed'" class="size-3.5 text-dimmed" />
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
          class="mb-1 text-11.5"
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
                box.groups.filter((one) => listValues(one).length < 2),
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
            v-for="group in listRows(box).filter((one) => box.look === 'rows' || listValues(one).length > 1)"
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
