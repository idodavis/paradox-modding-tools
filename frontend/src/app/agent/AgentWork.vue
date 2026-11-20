<!--
  What runs beside a session's answer, as one row above the prompt box: the
  checklist's step and a count of the subagents and of the background
  commands that run. A click opens a tray on one of the three, which also
  lists the ones that ended.
-->
<script setup lang="ts">
import { useIntervalFn, useTimestamp } from "@vueuse/core";
import { computed, reactive, shallowRef } from "vue";

import { describeDuration, plural } from "../../lib/format";
import { useAgentStore, type Session } from "../../stores/agent";
import { listChecklist, printTerminal, type Terminal } from "../../stores/agentThread";
import type { Subagent } from "../../stores/agentWork";
import AgentTranscript, { describeTool, STATUSES } from "./AgentTranscript.vue";

/** A Tab is one of the tray's lists, "" while the tray is closed. */
type Tab = "" | "tasks" | "subagents" | "background";

/** TAIL is how many of a command's last lines the tray shows. */
const TAIL = 8;

const { session } = defineProps<{ session: Session }>();
const open = defineModel<Tab>("open", { default: "" });

const agent = useAgentStore();
const now = useTimestamp({ scheduler: (tick) => useIntervalFn(tick, 1000) });
// The rows the user unfolded or folded by hand. A running row is unfolded
// and an ended one folded until then.
const toggled = reactive(new Set<string>());
// What the dialog shows whole: a subagent's conversation or a command's
// output.
const viewed = shallowRef<{ subagent?: Subagent; terminal?: Terminal }>();

const steps = computed(() => listChecklist(session));
// The step the agent is on: the one in progress, else the first not done.
const current = computed(
  () =>
    steps.value.find((step) => step.status === "in_progress") ?? steps.value.find((step) => step.status === "pending"),
);
// Where the checklist stands: the current step's place, or every step once
// all are done.
const position = computed(() => (current.value ? steps.value.indexOf(current.value) + 1 : steps.value.length));
const running = computed(() => session.subagents.filter((subagent) => subagent.isRunning).length);
// The commands that ran in the background, the ended ones too.
const commands = computed(() => session.backgrounded.flatMap((id) => session.terminals[id] ?? []));
const runningCommands = computed(() => commands.value.filter((terminal) => !terminal.isDone).length);
const tabs = computed(() =>
  [
    {
      value: "tasks" as const,
      label: `Tasks ${position.value} of ${steps.value.length}`,
      icon: "i-lucide-list-checks",
      count: steps.value.length,
      isLive: false,
    },
    {
      value: "subagents" as const,
      label: `Subagents ${running.value}`,
      icon: "i-lucide-bot",
      count: session.subagents.length,
      isLive: running.value > 0,
    },
    {
      value: "background" as const,
      label: `Background ${runningCommands.value}`,
      icon: "i-lucide-terminal",
      count: commands.value.length,
      isLive: runningCommands.value > 0,
    },
  ].filter((tab) => tab.count),
);
/** isUnfolded reports whether a row of the tray shows its details. */
function isUnfolded(id: string, isRunning: boolean): boolean {
  return toggled.has(id) ? !isRunning : isRunning;
}

/** toggle unfolds a folded row of the tray, or folds an unfolded one. */
function toggle(id: string): void {
  if (!toggled.delete(id)) toggled.add(id);
}

/**
 * describeTime returns how long something has run so far, or ran before it
 * ended.
 */
function describeTime(startedAt: number, endedAt: number): string {
  return describeDuration((endedAt || now.value) - startedAt);
}

/**
 * listLines returns a command's output as lines, without the blank ones at
 * its end.
 */
function listLines(terminal: Terminal): string[] {
  return printTerminal({ ...terminal, command: "" })
    .trimEnd()
    .split(/\r?\n/);
}
</script>

