<!--
  The agent pane, docked at the right of the main window's workspace pages: the Agents page's session, narrowed, with
  the session menu in its header. With no session it offers the installed agents, the recent sessions to resume and
  the starter tasks.
-->
<script setup lang="ts">
import type { DropdownMenuItem } from "@nuxt/ui";
import { useQuery } from "@pinia/colada";
import { until } from "@vueuse/core";
import { computed } from "vue";
import { useRouter } from "vue-router";

import type { Task } from "@services/mcp/models";
import { abbreviate, describeAge } from "../lib/format";
import { agentsQuery, tasksQuery } from "../queries";
import { useAgentStore } from "../stores/agent";
import { useWorkspaceStore } from "../stores/workspace";
import AgentSession from "../ui/AgentSession.vue";

const agent = useAgentStore();
const workspace = useWorkspaceStore();
const router = useRouter();
const { data: agents } = useQuery(agentsQuery);
const { data: tasks } = useQuery(tasksQuery);

const session = computed(() => agent.active);
const ready = computed(() => (agents.value ?? []).filter((row) => row.program));
// the agent a task starts with: the one used last, else the first that can start
const taskAgent = computed(() => ready.value.find((row) => row.id === agent.lastAgent) ?? ready.value[0]);
// the sessions to resume, the latest first: those not archived whose agent can start, each with its agent's name
const recent = computed(() =>
  agent.records
    .filter((record) => !record.isArchived)
    .flatMap((record) => {
      const row = ready.value.find((one) => one.id === record.agent);
      return row ? [{ record, line: `${row.name} · ${describeAge(new Date(record.updatedAt), "short")}` }] : [];
    })
    .slice(0, 8),
);

/**
 * The session menu: the shown agent's sessions but the archived ones, each with Archive and Delete, then a new
 * session, another agent, the Agents page, and undoing the session's edits while any is not undone.
 */
const menu = computed(() => {
  const shown = session.value?.agent;
  const isUndoable = !!session.value && agent.isUndoable(session.value);
  const records = agent.records.filter((record) => record.agent === shown?.id && !record.isArchived).slice(0, 8);
  return [
    shown && records.length
      ? [
          { type: "label" as const, label: `Sessions with ${shown.name}` },
          ...records.map((record) => ({
            label: record.title,
            slot: "session" as const,
            record,
            onSelect: () => agent.open(record),
          })),
        ]
      : [],
    [
      ...(shown ? [{ label: "New session", icon: "i-lucide-plus", onSelect: () => agent.start(shown.id) }] : []),
      {
        label: "Another agent",
        icon: "i-lucide-bot",
        children: ready.value.map((row) => ({ label: row.name, onSelect: () => agent.start(row.id) })),
      },
      { label: "Open in the Agents page", icon: "i-lucide-maximize-2", onSelect: openPage },
    ],
    isUndoable
      ? [{ label: "Undo all edits", icon: "i-lucide-undo-2", color: "error" as const, onSelect: undoAll }]
      : [],
  ] satisfies DropdownMenuItem[][];
});

/** undoAll asks first, then undoes the edits of the session shown that are not undone yet. */
function undoAll(): void {
  if (session.value) void agent.undoAll(session.value);
}

/** openPage shows the Agents page in this window, which shows the same session. */
function openPage(): void {
  void router.push({ name: "agents", params: { id: workspace.activeId() } });
}

/** runTask starts a session with the agent used last and sends a starter task once the agent is ready. */
async function runTask(task: Task): Promise<void> {
  if (!taskAgent.value) return;
  const taskSession = agent.start(taskAgent.value.id);
  await until(() => taskSession.state).not.toBe("starting");
  if (taskSession.state === "idle") await agent.prompt(taskSession, task.text, []);
}
</script>

