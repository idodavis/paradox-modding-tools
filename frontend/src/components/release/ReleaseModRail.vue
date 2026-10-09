<script lang="ts">
export type Step = "listing" | "release" | "review";
/** Each step's title, in the rail and as its page's heading. */
export const STEP_TITLES: Record<Step, string> = {
  listing: "Listing and description",
  release: "Version and change note",
  review: "Review and upload",
};
</script>

<script setup lang="ts">
/**
 * Publish's rail (the Publish boards' aside): the workspace's mods with their state on Steam, and the three
 * steps (O-R44); Compare with Steam opens from Review's checklist and the state bar (O-R40).
 */
import { computed } from "vue";
import type { StepperItem } from "@nuxt/ui";
import type { WorkspaceMod } from "@services/models";
import { modColor } from "../../stores/workspace";

const { workspaceId, mods, saved } = defineProps<{ workspaceId: string; mods: WorkspaceMod[]; saved: boolean }>();
const mod = defineModel<string>("mod", { required: true });
const step = defineModel<Step>("step", { required: true });

const ORDER: Step[] = ["listing", "release", "review"];
// with everything saved, the steps before this one are done and the one after it is next, outlined in the
// accent; with changes unsaved, Review can't be reached, so nothing unsaved is uploaded
const steps = computed<StepperItem[]>(() => {
  const at = ORDER.indexOf(step.value);
  const blocked = !saved && step.value !== "review";
  const items = [
    { value: "listing", title: STEP_TITLES.listing, line: "What players see" },
    { value: "release", title: STEP_TITLES.release, line: "What this upload is" },
    {
      value: "review",
      title: STEP_TITLES.review,
      line: blocked ? "Save to workspace first" : "Check, then send to Steam",
      disabled: blocked,
    },
  ];
  return items.map(({ line, ...item }, i) => {
    const done = saved && i < at;
    const next = saved && i === at + 1;
    return {
      ...item,
      description: done ? "Saved" : next ? `Next: ${line.charAt(0).toLowerCase()}${line.slice(1)}` : line,
      icon: done ? "i-lucide-check" : undefined,
      ui: next ? { item: "ring-primary", title: "text-highlighted" } : undefined,
    };
  });
});
</script>

<template>
  <section class="relative flex min-h-0 w-64 shrink-0 flex-col gap-1 border-e border-default bg-muted px-2.5 py-3.5">
    <span class="px-2.5 pb-1.5 text-11 font-medium text-dimmed">Mod</span>
    <UListbox
      v-model="mod"
      :items="mods"
      value-key="id"
      :ui="{
        root: 'gap-0 ring-0',
        content: 'max-h-none',
        group: 'flex flex-col gap-1 p-0',
        item: [
          'h-12 items-center gap-2.5 rounded-md px-2.5 text-13 text-toned before:rounded-md',
          'data-[state=checked]:bg-elevated data-[state=checked]:text-highlighted data-[state=checked]:ring data-[state=checked]:ring-inset data-[state=checked]:ring-accented',
        ],
        itemTrailingIcon: 'hidden',
      }"
    >
      <template #item="{ item }">
        <Thumb :path="item.thumbnail" :color="modColor(item)" />
        <span class="flex min-w-0 grow flex-col">
          <span class="truncate font-medium">{{ item.name }}</span>
          <SteamState :workspace-id="workspaceId" :mod-id="item.id" />
        </span>
      </template>
    </UListbox>
    <USeparator class="my-3 px-1" />
    <span class="px-2.5 pb-1.5 text-11 font-medium text-dimmed">Steps</span>
    <UStepper
      :model-value="step"
      :items="steps"
      orientation="vertical"
      :linear="false"
      @update:model-value="step = ORDER.find((each) => each === $event) ?? 'listing'"
    />
  </section>
</template>