<template>
  <!-- The row stays once the session has anything to list, so a folded tray
       can always be opened again. -->
  <div v-if="tabs.length" class="flex flex-col">
    <div v-if="!open" class="flex h-6.5 items-center gap-1 ps-2 text-12">
      <template v-if="steps.length">
        <UIcon name="i-lucide-list-checks" class="size-3.5 shrink-0 text-dimmed" />
        <span class="shrink-0 font-medium tabular-nums">{{ position }} of {{ steps.length }}</span>
        <span class="min-w-0 truncate text-toned" :title="current?.text">{{ current?.text }}</span>
      </template>
      <span class="grow" />
      <template v-for="tab in tabs" :key="tab.value">
        <UButton
          v-if="tab.value !== 'tasks'"
          :label="tab.label.split(' ').at(-1)"
          :icon="tab.icon"
          variant="ghost"
          color="neutral"
          size="xs"
          :aria-label="tab.label"
          :ui="{ leadingIcon: tab.isLive ? 'text-primary' : 'text-dimmed' }"
          @click="open = tab.value"
        />
      </template>
      <UButton
        icon="i-lucide-chevron-up"
        variant="ghost"
        color="neutral"
        size="xs"
        square
        aria-label="Show what is running"
        @click="open = tabs[0]?.value ?? ''"
      />
    </div>

    <div v-else class="flex flex-col overflow-hidden rounded-lg border border-default bg-elevated/40">
      <div class="flex h-8.5 shrink-0 items-center gap-1 border-b border-default ps-1 pe-1">
        <UButton
          v-for="tab in tabs"
          :key="tab.value"
          :label="tab.label"
          :icon="tab.icon"
          :variant="open === tab.value ? 'soft' : 'ghost'"
          color="neutral"
          size="xs"
          :ui="{ leadingIcon: tab.isLive ? 'text-primary' : 'text-dimmed' }"
          @click="open = tab.value"
        />
        <span class="grow" />
        <UButton
          icon="i-lucide-chevron-down"
          variant="ghost"
          color="neutral"
          size="xs"
          square
          aria-label="Fold what is running"
          @click="open = ''"
        />
      </div>

      <div class="flex max-h-72 flex-col gap-0.5 overflow-y-auto p-1 text-12">
        <template v-if="open === 'tasks'">
          <span v-for="(step, index) in steps" :key="index" class="flex min-h-6 items-center gap-2 px-2 text-toned">
            <UIcon
              :name="STATUSES[step.status]?.icon ?? 'i-lucide-circle'"
              :class="STATUSES[step.status]?.color"
              class="size-3.5 shrink-0"
            />
            <span class="min-w-0 truncate" :class="STATUSES[step.status]?.text" :title="step.text">{{
              step.text
            }}</span>
          </span>
        </template>

        <template v-else-if="open === 'subagents'">
          <template v-for="subagent in session.subagents" :key="subagent.id">
            <div class="flex min-h-7 items-center gap-2 ps-2 pe-0.5">
              <UIcon
                :name="subagent.isRunning ? 'i-lucide-loader-circle' : 'i-lucide-check'"
                :class="subagent.isRunning ? 'animate-spin text-primary' : 'text-success'"
                class="size-3.5 shrink-0"
              />
              <span class="shrink-0 font-medium">{{ subagent.kind || "Subagent" }}</span>
              <span class="min-w-0 grow truncate text-toned" :title="subagent.title">{{ subagent.title }}</span>
              <span class="shrink-0 text-11 text-dimmed tabular-nums">
                {{ describeTime(subagent.startedAt, subagent.endedAt) }}
              </span>
              <UButton
                v-if="subagent.isRunning && subagent.canCancel"
                icon="i-lucide-square"
                color="error"
                variant="ghost"
                size="xs"
                square
                aria-label="Stop this subagent"
                @click="agent.stopSubagent(session, subagent)"
              />
              <UButton
                :icon="isUnfolded(subagent.id, subagent.isRunning) ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'"
                variant="ghost"
                color="neutral"
                size="xs"
                square
                :aria-label="isUnfolded(subagent.id, subagent.isRunning) ? 'Fold its steps' : 'Show its steps'"
                @click="toggle(subagent.id)"
              />
            </div>
            <div
              v-if="isUnfolded(subagent.id, subagent.isRunning)"
              class="ms-3.5 mb-1 flex flex-col gap-1 border-s border-accented ps-3"
            >
              <template v-for="entry in subagent.entries" :key="entry.id">
                <span v-if="entry.kind === 'tool'" class="flex min-h-5 items-center gap-1.5 text-toned">
                  <UIcon
                    :name="STATUSES[entry.status]?.icon ?? 'i-lucide-wrench'"
                    :class="[STATUSES[entry.status]?.color, { 'animate-spin': entry.status === 'in_progress' }]"
                    class="size-3.5 shrink-0"
                  />
                  <span class="min-w-0 truncate" :title="entry.title">{{ describeTool(entry).text }}</span>
                </span>
              </template>
              <UButton
                label="Its whole conversation"
                icon="i-lucide-arrow-up-right"
                variant="ghost"
                color="neutral"
                size="xs"
                class="-ms-1.5 self-start"
                @click="viewed = { subagent }"
              />
            </div>
          </template>
        </template>

        <template v-else>
          <template v-for="terminal in commands" :key="terminal.id">
            <div class="flex min-h-7 items-center gap-2 ps-2 pe-0.5">
              <UIcon
                :name="terminal.isDone ? 'i-lucide-check' : 'i-lucide-loader-circle'"
                :class="terminal.isDone ? 'text-success' : 'animate-spin text-primary'"
                class="size-3.5 shrink-0"
              />
              <span class="min-w-0 grow truncate font-mono text-11" :title="terminal.command">
                {{ terminal.command }}
              </span>
              <span class="shrink-0 text-11 text-dimmed tabular-nums">
                {{ terminal.isDone ? "ended after " : "" }}{{ describeTime(terminal.startedAt, terminal.endedAt) }}
              </span>
              <UButton
                v-if="!terminal.isDone && terminal.canCancel"
                icon="i-lucide-square"
                color="error"
                variant="ghost"
                size="xs"
                square
                aria-label="Stop this command"
                @click="agent.stopTerminal(session, session, terminal.id)"
              />
              <UButton
                :icon="isUnfolded(terminal.id, !terminal.isDone) ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'"
                variant="ghost"
                color="neutral"
                size="xs"
                square
                :aria-label="isUnfolded(terminal.id, !terminal.isDone) ? 'Fold its output' : 'Show its output'"
                @click="toggle(terminal.id)"
              />
            </div>
            <template v-if="isUnfolded(terminal.id, !terminal.isDone)">
              <pre
                class="mx-1 overflow-hidden rounded-sm bg-(--bg-0) px-2.5 py-1.5 font-mono text-11 leading-4.5 text-toned"
                >{{ listLines(terminal).slice(-TAIL).join("\n") }}</pre>
              <div class="flex items-center ps-2 pe-0.5">
                <span class="grow text-dimmed">
                  {{ plural(listLines(terminal).length, "line") }}{{ terminal.isDone ? "" : " so far" }}
                </span>
                <UButton
                  label="Whole output"
                  icon="i-lucide-arrow-up-right"
                  variant="ghost"
                  color="neutral"
                  size="xs"
                  @click="viewed = { terminal }"
                />
              </div>
            </template>
          </template>
        </template>
      </div>
    </div>

    <UModal
      :open="!!viewed"
      :title="
        viewed?.subagent ? `${viewed.subagent.kind || 'Subagent'}: ${viewed.subagent.title}` : viewed?.terminal?.command
      "
      :ui="{ content: 'max-w-3xl', body: 'max-h-[70vh] overflow-y-auto' }"
      @update:open="(isOpen) => isOpen || (viewed = undefined)"
    >
      <template #body>
        <AgentTranscript v-if="viewed?.subagent" :session="session" :thread="viewed.subagent" />
        <pre v-else-if="viewed?.terminal" class="font-mono text-11 leading-4.5 whitespace-pre-wrap text-toned">{{
          printTerminal(viewed.terminal)
        }}</pre>
      </template>
    </UModal>
  </div>
</template>
