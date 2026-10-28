<!--
  The Agents page: the agents PMT offers and the user's own, each one's sessions with the archived ones in a group of
  their own, and the session shown, with the files it touched beside it. Setup installs an agent, and Node when the
  system has none new enough; an installed agent's Manage shows its setup again.
-->
<script setup lang="ts">
import { useMutation, useQuery, useQueryCache } from "@pinia/colada";
import { computed, shallowRef, watch } from "vue";

import * as agentApi from "@services/agent/service";
import type { Agent } from "@services/agent/models";
import { abbreviate, describeAge, plural } from "../../lib/format";
import { agentsQuery, keys, nodeQuery } from "../../queries";
import { useAgentStore } from "../../stores/agent";
import { THIS_WINDOW } from "../../stores/windows";
import AgentSession from "../../ui/AgentSession.vue";
import StatusFacts from "../../ui/StatusFacts.vue";

defineProps<{ id: string }>();

const PANES = [
  { slot: "rail", sizeUnit: "px" as const, defaultSize: 260, minSize: 200, maxSize: 400 },
  { slot: "session" },
  { slot: "aside", sizeUnit: "px" as const, defaultSize: 300, minSize: 220, maxSize: 480, collapsible: true },
];

const agent = useAgentStore();
const cache = useQueryCache();
const { data: agents } = useQuery(agentsQuery);
const { data: node } = useQuery(nodeQuery);
// the agent the rail has picked, and whether its setup is shown in place of its session
const agentId = shallowRef(agent.lastAgent);
const setup = shallowRef(false);
// the Archived group starts closed
const showArchived = shallowRef(false);

const current = computed(() => agents.value?.find((one) => one.id === agentId.value));
const session = computed(() => (agent.active?.agent.id === agentId.value ? agent.active : undefined));
const history = computed(() => agent.records.filter((record) => record.agent === agentId.value));
// the picked agent's sessions, then the Archived group: its toggle, and its sessions while it is open
const lists = computed(() => {
  const archived = history.value.filter((record) => record.isArchived);
  return [
    { id: "sessions", toggle: "", records: history.value.filter((record) => !record.isArchived) },
    {
      id: "archived",
      toggle: archived.length ? `Archived (${archived.length})` : "",
      records: showArchived.value ? archived : [],
    },
  ];
});
const changes = computed(() => (session.value ? agent.listChanges(session.value) : []));
// the rail's agents, each with the line under its name
const rail = computed(() => (agents.value ?? []).map((row) => ({ row, line: describeAgent(row) })));
const facts = computed(() =>
  session.value ? [{ label: session.value.agent.name, value: `ACP v${session.value.protocol}` }] : [],
);

watch(
  agents,
  (rows) => {
    if (rows?.length && !rows.some((one) => one.id === agentId.value)) agentId.value = rows[0]?.id ?? "";
  },
  { immediate: true },
);

const refresh = () => cache.invalidateQueries({ key: keys.agents });
const {
  mutate: install,
  isLoading: installing,
  error: installError,
} = useMutation({
  mutation: (id: string) => agentApi.InstallAgent(id),
  onSettled: refresh,
});
const {
  mutate: installNode,
  isLoading: downloading,
  error: nodeError,
} = useMutation({
  mutation: () => agentApi.InstallNode(),
  onSettled: refresh,
});

/** describeAgent returns the line under an agent's name: whether it is installed, and how its last session went. */
function describeAgent(row: Agent): { text: string; dot: string } {
  const live = agent.sessions.filter((one) => one.agent.id === row.id);
  if (live.some((one) => one.state === "sign-in")) return { text: "Needs sign-in", dot: "bg-warning" };
  if (live.some((one) => one.state === "idle" || one.state === "running"))
    return { text: "Signed in", dot: "bg-success" };
  if (!row.version) return { text: "Your own command", dot: "" };
  return row.installed ? { text: "Installed", dot: "" } : { text: "Not installed", dot: "" };
}

/** choose picks an agent in the rail: its setup while it cannot start, else its session. */
function choose(row: Agent): void {
  agentId.value = row.id;
  setup.value = !row.program;
}

/** manage picks an installed agent in the rail and shows its setup, where a newer version is installed. */
function manage(row: Agent): void {
  agentId.value = row.id;
  setup.value = true;
}

/** begin starts a new session with the agent the rail has picked, and shows it. */
function begin(): void {
  if (!current.value) return;
  setup.value = false;
  agent.start(current.value.id);
}
</script>