<template>
  <aside class="flex h-full min-h-0 min-w-0 flex-1 flex-col border-s border-default bg-default" aria-label="Agent pane">
    <div class="flex h-10 shrink-0 items-center gap-1 border-b border-default ps-2.5 pe-1.5">
      <template v-if="session">
        <UAvatar :text="abbreviate(session.agent.name)" size="xs" class="rounded-sm" />
        <UDropdownMenu :items="menu" :content="{ align: 'start' }">
          <UButton
            :label="session.title"
            trailing-icon="i-lucide-chevron-down"
            variant="ghost"
            size="sm"
            class="min-w-0 font-semibold"
            :ui="{ label: 'truncate', trailingIcon: 'text-dimmed' }"
          />
          <!-- the buttons keep their pointer to themselves, so the item does not open the session too -->
          <template #session-trailing="{ item }">
            <span class="ms-auto flex" @pointerdown.stop @pointerup.stop @click.stop>
              <UButton
                icon="i-lucide-archive"
                variant="ghost"
                size="xs"
                square
                aria-label="Archive"
                @click="agent.archive(item.record, true)"
              />
              <UButton
                icon="i-lucide-trash-2"
                color="error"
                variant="ghost"
                size="xs"
                square
                aria-label="Delete"
                @click="agent.deleteSession(item.record)"
              />
            </span>
          </template>
        </UDropdownMenu>
      </template>
      <span v-else class="ps-1 text-13 font-semibold">Agent</span>
      <span class="grow" />
      <UTooltip v-if="session" text="New session">
        <UButton
          icon="i-lucide-plus"
          variant="ghost"
          size="sm"
          square
          aria-label="New session"
          @click="agent.start(session.agent.id)"
        />
      </UTooltip>
      <UTooltip text="Open in the Agents page">
        <UButton
          icon="i-lucide-maximize-2"
          variant="ghost"
          size="sm"
          square
          aria-label="Open in the Agents page"
          @click="openPage"
        />
      </UTooltip>
      <UTooltip text="Open in its own window">
        <UButton
          icon="i-lucide-app-window"
          variant="ghost"
          size="sm"
          square
          aria-label="Open in its own window"
          :disabled="!!session && !session.resumable"
          @click="agent.popOut(session)"
        />
      </UTooltip>
      <UTooltip text="Close the pane">
        <UButton
          icon="i-lucide-x"
          variant="ghost"
          size="sm"
          square
          aria-label="Close the pane"
          @click="agent.isPaneOpen = false"
        />
      </UTooltip>
    </div>

    <AgentSession v-if="session" :key="session.key" :session="session" narrow />
    <div v-else class="flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto px-3.5 py-4">
      <span class="text-12 text-dimmed">Start a session with an agent, or with a task.</span>
      <div
        v-for="row in ready"
        :key="row.id"
        class="flex items-center gap-2.5 rounded-md border border-default bg-muted px-2.5 py-2"
      >
        <UAvatar :text="abbreviate(row.name)" size="xs" class="rounded-sm" />
        <span class="grow truncate text-13 font-medium">{{ row.name }}</span>
        <UButton label="Start" size="xs" @click="agent.start(row.id)" />
      </div>
      <template v-if="recent.length">
        <span class="mt-2 text-11 font-semibold text-dimmed">Recent sessions</span>
        <button
          v-for="{ record, line } in recent"
          :key="record.id"
          type="button"
          class="flex items-center gap-2 rounded-md px-2 py-1.5 text-start text-12 hover:bg-elevated"
          @click="agent.open(record)"
        >
          <UIcon name="i-lucide-message-circle" class="size-3.5 shrink-0 text-dimmed" />
          <span class="flex min-w-0 grow flex-col">
            <span class="truncate">{{ record.title }}</span>
            <span class="text-11 text-dimmed">{{ line }}</span>
          </span>
        </button>
      </template>
      <template v-if="taskAgent">
        <UButton
          v-for="task in tasks ?? []"
          :key="task.name"
          :label="task.title"
          icon="i-lucide-sparkles"
          variant="outline"
          class="self-start"
          @click="runTask(task)"
        />
      </template>
      <UButton label="Add an agent" icon="i-lucide-plus" variant="ghost" class="self-start" @click="openPage" />
    </div>
  </aside>
</template>
