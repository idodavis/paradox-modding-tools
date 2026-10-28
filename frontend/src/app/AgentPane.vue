<!--
  The agent pane, docked at the right of the main window's workspace pages or alone in a window of its own: a session,
  with the session menu, the files it edited and its own buttons in its header, and over the whole window on ⤢. With
  no session it offers the installed agents, the recent sessions to resume and the starter tasks.
-->
<script setup lang="ts">
import type { DropdownMenuItem } from "@nuxt/ui";
import { useQuery } from "@pinia/colada";
import { until, useEventBus } from "@vueuse/core";
import { computed, nextTick, shallowRef, useTemplateRef } from "vue";

import type { Task } from "@services/mcp/models";
import { abbreviate, describeAge, plural } from "../lib/format";
import { agentsQuery, tasksQuery } from "../queries";
import { describeTitle, useAgentStore, type SessionRecord } from "../stores/agent";
import { THIS_WINDOW } from "../stores/windows";
import AgentSession from "../ui/AgentSession.vue";

// whether the pane is a window of its own, which has no pane to close or pop out
const IS_WINDOW = THIS_WINDOW === "agent";

const agent = useAgentStore();
const commands = useEventBus<string>("pmt.command");
const { data: agents } = useQuery(agentsQuery);
const { data: tasks } = useQuery(tasksQuery);
const field = useTemplateRef("field");
// whether the session shows over the whole window, and the title being typed while it is renamed
const isFullscreen = shallowRef(false);
const draft = shallowRef<string | null>(null);

const session = computed(() => agent.active);
const ready = computed(() => (agents.value ?? []).filter((row) => row.program));
const changes = computed(() => (session.value ? agent.listChanges(session.value) : []));
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
 * The session menu: the shown agent's sessions, each with Rename, Archive and Delete, and its archived ones in a menu
 * of their own; then a new session, another agent and the agents' settings; then what the session speaks.
 */
const menu = computed(() => {
  const shown = session.value;
  const own = agent.records.filter((record) => record.agent === shown?.agent.id);
  const listed = own.filter((record) => !record.isArchived);
  const archived = own.filter((record) => record.isArchived);
  const toItem = (record: SessionRecord) => ({
    label: describeTitle(record),
    slot: "session" as const,
    record,
    onSelect: () => agent.open(record),
  });
  return [
    shown && listed.length
      ? [{ type: "label" as const, label: `Sessions with ${shown.agent.name}` }, ...listed.map(toItem)]
      : [],
    [
      ...(archived.length
        ? [{ label: `Archived (${archived.length})`, icon: "i-lucide-archive", children: archived.map(toItem) }]
        : []),
      ...(shown ? [{ label: "New session", icon: "i-lucide-plus", onSelect: () => agent.start(shown.agent.id) }] : []),
      {
        label: "Another agent",
        icon: "i-lucide-bot",
        children: ready.value.map((row) => ({ label: row.name, onSelect: () => agent.start(row.id) })),
      },
      { label: "Agents and tools", icon: "i-lucide-settings", onSelect: openSettings },
    ],
    shown
      ? [
          {
            type: "label" as const,
            label: `ACP version ${shown.protocol} · ${plural(agent.sessions.length, "session")} running here`,
          },
        ]
      : [],
  ] satisfies DropdownMenuItem[][];
});

/** openSettings shows Settings at the agents' section, where agents are installed and updated. */
function openSettings(): void {
  commands.emit("settings.open", "agents");
}

/** startRename shows a session, the shown one unless one is given, with its title ready to be typed over. */
async function startRename(record?: SessionRecord): Promise<void> {
  if (record) agent.open(record);
  draft.value = record?.title ?? session.value?.title ?? "";
  await nextTick();
  field.value?.inputRef?.select();
}

