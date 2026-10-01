<script setup lang="ts">
/**
 * The selected definition, opened: its header, then three sections (overview, script and links),
 * each written once and placed as tabs in a drawer or as columns in a bottom panel.
 */
import { computed, ref } from "vue";
import { createReusableTemplate } from "@vueuse/core";
import type { Detail } from "@services/models";
import FieldHelpTip from "../FieldHelpTip.vue";
import OriginBadge from "../OriginBadge.vue";
import ScriptSnippet from "../ScriptSnippet.vue";
import { useOpenInIde } from "../../composables/useOpenInIde";
import { originHexByOriginId } from "../../ide/rootDecorations";
import { useWorkspaceStore } from "../../stores/workspace";

const props = defineProps<{ workspaceId: string; detail: Detail | null | undefined; layout: "drawer" | "panel" }>();
const emit = defineEmits<{ close: []; root: [id: string] }>();
const store = useWorkspaceStore();
const { openInIde } = useOpenInIde();
const [DefineHead, Head] = createReusableTemplate<{ title: string; hint: string }>();
const [DefineOverview, Overview] = createReusableTemplate();
const [DefineScript, Script] = createReusableTemplate();
const [DefineLinks, Links] = createReusableTemplate();

// what each section holds, told by the tip beside its head
const HINTS = {
  properties: "The block's one-word members, as written.",
  text: "The members that name a localization key, with the text the playset gives it. Hover a name for its key.",
  options: "The event's options in order, by the text of their name.",
  glance: "How many definitions this one fires, how many fire it, and how many kinds of name its block uses.",
  script: "The definition as the kept file writes it.",
  links:
    "What this fires and what fires it: click one to draw the graph from it. Then every name its block uses, " +
    "by kind: click one to open it in the IDE.",
};
const TABS = [
  { label: "Overview", slot: "overview" as const },
  { label: "Script", slot: "script" as const },
  { label: "Links", slot: "links" as const },
];

// the block's one-word keys, and its keys that name loc text
const words = computed(() => props.detail?.sections?.filter((section) => section.value && !section.text) ?? []);
const texts = computed(() => props.detail?.sections?.filter((section) => section.text) ?? []);

// the panel's height and the drawer's width, each dragged by its inner edge
const size = ref({ height: 300, width: 560 });
function resize(down: PointerEvent, side: "height" | "width"): void {
  const client = side === "height" ? "clientY" : "clientX";
  const [least, most] = side === "height" ? [160, 640] : [380, 960];
  const start = size.value[side] + down[client];
  const move = (event: PointerEvent) => (size.value[side] = Math.min(Math.max(start - event[client], least), most));
  window.addEventListener("pointermove", move);
  window.addEventListener("pointerup", () => window.removeEventListener("pointermove", move), { once: true });
}

function open(path: string, line: number): void {
  void openInIde(props.workspaceId, path, line + 1);
}
</script>

