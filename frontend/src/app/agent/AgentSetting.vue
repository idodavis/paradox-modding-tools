<!--
  One of an agent's settings as a panel: what the setting is, then each of its
  choices with a line, the one in force marked. A choice that makes the agent
  ask PMT nothing is red.
-->
<script setup lang="ts">
import type { SessionConfigOption } from "@agentclientprotocol/sdk/experimental/v2";
import { uniqBy } from "es-toolkit";
import { computed } from "vue";

import { isUnaskedMode, listChoices, useAgentStore, type Session } from "../../stores/agent";

const {
  session,
  option,
  title = "",
  description = "",
  hidden = [],
  lines = {},
  marks = {},
} = defineProps<{
  session: Session;
  option: SessionConfigOption;
  /** The panel's title and its line, where the setting's own do not say enough. */
  title?: string;
  description?: string;
  /** The values left out of the list, which another control sets. */
  hidden?: string[];
  /** PMT's line for a choice, by its value, where the agent sends none. */
  lines?: Record<string, string>;
  /** A badge beside a choice, by its value. */
  marks?: Record<string, string>;
}>();

const agent = useAgentStore();
// An agent may send one choice twice, as GitHub Copilot does its Auto model.
const choices = computed(() =>
  uniqBy(listChoices(option), (choice) => choice.value).filter((choice) => !hidden.includes(choice.value)),
);
</script>

<template>
  <div class="flex flex-col gap-1.5 border-b border-default px-3 py-2.5">
    <span class="text-13 font-semibold">{{ title || option.name }}</span>
    <p v-if="description || option.description" class="text-12 leading-4.5 text-toned">
      {{ description || option.description }}
    </p>
  </div>
  <!-- A long list, as of Claude's fifteen models, scrolls by itself. -->
  <div class="flex max-h-80 shrink-0 flex-col gap-px overflow-y-auto p-1">
    <button
      v-for="choice in choices"
      :key="choice.value"
      type="button"
      class="flex items-start gap-2 rounded-md px-2 py-1.5 text-start hover:bg-accented"
      :class="choice.value === option.currentValue && 'bg-(--accent-soft)'"
      :aria-pressed="choice.value === option.currentValue"
      @click="agent.configure(session, option.configId, choice.value)"
    >
      <UIcon
        name="i-lucide-check"
        class="mt-0.5 size-3.5 shrink-0 text-primary"
        :class="choice.value !== option.currentValue && 'invisible'"
      />
      <span class="flex min-w-0 grow flex-col">
        <span class="text-13" :class="isUnaskedMode(session.agent, option, choice.value) && 'text-error'">
          {{ choice.name }}
        </span>
        <span
          v-if="choice.description || lines[choice.value]"
          class="line-clamp-3 text-11.5 leading-4 text-dimmed"
          :title="choice.description ?? ''"
        >
          {{ choice.description || lines[choice.value] }}
        </span>
      </span>
      <UBadge
        v-if="marks[choice.value]"
        :label="marks[choice.value]"
        color="neutral"
        variant="subtle"
        size="sm"
        class="shrink-0"
      />
    </button>
  </div>
  <slot />
</template>