<template>
  <USplitter :items="PANES" auto-save-id="pmt.agents.panes" class="h-full" :ui="{ handle: 'mx-0' }">
    <template #rail>
      <nav class="flex h-full min-w-0 flex-1 flex-col gap-0.5 border-e border-default bg-muted p-2">
        <span class="px-2.5 pt-1 pb-2 text-13 font-semibold">Agents</span>
        <div
          v-for="{ row, line } in rail"
          :key="row.id"
          class="group flex items-center rounded-md pe-1 text-13"
          :class="row.id === agentId ? 'bg-elevated' : 'hover:bg-elevated'"
        >
          <button
            type="button"
            class="flex min-w-0 grow items-center gap-2.5 px-2.5 py-1.5 text-start"
            @click="choose(row)"
          >
            <UAvatar :text="abbreviate(row.name)" size="sm" class="rounded-md" />
            <span class="flex min-w-0 grow flex-col">
              <span class="truncate font-medium">{{ row.name }}</span>
              <span class="text-11 text-dimmed">{{ line.text }}</span>
            </span>
            <span v-if="line.dot" class="size-1.5 rounded-full" :class="line.dot" />
          </button>
          <UTooltip v-if="row.installed" text="Manage">
            <UButton
              icon="i-lucide-settings"
              variant="ghost"
              size="xs"
              square
              aria-label="Manage"
              class="opacity-0 group-hover:opacity-100 focus-visible:opacity-100"
              @click="manage(row)"
            />
          </UTooltip>
        </div>
        <template v-if="current && history.length">
          <span class="mt-3 px-2.5 pb-1 text-11 font-semibold text-dimmed">Sessions with {{ current.name }}</span>
          <template v-for="list in lists" :key="list.id">
            <button
              v-if="list.toggle"
              type="button"
              class="mt-1 flex items-center gap-1.5 rounded-md px-2.5 py-1 text-start text-11 text-dimmed hover:bg-elevated"
              :aria-expanded="showArchived"
              @click="showArchived = !showArchived"
            >
              <UIcon :name="showArchived ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'" class="size-3.5" />
              {{ list.toggle }}
            </button>
            <div
              v-for="record in list.records"
              :key="record.id"
              class="group flex items-center rounded-md pe-1 text-12"
              :class="session?.id === record.id ? 'bg-elevated' : 'hover:bg-elevated'"
            >
              <button
                type="button"
                class="flex min-w-0 grow items-center gap-2 px-2.5 py-1.5 text-start"
                @click="agent.open(record)"
              >
                <UIcon name="i-lucide-message-circle" class="size-3.5 shrink-0 text-dimmed" />
                <span class="flex min-w-0 grow flex-col">
                  <span class="truncate">{{ record.title }}</span>
                  <span class="text-11 text-dimmed">{{ describeAge(new Date(record.updatedAt), "short") }}</span>
                </span>
              </button>
              <UTooltip :text="record.isArchived ? 'Restore' : 'Archive'">
                <UButton
                  :icon="record.isArchived ? 'i-lucide-archive-restore' : 'i-lucide-archive'"
                  variant="ghost"
                  size="xs"
                  square
                  :aria-label="record.isArchived ? 'Restore' : 'Archive'"
                  class="opacity-0 group-hover:opacity-100 focus-visible:opacity-100"
                  @click="agent.archive(record, !record.isArchived)"
                />
              </UTooltip>
              <UTooltip text="Delete">
                <UButton
                  icon="i-lucide-trash-2"
                  color="error"
                  variant="ghost"
                  size="xs"
                  square
                  aria-label="Delete"
                  class="opacity-0 group-hover:opacity-100 focus-visible:opacity-100"
                  @click="agent.deleteSession(record)"
                />
              </UTooltip>
            </div>
          </template>
        </template>
        <span class="grow" />
        <p class="px-2.5 py-2 text-11 text-dimmed">
          Every session is given PMT's tools: problems, references, Health and the Event Graph, with your unsaved text.
        </p>
      </nav>
    </template>

    <template #session>
      <section v-if="setup && current" class="flex h-full min-w-0 flex-1 flex-col gap-3.5 overflow-y-auto px-7 py-5.5">
        <div class="flex items-center gap-3">
          <UAvatar :text="abbreviate(current.name)" size="lg" class="rounded-md" />
          <span class="flex flex-col">
            <span class="text-20">{{ current.name }}</span>
            <span class="text-12 text-dimmed">{{ current.description || "Your own command" }}</span>
          </span>
          <span class="grow" />
          <UButton label="Start a session" size="md" :disabled="!current.program" @click="begin" />
        </div>
        <div v-if="current.version" class="flex gap-3 rounded-lg border border-default bg-muted p-3.5">
          <UIcon
            :name="node?.path ? 'i-lucide-circle-check' : 'i-lucide-circle-alert'"
            :class="node?.path ? 'text-success' : 'text-warning'"
            class="size-5 shrink-0"
          />
          <span class="flex grow flex-col gap-1">
            <span class="text-13 font-medium">Node</span>
            <span v-if="node?.path" class="text-12 text-dimmed">
              {{ node.own ? "Node downloaded by PMT into its own folder." : `Your system's Node ${node.system}.` }}
            </span>
            <template v-else>
              <span class="text-12 text-toned">
                {{ node?.system ? `Your system has Node ${node.system}.` : "Your system has no Node." }} These agents
                need 22 or newer.
              </span>
              <span class="flex items-center gap-2">
                <UButton label="Download Node" color="primary" :loading="downloading" @click="installNode()" />
                <span class="text-11 text-dimmed">
                  No admin rights needed. It goes into PMT's folder and is checked against nodejs.org's published sums.
                </span>
              </span>
              <span v-if="nodeError" class="text-12 text-error">{{ nodeError.message }}</span>
            </template>
          </span>
        </div>
        <div v-if="current.version" class="flex gap-3 rounded-lg border border-default bg-muted p-3.5">
          <UIcon
            :name="current.installed ? 'i-lucide-circle-check' : 'i-lucide-circle-dashed'"
            :class="current.installed ? 'text-success' : 'text-dimmed'"
            class="size-5 shrink-0"
          />
          <span class="flex grow flex-col gap-1">
            <span class="text-13 font-medium">The agent</span>
            <span class="text-12 text-dimmed">
              {{
                current.installed
                  ? `Version ${current.installed}, installed from the registry into PMT's folder.`
                  : `Version ${current.version} from the registry, installed into PMT's folder with npm; nothing changes on your system.`
              }}
            </span>
            <UButton
              v-if="current.installed !== current.version"
              :label="current.installed ? `Update to ${current.version}` : 'Install'"
              class="self-start"
              :disabled="!node?.path"
              :loading="installing"
              @click="install(current.id)"
            />
            <span v-if="installError" class="text-12 whitespace-pre-wrap text-error">{{ installError.message }}</span>
          </span>
        </div>
        <div class="flex gap-3 rounded-lg border border-default bg-muted p-3.5">
          <UIcon name="i-lucide-log-in" class="size-5 shrink-0 text-dimmed" />
          <span class="flex grow flex-col gap-1">
            <span class="text-13 font-medium">Sign in to {{ current.name }}</span>
            <span class="text-12 text-dimmed">
              {{ current.name }} signs in to its own account, and PMT never sees it. A new session asks when it needs
              you to.
            </span>
          </span>
        </div>
      </section>
      <section v-else-if="session" class="flex h-full min-w-0 flex-1 flex-col">
        <div class="flex h-12 shrink-0 items-center gap-2.5 border-b border-default ps-5 pe-4">
          <span class="truncate text-13 font-semibold">{{ session.title }}</span>
          <UBadge :label="session.agent.name" variant="soft" color="neutral" size="sm" />
          <span class="grow" />
          <USwitch v-model="agent.follow" label="Follow the agent" size="sm" />
          <UTooltip v-if="THIS_WINDOW === 'main'" text="Open in a new window">
            <UButton
              icon="i-lucide-app-window"
              variant="ghost"
              size="sm"
              square
              aria-label="Open in a new window"
              :disabled="!session.resumable"
              @click="agent.popOut(session)"
            />
          </UTooltip>
          <UButton label="New session" icon="i-lucide-plus" variant="ghost" size="sm" @click="begin" />
        </div>
        <AgentSession :session="session" />
      </section>
      <section v-else class="flex h-full min-w-0 flex-1 flex-col items-center justify-center gap-3 text-13 text-dimmed">
        <template v-if="current?.program">
          <span>No session with {{ current.name }} is open here.</span>
          <UButton :label="`Start a session with ${current.name}`" @click="begin" />
        </template>
        <template v-else-if="current">
          <span>{{ current.name }} is not ready to start.</span>
          <UButton label="Set it up" @click="setup = true" />
        </template>
      </section>
    </template>

    <template #aside>
      <aside class="flex h-full min-w-0 flex-1 flex-col gap-3.5 border-s border-default bg-muted p-4">
        <span class="text-11 font-semibold tracking-wide text-dimmed uppercase">Files this session touched</span>
        <span v-if="!changes.length" class="text-12 text-dimmed">None yet.</span>
        <span v-for="change in changes" :key="change.path" class="truncate font-mono text-11" :title="change.path">
          {{ change.label }}
        </span>
        <UButton
          v-if="session && agent.isUndoable(session)"
          label="Undo all edits"
          icon="i-lucide-undo-2"
          color="error"
          variant="soft"
          size="sm"
          class="self-start"
          @click="agent.undoAll(session)"
        />
        <span class="grow" />
        <span v-if="agent.sessions.length > 1" class="text-11 text-dimmed">
          {{ plural(agent.sessions.length, "session") }} running in this window
        </span>
      </aside>
    </template>
  </USplitter>
  <StatusFacts :facts="facts" />
</template>
