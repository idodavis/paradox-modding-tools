<script setup lang="ts" generic="S extends string">
/**
 * Publish's rail: the workspace's mods with their state on Steam, and the steps the page gives it; Compare with Steam
 * opens from Review's checklist and the state bar.
 */
import { computed } from "vue";
import type { StepperItem } from "@nuxt/ui";
import type { WorkspaceMod } from "@services/models";
import { modColor } from "../../stores/workspace";
import SteamState from "../../ui/SteamState.vue";
import ModThumb from "../../ui/ModThumb.vue";

const { workspaceId, mods, saved, steps } = defineProps<{
  workspaceId: string;
  mods: WorkspaceMod[];
  saved: boolean;
  /** The steps in order, each with its title and the line under it. */
  steps: readonly { value: S; title: string; line: string }[];
}>();
const mod = defineModel<string>("mod", { required: true });
const step = defineModel<S>("step", { required: true });

// with everything saved, the steps before this one are done and the one after it is next, outlined in the
// accent; with changes unsaved, Review can't be reached, so nothing unsaved is uploaded
const items = computed<StepperItem[]>(() => {
  const stepIndex = steps.findIndex((one) => one.value === step.value);
  const blocked = !saved && step.value !== "review";
  return steps.map(({ value, title, line }, i) => {
    const done = saved && i < stepIndex;
    const next = saved && i === stepIndex + 1;
    const review = blocked && value === "review";
    const text = review ? "Save to workspace first" : line;
    return {
      value,
      title,
      disabled: review,
      description: done ? "Saved" : next ? `Next: ${text.charAt(0).toLowerCase()}${text.slice(1)}` : text,
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
        <ModThumb :path="item.thumbnail" :color="modColor(item)" />
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
      :items="items"
      orientation="vertical"
      :linear="false"
      @update:model-value="step = steps.find((one) => one.value === $event)?.value ?? step"
    />
  </section>
</template>
