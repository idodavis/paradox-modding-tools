<script setup lang="ts">
/**
 * The panel of a drawn file, named by its kind's row: under the file's header (a coat's with what copies, pastes or
 * edits a copy of it), a coat's Layers (CoatLayers.vue), or a GUI file's Widgets: the way down to the selected widget
 * as chips (the one the pointer is on is the picture's hovered widget, outlined there); the widget, a link to its code
 * and whether it is shown for this session; the condition it is shown on, with the state each group it reads is drawn
 * at; and its properties as written, grouped by who wrote them, each a link to where it is written. The selection is
 * the picture's or the caret's.
 */
import { computed, onScopeDispose } from "vue";
import { useQuery } from "@pinia/colada";
import type { BreadcrumbItem } from "@nuxt/ui";
import type { Location } from "@services/internal/lsp";
import { pathKey, useIdeRoots } from "../composables/useIdeRoots";
import { viewsOf } from "../editor/tabs";
import { fileName } from "../lib/format";
import { stepHistory } from "../lib/history";
import { propertiesQuery } from "../queries";
import { useDraw, useDrawStore } from "../stores/draw";
import DetailHeader from "../ui/DetailHeader.vue";
import ScriptSnippet from "../ui/ScriptSnippet.vue";
import CoatLayers from "./CoatLayers.vue";
import { isEditable } from "./coat";
import { choicesOf, groupsOf, stateOf, typeIcon } from "./widgets";

const { workspaceId, path } = defineProps<{ workspaceId: string; path: string }>();

/** A widget on the way to the selected one: its key as the label, a name of its own ("" for none), its element. */
type Crumb = BreadcrumbItem & { name: string; element: number };

const store = useDrawStore();
const { locate } = useIdeRoots(() => workspaceId);
const root = computed(() => locate(path)?.root);
const { file, request, drawing, list, elements, render } = useDraw(
  () => workspaceId,
  () => path,
);
// a coat the open file writes is pasted over, any other copied into a mod
const isCoatEditable = computed(
  () => !!elements.value[0] && isEditable(path, file.value.entry, elements.value, elements.value[0].location),
);

// the selected element and the way down to it, root first
const selection = computed(() => file.value.selection);
const element = computed(() => list.value?.elements?.[selection.value]);
const crumbs = computed(() =>
  [...(element.value?.ancestors ?? []), selection.value].flatMap((step): Crumb[] => {
    const crumb = elements.value[step];
    if (!crumb) return [];
    return [
      { label: crumb.key, name: crumb.name !== crumb.key ? crumb.name : "", icon: typeIcon(crumb.type), element: step },
    ];
  }),
);

/** Makes a step of the path the picture's hovered widget; -1 for none. */
const point = (step: number) => (file.value.hover = step);

/** A chip's look: the selected widget's in the accent, the hovered one's in the link color, any other plain. */
function chipClass(index: number): string {
  if (index === selection.value) return "border-primary/45 bg-(--accent-soft) text-primary";
  if (index === file.value.hover) return "border-secondary/45 bg-(--link-soft) text-secondary";
  return "border-transparent text-dimmed hover:bg-accented";
}

// a panel closed while a chip is pointed at leaves no mark on the picture
onScopeDispose(() => point(-1));

// whether the selected element itself is hidden for the session (one under a hidden element is drawn with it again)
const isShown = computed(() => !file.value.hidden.has(selection.value));

// the groups the selected element's condition reads, each with the value it is drawn at and its choices
const conditionGroups = computed(() => {
  const condition = element.value?.condition;
  if (!condition) return [];
  return groupsOf(condition).flatMap((index) => {
    const group = list.value?.groups?.[index];
    return group ? [{ call: group.call, value: stateOf(file.value, group), items: choicesOf(group) }] : [];
  });
});

const { data: properties } = useQuery(() =>
  propertiesQuery({
    id: workspaceId,
    request: request.value,
    render: render.value,
    element: drawing.value === "gui" && element.value ? selection.value : -1,
  }),
);

