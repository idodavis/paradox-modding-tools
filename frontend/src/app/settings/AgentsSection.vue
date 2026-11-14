<!-- Settings' Agents and tools section: the agents the pane runs, and PMT's tools for coding agents. -->
<script lang="ts">
import { shallowRef } from "vue";

// Outlasts this section being closed and opened again.
const tokenChanged = shallowRef(false);
</script>

<script setup lang="ts">
import { useMutation, useQuery, useQueryCache } from "@pinia/colada";
import { useIntervalFn, useNow } from "@vueuse/core";
import { computed } from "vue";

import type { Agent } from "@services/agent/models";
import * as agentApi from "@services/agent/service";
import * as mcp from "@services/mcp/service";
import { confirm, copyText } from "../../lib/feedback";
import { abbreviate, parseTime, splitError } from "../../lib/format";
import { accessQuery, agentsQuery, keys, nodeQuery, toolsQuery } from "../../queries";
import { ALLOWED, FILE_KINDS, toPmtTool, useAgentStore } from "../../stores/agent";
import { useCompletionStore } from "../../stores/completion";
import { useSettingsStore } from "../../stores/settings";
import AgentLink from "../../ui/AgentLink.vue";
import AgentSignIn from "../../ui/AgentSignIn.vue";
import FileSelector from "../../ui/FileSelector.vue";
import SettingsSection from "../../ui/SettingsSection.vue";

/** DOTS replaces the token in the register text shown. */
const DOTS = "••••••••";
/** NOTES holds the line shown above an agent's register text, by agent id. */
const NOTES: Record<string, string> = { "codex-acp": "# add to ~/.codex/config.toml" };

const cache = useQueryCache();
const agent = useAgentStore();
const completion = useCompletionStore();
const settings = useSettingsStore();
const { data: access } = useQuery(accessQuery);
const { data: agents } = useQuery(agentsQuery);
const { data: node } = useQuery(nodeQuery);
const { data: tools } = useQuery(toolsQuery);
const name = shallowRef("");
const command = shallowRef("");
const now = useNow({ scheduler: (tick) => useIntervalFn(tick, 1000) });

// An agent that another runs for chat is a part of that agent's card, not a
// card of its own.
const offered = computed(() => {
  const rows = (agents.value ?? []).filter((row) => row.version);

  return rows
    .filter((row) => !rows.some((other) => other.chat === row.id))
    .map((row) => {
      const chat = rows.find((other) => other.id === row.chat);
      if (!chat) return { row, parts: [makePart("", row)] };

      return { row, parts: [makePart("Tab completion", row), makePart("Chat", chat, "Add chat")] };
    });
});
const registrable = computed(() => (agents.value ?? []).filter((row) => row.register));
const own = computed(() =>
  (agents.value ?? [])
    .filter((row) => !row.version && row.program)
    .map((row) => ({ row, text: [row.program ?? "", ...(row.args ?? [])].map(quote).join(" ") })),
);

const readers = computed(() => (tools.value ?? []).filter((row) => row.readOnly));
// Each entry of the setting is the agent's id, a colon, and a kind of file work or the tool as the agent named it.
const permits = computed(() =>
  settings
    .value(ALLOWED)
    .split(",")
    .filter(Boolean)
    .map((permit) => {
      const agentId = permit.slice(0, permit.indexOf(":"));
      const title = permit.slice(permit.indexOf(":") + 1);
      const tool = tools.value?.find((row) => row.name === toPmtTool(title));
      return {
        permit,
        agentName: agents.value?.find((row) => row.id === agentId)?.name ?? agentId,
        toolTitle: FILE_KINDS[title] ?? tool?.title ?? title,
      };
    }),
);

const refresh = () => cache.invalidateQueries({ key: keys.agents });
const {
  mutate: install,
  isLoading: installing,
  variables: requested,
  error: installError,
} = useMutation({
  mutation: (id: string) => agentApi.InstallAgent(id),
  onSettled: refresh,
});
const installFailure = computed(() => (installError.value ? splitError(installError.value) : undefined));
const {
  mutate: installNode,
  isLoading: downloading,
  error: nodeError,
} = useMutation({ mutation: () => agentApi.InstallNode(), onSettled: refresh });

