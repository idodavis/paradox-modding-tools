<!--
  The Visual view's panel: a window's Properties and States tabs, or a coat's Layers.
-->
<script setup lang="ts">
import { useMutation, useQuery } from "@pinia/colada";
import { Clipboard } from "@wailsio/runtime";
import { Range } from "monaco-editor";
import { computed } from "vue";

import * as draw from "@services/draw/service";
import * as workspace from "@services/workspace/service";
import type { Coat, Window } from "@services/draw/models";
import type { Edit, Range as Span } from "@services/lsp/models";
import type { Place } from "@services/workspace/models";

import { drawablesQuery, stateQuery } from "../../../queries";
import { useDrawStore } from "../../../stores/draw";
import type { DrawFile } from "../../../stores/draw";
import { useEditorStore } from "../../../stores/editor";
import { useWorkspaceStore } from "../../../stores/workspace";
import ConfirmModal from "../../../ui/ConfirmModal.vue";
import OriginChip from "../../../ui/OriginChip.vue";
import { applyEdits, openModel } from "../editor/models";
import type { Model } from "../editor/models";
import CoatLayers from "./CoatLayers.vue";
import WidgetProperties from "./WidgetProperties.vue";
import WindowStates from "./WindowStates.vue";
import { listMods } from "./coat";

const props = defineProps<{ model: Model; window: Window | null; coat: Coat | null }>();

type Tab = { label: string; icon: string; value: DrawFile["tab"] };

const WINDOW_TABS: Tab[] = [
  { label: "Properties", icon: "i-lucide-sliders-horizontal", value: "properties" },
  { label: "States", icon: "i-lucide-braces", value: "states" },
];
const COAT_TABS: Tab[] = [{ label: "Layers", icon: "i-lucide-layers", value: "layers" }];

const store = useDrawStore();
const editor = useEditorStore();
const workspaceStore = useWorkspaceStore();
const overlay = useOverlay();

const file = computed(() => store.file(props.model.place.path));
const tabs = computed(() => (props.coat ? COAT_TABS : WINDOW_TABS));
const tab = computed(() =>
  tabs.value.some(({ value }) => value === file.value.tab) ? file.value.tab : tabs.value[0].value,
);

const { data: state } = useQuery(() => ({ ...stateQuery, enabled: workspaceStore.isOpen() }));
const { data: drawables } = useQuery(() => ({
  ...drawablesQuery(props.model.place.path),
  enabled: workspaceStore.isOpen(),
}));

const mods = computed(() =>
  listMods(state.value?.mods).map(({ label, value }) => ({ label, onSelect: () => copy(value) })),
);

const actions = computed(() => [
  {
    label: "Copy for the game",
    tip: "Copy this coat's script for the game's designer",
    icon: "i-lucide-copy",
    color: "neutral" as const,
    onClick: copyScript,
  },
  ...(props.model.place.readOnly
    ? []
    : [
        {
          label: "Paste from the game",
          tip: "Replace this coat with the game designer's copy",
          icon: "i-lucide-clipboard-paste",
          color: "neutral" as const,
          onClick: () => void pasteScript(),
        },
        {
          label: "Delete the coat",
          tip: "Delete this coat from the file",
          icon: "i-lucide-trash-2",
          color: "error" as const,
          onClick: () => void deleteCoat(),
        },
      ]),
]);

const drawn = computed(() => drawables.value?.find(({ name }) => name === file.value.drawable));

const { mutate: edit } = useMutation({
  mutation: async (change: { place: Place; request: () => Promise<Edit[] | null> }) =>
    applyEdits(await openModel(change.place), (await change.request()) ?? []),
});
const { mutate: copy } = useMutation({
  mutation: async (modId: string) =>
    workspace.Locate(await draw.CopyCoat(props.model.place.path, props.coat?.name ?? "", modId)),
  onSuccess: (place) => editor.show(place),
});

/** Finds the script inside the braces of the coat and where it is, or null for a coat written as another's name. */
function findBody(model: Model, span: Span): { text: string; range: Range } | null {
  const text = model.model.getValueInRange(span);
  const open = text.indexOf("{");
  if (open < 0) return null;
  const base = model.model.getOffsetAt(Range.getStartPosition(span));
  const start = model.model.getPositionAt(base + open + 1);
  const end = model.model.getPositionAt(base + text.lastIndexOf("}"));
  return { text: text.slice(open + 1, text.lastIndexOf("}")).trim(), range: Range.fromPositions(start, end) };
}

/** Copies the coat's script to the clipboard, as the game's coat designer pastes it. */
function copyScript(): void {
  const body = props.coat && findBody(props.model, props.coat.range);
  if (body) void Clipboard.SetText(body.text);
}

/** Replaces the coat's script with the one on the clipboard, which the game's coat designer copies. */
async function pasteScript(): Promise<void> {
  const body = props.coat && findBody(props.model, props.coat.range);
  if (!body || !props.coat) return;
  const text = await Clipboard.Text();
  edit({ place: props.coat.place, request: () => Promise.resolve([{ range: body.range, text: `\n${text}\n` }]) });
}

/** Deletes the coat from the file once the user confirms. */
async function deleteCoat(): Promise<void> {
  const coat = props.coat;
  const choice = await overlay.create(ConfirmModal).open({
    title: "Delete the coat",
    body: `${coat?.name} is removed from the file.`,
    confirm: "Delete",
    destructive: true,
  });
  if (coat && choice) edit({ place: coat.place, request: () => draw.RemoveBlock(props.model.place.path, coat.range) });
}

/** Shows a tab of the panel. */
function selectTab(value: string | number): void {
  const next = tabs.value.find((one) => one.value === value);
  if (next) file.value.tab = next.value;
}
</script>

<template>
  <aside class="flex min-h-0 min-w-0 flex-1 flex-col bg-muted text-(length:--ide-ui-font)">
    <div class="flex flex-col gap-1.5 border-b border-default px-4 pt-3.5 pb-3">
      <div class="flex flex-wrap items-center gap-2">
        <UBadge v-if="drawn" :label="drawn.what" color="neutral" variant="subtle" size="sm" class="capitalize" />
        <OriginChip :origin="model.place.origin" />
        <span class="grow" />
        <template v-if="coat">
          <UTooltip v-for="action in actions" :key="action.icon" :text="action.tip">
            <UButton
              :icon="action.icon"
              :aria-label="action.label"
              variant="ghost"
              :color="action.color"
              size="xs"
              square
              @click="action.onClick"
            />
          </UTooltip>
          <UDropdownMenu v-if="model.place.readOnly" :items="mods">
            <UButton label="Edit a copy" icon="i-lucide-copy-plus" size="xs" />
          </UDropdownMenu>
        </template>
      </div>
      <span class="font-mono text-16 leading-5.5 font-semibold break-all text-highlighted">{{ model.place.rel }}</span>
    </div>
    <UTabs
      :model-value="tab"
      :items="tabs"
      :content="false"
      size="xs"
      variant="link"
      class="shrink-0 border-b border-default px-2"
      @update:model-value="selectTab"
    />
    <CoatLayers v-if="tab === 'layers' && coat" :model="model" :coat="coat" />
    <WidgetProperties v-else-if="tab === 'properties' && window" :path="model.place.path" :window="window" />
    <WindowStates v-else-if="tab === 'states' && window" :path="model.place.path" :window="window" />
  </aside>
</template>