// the properties by who wrote them, the widget's own first, then each writer as it first appears (the sort is
// stable); a writer's location is where it writes its first row
const writers = computed(() =>
  [...Map.groupBy(properties.value ?? [], (property) => property.writer)]
    .sort(([first], [second]) => Number(first !== "") - Number(second !== ""))
    .map(([writer, rows]) => ({ writer, rows, location: rows[0].location })),
);

/** A location as a card's link writes it: the line alone in this file, else the file and the line. */
function linkText(location: Location): string {
  const line = location.range.start.line + 1;
  return pathKey(location.path) === pathKey(path) ? `L${line}` : `${location.rel}:${line}`;
}
</script>

<template>
  <aside
    tabindex="-1"
    class="flex min-h-0 min-w-0 flex-col border-s border-default bg-muted text-(length:--ide-ui-font) outline-none"
    @keydown="stepHistory($event, store.historyOf(path))"
  >
    <DetailHeader :kind="viewsOf(path)?.label ?? ''" :origin="root?.origin ?? ''" :name="fileName(path)">
      <template v-if="drawing === 'coat' && elements[0]" #actions>
        <UTooltip text="Copy this coat's script for the game's designer">
          <UButton
            icon="i-lucide-copy"
            aria-label="Copy for the game"
            variant="ghost"
            color="neutral"
            size="xs"
            square
            @click="store.copyCoat(elements[0])"
          />
        </UTooltip>
        <UTooltip v-if="isCoatEditable" text="Replace this coat with the game designer's copy">
          <UButton
            icon="i-lucide-clipboard-paste"
            aria-label="Paste from the game"
            variant="ghost"
            color="neutral"
            size="xs"
            square
            @click="store.pasteCoat(path, elements[0])"
          />
        </UTooltip>
        <UButton
          v-else
          label="Edit a copy"
          icon="i-lucide-copy-plus"
          size="xs"
          @click="store.copyToMod(path, elements[0])"
        />
        <UTooltip v-if="isCoatEditable" text="Delete this coat from the file">
          <UButton
            icon="i-lucide-trash-2"
            aria-label="Delete the coat"
            variant="ghost"
            color="error"
            size="xs"
            square
            @click="store.removeBlock(path, elements[0].location)"
          />
        </UTooltip>
      </template>
    </DetailHeader>
    <CoatLayers v-if="drawing === 'coat'" :workspace-id="workspaceId" :path="path" />
    <template v-else-if="element && list">
      <!-- the way down to the selected widget: the chip the pointer is on outlines its widget -->
      <div class="mx-3 mt-3 rounded-lg border border-default bg-default p-1.25" @mouseleave="point(-1)">
        <UBreadcrumb
          :items="crumbs"
          color="neutral"
          :ui="{ list: 'flex-wrap gap-x-px gap-y-1', separatorIcon: 'size-3 text-dimmed' }"
        >
          <template #item="{ item }">
            <button
              type="button"
              class="flex h-6 max-w-full min-w-0 items-center gap-1.25 rounded-sm border ps-1.25 pe-1.75 font-mono text-11.5 font-normal whitespace-nowrap"
              :class="chipClass(item.element)"
              :title="item.name ? `${item.label} ${item.name}` : item.label"
              @mouseenter="point(item.element)"
              @focus="point(item.element)"
              @blur="point(-1)"
              @click="file.selection = item.element"
            >
              <UIcon :name="item.icon ?? ''" class="size-3.25 shrink-0" />
              <span class="truncate text-highlighted">{{ item.label }}</span>
              <span v-if="item.name" class="truncate text-dimmed">{{ item.name }}</span>
            </button>
          </template>
        </UBreadcrumb>
      </div>
      <div class="mt-3 flex h-10 shrink-0 items-center gap-2 border-y border-default px-3">
        <span class="min-w-0 truncate font-mono text-13 font-semibold text-highlighted">{{ element.label }}</span>
        <span class="shrink-0 text-11.5 text-dimmed">{{ element.type }}</span>
        <span class="grow" />
        <UTooltip text="Put the caret where this widget is written">
          <UButton
            label="Go to code"
            trailing-icon="i-lucide-arrow-up-right"
            variant="ghost"
            color="neutral"
            size="xs"
            class="shrink-0"
            @click="store.goTo(path, element.location)"
          />
        </UTooltip>
        <USwitch
          :model-value="isShown"
          size="xs"
          :label="isShown ? 'Shown' : 'Hidden'"
          :ui="{ label: 'text-12 text-muted' }"
          class="shrink-0"
          @update:model-value="store.toggleHidden(path, [selection])"
        />
      </div>
      <!-- the condition the widget is shown on: the game answers it; the window is drawn at one value of each group -->
      <UAlert
        v-if="conditionGroups.length"
        title="Shown when"
        :description="element.visibleBinding"
        color="secondary"
        variant="subtle"
        orientation="vertical"
        class="mx-3 mt-2.5 w-auto shrink-0 px-2.5 py-2"
        :ui="{
          title: 'text-11.5 font-normal text-toned',
          description: 'mt-1 font-mono text-11.5 break-all text-(--syn-scope) opacity-100',
          actions: 'mt-1.5 w-full flex-col items-stretch gap-1',
        }"
      >
        <template #actions>
          <div v-for="group in conditionGroups" :key="group.call" class="flex items-center gap-2">
            <span class="min-w-0 grow truncate font-mono text-11 text-dimmed" :title="group.call">{{
              group.call
            }}</span>
            <USelect
              :model-value="group.value"
              :items="group.items"
              size="xs"
              class="w-40 shrink-0"
              @update:model-value="(value) => store.setState(path, group.call, String(value))"
            />
          </div>
        </template>
      </UAlert>
      <!-- one card per writer: its header names it and links to where it writes, each row a link of its own -->
      <div class="flex min-h-0 flex-1 flex-col gap-2.5 overflow-auto p-3">
        <section
          v-for="group in writers"
          :key="group.writer"
          class="shrink-0 overflow-hidden rounded-lg border border-default bg-default"
        >
          <div
            class="flex h-7.5 items-center gap-1.5 border-b border-default bg-elevated ps-2.5 pe-1.5 text-11.5 whitespace-nowrap text-dimmed"
          >
            <UIcon :name="group.writer ? 'i-lucide-layers' : 'i-lucide-pen'" class="size-3.25 shrink-0" />
            <template v-if="group.writer">
              <span>from</span>
              <span class="min-w-0 truncate font-mono font-medium text-highlighted" :title="group.writer">
                {{ group.writer }}
              </span>
            </template>
            <span v-else>This widget</span>
            <UTooltip text="Open where it is written">
              <UButton
                :label="linkText(group.location)"
                trailing-icon="i-lucide-arrow-up-right"
                variant="ghost"
                color="neutral"
                size="xs"
                class="ms-auto min-w-0 px-1.25 py-0.5 font-mono text-11 text-dimmed hover:bg-accented hover:text-secondary"
                :ui="{ label: 'truncate', trailingIcon: 'size-2.75' }"
                @click="store.goTo(path, group.location)"
              />
            </UTooltip>
          </div>
          <button
            v-for="property in group.rows"
            :key="property.key"
            type="button"
            class="group grid h-7 w-full grid-cols-[112px_minmax(0,1fr)_14px] items-center gap-x-3 ps-2.5 pe-2 text-start text-11.5 even:bg-accented/30 hover:bg-accented"
            :title="`${property.key} = ${property.value}`"
            @click="store.goTo(path, property.location)"
          >
            <span class="truncate font-medium text-toned">{{ property.key }}</span>
            <span class="min-w-0 truncate text-end"><ScriptSnippet :text="property.value" :name="path" inline /></span>
            <UIcon name="i-lucide-arrow-up-right" class="size-3.25 text-secondary opacity-0 group-hover:opacity-100" />
          </button>
        </section>
      </div>
    </template>
    <UEmpty
      v-else
      icon="i-lucide-mouse-pointer-click"
      title="Select a widget"
      description="Click it in the picture, or put the caret in its code."
      variant="naked"
      size="sm"
      class="flex-1"
    />
  </aside>
</template>