const { mutate: renewToken } = useMutation({
  mutation: () => mcp.CreateToken(),
  onSuccess: () => {
    void cache.invalidateQueries({ key: accessQuery.key });
    void cache.invalidateQueries({ key: keys.agents });
    tokenChanged.value = true;
  },
});
const { mutate: add } = useMutation({
  mutation: () => {
    const [program = "", ...args] = splitCommand(command.value);
    return agentApi.AddAgent({ id: "", name: name.value.trim(), program, args });
  },
  onSuccess: () => {
    name.value = "";
    command.value = "";
  },
});
const { mutate: remove } = useMutation({ mutation: (id: string) => agentApi.RemoveAgent(id) });
const { mutate: uninstall } = useMutation({
  mutation: async ({ title, row }: ReturnType<typeof makePart>) => {
    // A part of a card is named by its agent and its title.
    const subject = title ? `${row.name}'s ${title.toLowerCase()}` : row.name;
    const confirmed = await confirm.open({
      title: `Uninstall ${subject}?`,
      body: `${subject} is deleted from PMT's folder. You can install it again.`,
      confirm: "Uninstall",
      destructive: true,
    });
    if (!confirmed) return;

    await agentApi.UninstallAgent(row.id);
  },
  onSettled: refresh,
});

/**
 * makePart returns one part of an agent's card: its line and the button that
 * installs or updates it, if it needs one. The button reads Install while the
 * part is missing, or the given words.
 */
function makePart(title: string, row: Agent, missingLabel = "Install") {
  const button = !row.installed ? missingLabel : row.installed !== row.version ? `Update to ${row.version}` : undefined;

  return { title, row, line: describeAgent(row), button };
}

/**
 * describeAgent returns the line under an agent's name: its install progress,
 * the state of its completion or of its sessions, or its install.
 */
function describeAgent(row: Agent): { text: string; dot: string } {
  if (isInstalling(row)) {
    const started = parseTime(row.installing) ?? now.value;
    // The clock ticks once a second, so it can read just before the start.
    const seconds = Math.max(0, Math.round((now.value.getTime() - started.getTime()) / 1000));

    return { text: `${row.installed ? "Updating to" : "Installing"} ${row.version} with npm · ${seconds} s`, dot: "" };
  }

  if (row.completion && completion.state === "sign-in") return { text: "Needs sign-in", dot: "bg-warning" };
  if (row.completion && completion.state === "ready") return { text: "Signed in", dot: "bg-success" };
  if (row.completion && completion.state === "error") return { text: completion.error, dot: "bg-error" };
  const live = agent.sessions.filter((session) => session.agent.id === row.id);
  if (live.some((session) => session.state === "sign-in")) return { text: "Needs sign-in", dot: "bg-warning" };
  if (live.some((session) => session.state === "idle" || session.state === "running"))
    return { text: "Signed in", dot: "bg-success" };
  if (!row.installed) return { text: `Not installed; version ${row.version} is available`, dot: "" };
  return row.installed === row.version
    ? { text: `Version ${row.installed}, installed`, dot: "" }
    : { text: `Version ${row.installed}; ${row.version} is out`, dot: "bg-primary" };
}

/** isRunning reports whether one of an agent's sessions has its program running here, which keeps its files. */
function isRunning(row: Agent): boolean {
  return agent.sessions.some((session) => session.agent.id === row.id && session.state !== "ended");
}

/** isInstalling reports whether npm is installing an agent, as Go's list says or this view just asked. */
function isInstalling(row: Agent): boolean {
  return !!parseTime(row.installing) || (installing.value && requested.value === row.id);
}

/** mask returns a register text with the token shown as dots. */
function mask(text: string): string {
  return access.value?.token ? text.replaceAll(access.value.token, DOTS) : text;
}

/** splitCommand splits a command line into its program and arguments. */
function splitCommand(line: string): string[] {
  return [...line.matchAll(/"([^"]*)"|(\S+)/g)].map((match) => match[1] ?? match[2] ?? "");
}

/** quote returns a path as a command line writes it. */
function quote(path: string): string {
  return path.includes(" ") ? `"${path}"` : path;
}
</script>