<template>
  <DefineHead v-slot="{ title, hint }">
    <div class="mb-2 flex items-center gap-1">
      <h4 class="text-sm font-semibold text-highlighted">{{ title }}</h4>
      <FieldHelpTip :text="hint" />
    </div>
  </DefineHead>

  <DefineOverview>
    <div v-if="detail" class="space-y-6 text-sm">
      <section v-if="words.length">
        <Head title="Properties" :hint="HINTS.properties" />
        <div class="flex flex-wrap gap-1.5">
          <UBadge
            v-for="section in words"
            :key="section.line"
            color="neutral"
            variant="subtle"
            :label="`${section.key} = ${section.value}`"
          />
        </div>
      </section>
      <section v-if="texts.length">
        <Head title="Text" :hint="HINTS.text" />
        <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-3 rounded-md border border-default bg-default p-3">
          <template v-for="section in texts" :key="section.line">
            <dt class="text-muted" :title="section.value">{{ section.key }}</dt>
            <dd>
              <blockquote class="border-l-2 border-accented pl-3 text-default italic">{{ section.text }}</blockquote>
            </dd>
          </template>
        </dl>
      </section>
      <section v-if="detail.options?.length">
        <Head :title="`Options (${detail.options.length})`" :hint="HINTS.options" />
        <ol class="divide-y divide-default rounded-md border border-default bg-default">
          <li
            v-for="(option, i) in detail.options"
            :key="option.line"
            class="flex items-center gap-3 px-3 py-2"
            :title="option.name"
          >
            <span class="grid size-5 shrink-0 place-items-center rounded bg-accented text-xs font-medium text-toned">
              {{ String.fromCharCode(97 + i) }}
            </span>
            <span v-if="option.text || option.name" class="text-default">{{ option.text || option.name }}</span>
            <span v-else class="text-dimmed">No name</span>
          </li>
        </ol>
      </section>
      <section>
        <Head title="At a glance" :hint="HINTS.glance" />
        <div class="flex flex-wrap gap-1.5">
          <UBadge color="neutral" variant="outline" :label="`Fires ${detail.fires?.length ?? 0}`" />
          <UBadge color="neutral" variant="outline" :label="`Fired by ${detail.firedBy?.length ?? 0}`" />
          <UBadge color="neutral" variant="outline" :label="`Names ${detail.refs?.length ?? 0} kinds`" />
        </div>
      </section>
    </div>
  </DefineOverview>

  <!-- the definition as written; long lines scroll -->
  <DefineScript>
    <Head title="Script" :hint="HINTS.script" />
    <div v-if="detail?.lines?.length" class="overflow-x-auto rounded-md border border-default bg-default">
      <ScriptSnippet
        :text="detail.lines.join('\n')"
        language="paradox"
        :from-line="detail.line"
        gutter
        class="w-max min-w-full border-0! bg-transparent!"
      />
    </div>
  </DefineScript>

  <DefineLinks>
    <Head title="Links" :hint="HINTS.links" />
    <div v-if="detail" class="divide-y divide-default text-sm">
      <section
        v-for="[head, fires] in [['Fires', detail.fires], ['Fired by', detail.firedBy]] as const"
        v-show="fires?.length"
        :key="head"
        class="py-3 first:pt-0"
      >
        <h5 class="mb-1.5 font-medium text-toned">{{ head }}</h5>
        <!-- each with the chain of macros and the token between, as the graph draws it -->
        <div v-for="fire in fires" :key="fire.name" class="py-0.5">
          <UButton :label="fire.name" size="sm" variant="link" class="p-0" @click="emit('root', fire.name)" />
          <div class="font-mono text-xs text-dimmed">{{ fire.via }}</div>
        </div>
      </section>
      <section v-for="group in detail.refs" :key="group.database" class="py-3 first:pt-0">
        <h5 class="mb-1.5 font-medium text-toned">{{ group.database }}</h5>
        <div class="flex flex-wrap gap-x-3 gap-y-1">
          <UButton
            v-for="ref in group.refs"
            :key="`${ref.line}:${ref.col}`"
            :label="ref.name"
            size="sm"
            variant="link"
            color="neutral"
            class="p-0"
            @click="open(ref.at.path, ref.line)"
          />
        </div>
      </section>
    </div>
  </DefineLinks>

  <div v-if="layout === 'drawer'" class="contents" :style="{ '--drawer': `${size.width}px` }">
    <USlideover
      :open="!!detail"
      :modal="false"
      :overlay="false"
      :portal="false"
      :dismissible="false"
      :title="detail?.name"
      :description="detail?.title"
      :ui="{ content: 'absolute max-w-none w-(--drawer)', title: 'text-lg', body: 'space-y-4' }"
      @update:open="emit('close')"
    >
      <template #body>
        <div
          class="absolute inset-y-0 left-0 w-1.5 cursor-col-resize hover:bg-primary/30"
          @pointerdown="resize($event, 'width')"
        />
        <template v-if="detail">
          <div class="flex items-center gap-2">
            <OriginBadge :label="store.originName(detail.at.origin)" :hex="originHexByOriginId(detail.at.origin)" />
            <span class="min-w-0 flex-1 truncate font-mono text-xs text-muted">
              {{ detail.at.rel }}:{{ detail.line + 1 }}
            </span>
            <UButton label="Open in IDE" size="sm" @click="open(detail.at.path, detail.line)" />
            <UButton
              label="Set as root"
              size="sm"
              color="neutral"
              variant="outline"
              @click="emit('root', detail.name)"
            />
          </div>
          <UTabs :items="TABS" variant="link" size="sm">
            <template #overview><Overview /></template>
            <template #script><Script /></template>
            <template #links><Links /></template>
          </UTabs>
        </template>
      </template>
    </USlideover>
  </div>

  <div
    v-else-if="detail"
    class="flex shrink-0 flex-col border-t border-default bg-elevated"
    :style="{ height: `${size.height}px` }"
  >
    <div class="flex h-3 shrink-0 cursor-row-resize justify-center pt-1" @pointerdown="resize($event, 'height')">
      <div class="h-1 w-10 rounded-full bg-accented" />
    </div>
    <div class="flex shrink-0 items-center gap-3 border-b border-default px-4 pb-2">
      <div class="min-w-0">
        <div class="truncate text-lg font-semibold text-highlighted">{{ detail.name }}</div>
        <div v-if="detail.title" class="truncate text-sm text-toned">{{ detail.title }}</div>
      </div>
      <OriginBadge :label="store.originName(detail.at.origin)" :hex="originHexByOriginId(detail.at.origin)" />
      <span class="truncate font-mono text-xs text-muted">{{ detail.at.rel }}:{{ detail.line + 1 }}</span>
      <div class="flex-1" />
      <UButton label="Open in IDE" size="sm" @click="open(detail.at.path, detail.line)" />
      <UButton label="Set as root" size="sm" color="neutral" variant="outline" @click="emit('root', detail.name)" />
      <UButton icon="i-lucide-x" label="Close" size="sm" color="neutral" variant="ghost" @click="emit('close')" />
    </div>
    <div class="grid min-h-0 flex-1 grid-cols-[1fr_1.5fr_1fr] divide-x divide-default">
      <section class="overflow-auto p-4"><Overview /></section>
      <section class="overflow-auto p-4"><Script /></section>
      <section class="overflow-auto p-4"><Links /></section>
    </div>
  </div>
</template>
