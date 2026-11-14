<!--
  A ring of the context an agent's session has used. It opens a panel of the
  context window and the agent's plan.
-->
<script setup lang="ts">
import { createReusableTemplate } from "@vueuse/core";
import { computed, shallowRef } from "vue";

import { describeCost, describeCount } from "../lib/format";
import { describeLimit, useAgentStore, type Session } from "../stores/agent";

/** An Action is a button of the panel: it sends one of the agent's commands. */
type Action = { command: string; label: string; icon: string };

const { session } = defineProps<{ session: Session }>();

const CONTEXT_ACTIONS: Action[] = [
  { command: "compact", label: "Compact now", icon: "i-lucide-shrink" },
  { command: "context", label: "Show breakdown", icon: "i-lucide-chart-pie" },
];
const PLAN_ACTIONS: Action[] = [{ command: "usage", label: "Show plan usage", icon: "i-lucide-gauge" }];

const agent = useAgentStore();
const [DefineActions, ShowActions] = createReusableTemplate<{ actions: Action[] }>();
const open = shallowRef(false);

// The ring's arc as a share of 100, which its circle's path length is.
const filled = computed(() => {
  const usage = session.usage;

  return usage?.size ? Math.min(100, (usage.used / usage.size) * 100) : 0;
});
const percent = computed(() => Math.round(filled.value));
const limitLine = computed(() => {
  const limit = session.usage?.limit;

  return limit ? [limit.window, describeLimit(limit)].filter(Boolean).join(" · ") : "";
});
const noLimit = computed(() =>
  isOffered("usage")
    ? `${session.agent.name} hasn't reported plan usage in this session yet.`
    : `${session.agent.name} doesn't report plan usage to PMT.`,
);

/** isOffered reports whether the session offers the command. */
function isOffered(command: string): boolean {
  return session.commands.some((offered) => offered.name === command);
}

/** run sends the command as a prompt and closes the panel. */
function run(command: string): void {
  void agent.prompt(session, `/${command}`, []);
  open.value = false;
}
</script>

<template>
  <!-- The panel takes no focus as it opens, so that no key press compacts. -->
  <UPopover
    v-if="session.usage"
    v-model:open="open"
    :content="{ align: 'start', side: 'top', onOpenAutoFocus: (event) => event.preventDefault() }"
  >
    <UTooltip :text="`Context: ${percent}% used`">
      <button
        type="button"
        class="mx-1 flex size-6 items-center justify-center rounded-md hover:bg-accented"
        :aria-label="`Context: ${percent}% used`"
      >
        <svg viewBox="0 0 20 20" class="size-4 shrink-0 -rotate-90 fill-none stroke-3" aria-hidden="true">
          <circle cx="10" cy="10" r="8" class="stroke-(color:--line-2)" />
          <circle
            cx="10"
            cy="10"
            r="8"
            pathLength="100"
            :stroke-dasharray="`${filled} 100`"
            class="stroke-(color:--accent)"
          />
        </svg>
      </button>
    </UTooltip>
    <template #content>
      <DefineActions v-slot="{ actions }">
        <div class="flex flex-wrap gap-1.5">
          <UButton
            v-for="action in actions.filter((row) => isOffered(row.command))"
            :key="action.command"
            :label="action.label"
            :icon="action.icon"
            variant="soft"
            size="xs"
            :disabled="session.state !== 'idle'"
            @click="run(action.command)"
          />
        </div>
      </DefineActions>
      <div class="w-72">
        <section class="flex flex-col gap-2 border-b border-default px-3.5 py-3">
          <span class="text-12 font-semibold">Context window</span>
          <UProgress :model-value="percent" size="sm" :ui="{ base: 'bg-(--line-2)' }" />
          <span class="text-12 text-toned tabular-nums">
            {{ describeCount(session.usage.used) }} of {{ describeCount(session.usage.size) }} tokens ({{ percent }}%)
          </span>
          <span class="text-12 text-dimmed">
            {{ session.agent.name }} compacts the conversation by itself when the window fills.
          </span>
          <ShowActions :actions="CONTEXT_ACTIONS" />
        </section>
        <section class="flex flex-col gap-2 px-3.5 py-3">
          <span class="text-12 font-semibold">Plan</span>
          <span v-if="limitLine" class="text-12 text-toned">{{ limitLine }}</span>
          <span v-else class="text-12 text-dimmed">{{ noLimit }}</span>
          <span v-if="session.usage.cost" class="text-12 text-toned tabular-nums">
            {{ describeCost(session.usage.cost) }} at API prices this session
          </span>
          <ShowActions :actions="PLAN_ACTIONS" />
        </section>
      </div>
    </template>
  </UPopover>
</template>