<template>
  <section class="flex flex-col gap-2.5 border-b border-accented pb-5">
    <h2 class="text-13 font-semibold">Agents</h2>
    <p class="text-12 text-dimmed">
      PMT installs each from the Agent Client Protocol's registry or from npm into its own folder; nothing changes on
      your system. Each signs in to its own account when a session first needs it, and PMT never sees it.
    </p>
    <p class="text-12 text-dimmed">
      Tab completion in the editor comes from GitHub Copilot only. Claude and Codex chat but don't complete code as you
      type. Copilot's free plan is enough for completion beside any agent.
    </p>
    <div class="flex items-center gap-3 rounded-md border border-default bg-muted px-3 py-2.5">
      <UIcon
        :name="node?.path ? 'i-lucide-circle-check' : 'i-lucide-circle-alert'"
        :class="node?.path ? 'text-success' : 'text-warning'"
        class="size-5 shrink-0"
      />
      <span class="flex min-w-0 grow flex-col gap-0.5">
        <span class="text-13 font-medium">Node</span>
        <span v-if="node?.path" class="text-12 text-dimmed">
          {{ node.own ? "Node downloaded by PMT into its own folder." : `Your system's Node ${node.system}.` }}
        </span>
        <span v-else class="text-12 text-toned">
          {{ node?.system ? `Your system has Node ${node.system}.` : "Your system has no Node." }} These agents need 22
          or newer. Download Node needs no admin rights: it goes into PMT's folder, checked against nodejs.org's
          published sums.
        </span>
        <span v-if="nodeError" class="text-12 text-error">{{ nodeError.message }}</span>
      </span>
      <UButton v-if="!node?.path" label="Download Node" :loading="downloading" @click="installNode()" />
    </div>
    <div
      v-for="{ row, parts } in offered"
      :key="row.id"
      class="flex flex-col gap-2.5 rounded-md border border-default px-3 py-2.5"
    >
      <div class="flex items-center gap-3">
        <UAvatar :text="abbreviate(row.name)" size="sm" class="rounded-md" />
        <span class="text-13 font-medium">{{ row.name }}</span>
        <span v-if="!row.completion" class="text-12 text-dimmed">Chat only</span>
      </div>
      <template v-for="part in parts" :key="part.row.id">
        <div class="flex flex-wrap items-center gap-3 ps-9">
          <span class="flex min-w-0 grow flex-col gap-0.5">
            <span v-if="part.title" class="text-12 font-medium">{{ part.title }}</span>
            <span class="flex items-center gap-1.5 text-12 text-dimmed">
              <span v-if="part.line.dot" class="size-1.5 rounded-full" :class="part.line.dot" />{{ part.line.text }}
            </span>
          </span>
          <UButton
            v-if="part.button"
            :label="part.button"
            :disabled="!node?.path"
            :loading="isInstalling(part.row)"
            @click="install(part.row.id)"
          />
          <UButton
            v-if="part.row.completion && completion.state === 'error'"
            label="Try again"
            icon="i-lucide-refresh-cw"
            @click="completion.restart()"
          />
          <UTooltip v-if="part.row.installed" :text="isRunning(part.row) ? 'Close its sessions first' : undefined">
            <UButton
              label="Uninstall"
              icon="i-lucide-trash-2"
              color="error"
              variant="ghost"
              :disabled="isInstalling(part.row) || isRunning(part.row)"
              @click="uninstall(part)"
            />
          </UTooltip>
        </div>
        <div v-if="part.row.completion && completion.state === 'sign-in'" class="flex flex-col gap-2.5 ps-9">
          <AgentSignIn :methods="completion.methods" @choose="completion.signIn" @done="completion.restart()" />
        </div>
        <AgentLink
          v-if="part.row.completion && completion.link"
          :agent="row.name"
          :message="completion.link.message"
          :url="completion.link.url"
          class="ms-9"
          @answer="completion.answer"
        />
      </template>
      <UAlert
        v-if="installFailure && parts.some((part) => requested === part.row.id)"
        color="error"
        variant="subtle"
        icon="i-lucide-circle-x"
        orientation="horizontal"
        :title="installFailure[0]"
        :actions="[
          {
            label: 'Copy',
            icon: 'i-lucide-copy',
            color: 'neutral',
            variant: 'outline',
            onClick: () => copyText(installError?.message ?? '', 'The error'),
          },
        ]"
      >
        <template v-if="installFailure[1]" #description>
          <pre class="max-h-40 overflow-y-auto font-mono text-11 leading-4.5 whitespace-pre-wrap">{{
            installFailure[1]
          }}</pre>
        </template>
      </UAlert>
    </div>
  </section>
  <SettingsSection section="agents" />
  <section id="setting-agents.allowed" class="flex flex-col gap-2.5 border-b border-accented pb-5">
    <h2 class="text-13 font-semibold">Allowed without asking</h2>
    <p class="text-12 text-dimmed">
      PMT allows its tools that only read, so an agent never waits on you for them. A tool you chose Always allow for is
      added here until you remove it.
    </p>
    <div class="flex flex-wrap gap-1.5">
      <UBadge v-for="tool in readers" :key="tool.name" :label="tool.title" color="neutral" variant="subtle" />
    </div>
    <div v-for="row in permits" :key="row.permit" class="flex items-center gap-3 border-b border-accented py-2">
      <span class="w-48 truncate text-13 font-medium">{{ row.agentName }}</span>
      <span class="min-w-0 grow truncate text-12 text-toned">{{ row.toolTitle }}</span>
      <UButton
        label="Remove"
        icon="i-lucide-trash-2"
        color="error"
        variant="ghost"
        size="sm"
        @click="settings.writeItem(ALLOWED, row.permit, false)"
      />
    </div>
  </section>
  <section class="grid grid-cols-[260px_minmax(0,1fr)] gap-8 border-b border-accented pb-5">
    <div class="min-w-0">
      <h2 class="text-13 font-semibold">Token</h2>
      <p class="mt-0.75 text-12 leading-4.5 text-dimmed">
        Kept in PMT's config folder. An agent sends it with every request.
      </p>
    </div>
    <div class="flex items-center gap-2 self-start">
      <UInput :model-value="DOTS + DOTS" readonly class="w-48 font-mono" />
      <UButton label="Copy" icon="i-lucide-copy" @click="copyText(access?.token ?? '', 'The token')" />
      <UButton label="New token" icon="i-lucide-refresh-cw" variant="ghost" @click="renewToken()" />
    </div>
  </section>
  <section class="flex flex-col gap-2.5 border-b border-accented pb-5">
    <h2 class="text-13 font-semibold">Register PMT with an agent you run yourself</h2>
    <p class="text-12 text-dimmed">
      Once for each agent, in any terminal or its own settings file. Each carries this computer's port and token, so
      after New token you register again. Sessions in the agent pane need none of this.
    </p>
    <div v-for="row in registrable" :key="row.id" class="flex flex-col gap-1">
      <span class="text-12 font-medium">{{ row.name }}</span>
      <div class="flex items-start gap-2 rounded-md border border-default bg-muted px-3 py-2">
        <pre class="min-w-0 grow font-mono text-11 leading-4.5 whitespace-pre-wrap text-toned"><span
              v-if="NOTES[row.id]"
              class="text-dimmed"
            >{{ NOTES[row.id] }}