/** endRename gives the shown session the title typed, unless the rename is cancelled. */
function endRename(isKept: boolean): void {
  if (isKept && session.value?.id && draft.value !== null) agent.rename(session.value.id, draft.value);
  draft.value = null;
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
  <aside
    class="flex h-full min-h-0 min-w-0 flex-1 flex-col bg-default"
    :class="!IS_WINDOW && 'border-s border-default'"
    aria-label="Agent pane"
  >
    <div class="flex h-10 shrink-0 items-center gap-1 border-b border-default ps-2.5 pe-1.5">
      <template v-if="session">
        <UAvatar :text="abbreviate(session.agent.name)" size="xs" class="rounded-sm" />
        <!-- Enter keeps the title typed and Escape drops it; leaving the field keeps it too -->
        <form
          v-if="draft !== null"
          class="min-w-0 grow"
          @submit.prevent="endRename(true)"
          @keydown.escape="endRename(false)"
        >
          <UInput
            ref="field"
            v-model="draft"
            size="sm"
            aria-label="The session's title"
            class="w-full"
            @blur="endRename(true)"
          />
        </form>
        <UDropdownMenu v-else :items="menu" :content="{ align: 'start' }">
          <!-- a double-click renames the session; the whole title shows on hover when the label is cut -->
          <span class="flex min-w-0" :title="session.title" @dblclick="session.id && startRename()">
            <UButton
              :label="describeTitle(session)"
              trailing-icon="i-lucide-chevron-down"
              variant="ghost"
              size="sm"
              class="min-w-0 font-semibold"
              :ui="{ label: 'truncate', trailingIcon: 'text-dimmed' }"
            />
          </span>
          <template #session-label="{ item }">
            <span class="truncate" :title="item.record.title">{{ item.label }}</span>
          </template>
          <!-- the buttons keep their pointer to themselves, so the item does not open the session too -->
          <template #session-trailing="{ item }">
            <span class="ms-auto flex" @pointerdown.stop @pointerup.stop @click.stop>
              <UButton
                icon="i-lucide-pencil"
                variant="ghost"
                size="xs"
                square
                aria-label="Rename"
                @click="startRename(item.record)"
              />
              <UButton
                :icon="item.record.isArchived ? 'i-lucide-archive-restore' : 'i-lucide-archive'"
                variant="ghost"
                size="xs"
                square
                :aria-label="item.record.isArchived ? 'Restore' : 'Archive'"
                @click="agent.archive(item.record, !item.record.isArchived)"
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
      <UPopover v-if="session && changes.length" :content="{ align: 'end' }">
        <UButton
          :label="`${plural(changes.length, 'file')} edited`"
          icon="i-lucide-file-pen"
          variant="ghost"
          size="xs"
        />
        <template #content>
          <div class="flex w-80 flex-col gap-1.5 p-3">
            <span class="text-11 font-semibold tracking-wide text-dimmed uppercase">Files this session edited</span>
            <span v-for="change in changes" :key="change.path" class="truncate font-mono text-11" :title="change.path">
              {{ change.label }}
            </span>
            <UButton
              v-if="agent.isUndoable(session)"
              label="Undo all edits"
              icon="i-lucide-undo-2"
              color="error"
              variant="soft"
              size="sm"
              class="mt-1 self-start"
              @click="agent.undoAll(session)"
            />
          </div>
        </template>
      </UPopover>
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
      <template v-if="!IS_WINDOW">
        <UTooltip v-if="session" text="Show over the whole window">
          <UButton
            icon="i-lucide-maximize-2"
            variant="ghost"
            size="sm"
            square
            aria-label="Show over the whole window"
            @click="isFullscreen = true"
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
      </template>
    </div>

    <AgentSession v-if="session && !isFullscreen" :key="session.key" :session="session" :narrow="!IS_WINDOW" />
    <div v-else-if="!session" class="flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto px-3.5 py-4">
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
          :title="record.title"
          @click="agent.open(record)"
        >
          <UIcon name="i-lucide-message-circle" class="size-3.5 shrink-0 text-dimmed" />
          <span class="flex min-w-0 grow flex-col">
            <span class="truncate">{{ describeTitle(record) }}</span>
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
      <UButton
        label="Set up agents"
        icon="i-lucide-settings"
        variant="ghost"
        class="self-start"
        @click="openSettings"
      />
    </div>

    <UModal
      v-model:open="isFullscreen"
      :title="session ? describeTitle(session) : ''"
      fullscreen
      :ui="{ body: 'flex min-h-0 flex-1 flex-col p-0 sm:p-0' }"
    >
      <template #body>
        <AgentSession v-if="session" :key="session.key" :session="session" />
      </template>
    </UModal>
  </aside>
</template>