</span>{{ mask(row.register ?? "") }}</pre>
        <UButton
          icon="i-lucide-copy"
          variant="ghost"
          size="xs"
          square
          :aria-label="`Copy ${row.name}'s text`"
          @click="copyText(row.register ?? '', `${row.name}'s text`)"
        />
      </div>
    </div>
    <p v-if="tokenChanged" class="text-12 text-warning">
      The token changed: register PMT again with each agent you run yourself.
    </p>
  </section>
  <section class="flex flex-col gap-2.5">
    <h2 class="text-13 font-semibold">Your own agents</h2>
    <p class="text-12 text-dimmed">
      Agents PMT does not list, started with your own command. Each must speak the Agent Client Protocol on its input
      and output.
    </p>
    <div v-for="{ row, text } in own" :key="row.id" class="flex items-center gap-3 border-b border-accented py-2">
      <span class="w-48 truncate text-13 font-medium">{{ row.name }}</span>
      <span class="min-w-0 grow truncate font-mono text-11 text-toned">{{ text }}</span>
      <UButton label="Remove" icon="i-lucide-trash-2" color="error" variant="ghost" size="sm" @click="remove(row.id)" />
    </div>
    <div class="flex items-center gap-2">
      <UInput v-model="name" placeholder="Its name in the pane's list" class="w-56" />
      <UInput v-model="command" placeholder="The program and its arguments" class="min-w-0 grow font-mono" />
      <FileSelector path="" :filter="[]" @update:path="command = quote($event)">
        <template #default="{ browse }">
          <UButton label="Browse" @click="browse" />
        </template>
      </FileSelector>
      <UButton label="Add" icon="i-lucide-plus" :disabled="!name.trim() || !command.trim()" @click="add()" />
    </div>
    <p class="text-12 text-dimmed">
      It starts in the folder of the mod that holds the open file, else the workspace's first mod, with the same
      environment as PMT's shells.
    </p>
  </section>
</template>
