<!--
  Settings' Agents and tools: the agents the agent pane runs, with Node and each one's install, the port and token of
  PMT's tools for coding agents, the text that registers them with each agent run outside PMT, and the agents of the
  user's own.
-->
<script setup lang="ts">
import { useMutation, useQuery, useQueryCache } from "@pinia/colada";
import { useIntervalFn, useNow } from "@vueuse/core";
import { Clipboard } from "@wailsio/runtime";
import { computed, shallowRef } from "vue";

import type { Agent } from "@services/agent/models";
import * as agentApi from "@services/agent/service";
import * as mcp from "@services/mcp/service";
import { abbreviate, parseTime, splitError } from "../../lib/format";
import { accessQuery, agentsQuery, keys, nodeQuery } from "../../queries";
import { useAgentStore } from "../../stores/agent";
import FileSelector from "../../ui/FileSelector.vue";
import SettingsSection from "../../ui/SettingsSection.vue";

// a register text shows the token as dots; Copy copies it whole
const DOTS = "••••••••";
// where Codex reads the register text from, since it is a config entry, not a command
const NOTES: Record<string, string> = { "codex-acp": "# add to ~/.codex/config.toml" };

const toast = useToast();
const cache = useQueryCache();
const agent = useAgentStore();
const { data: access } = useQuery(accessQuery);
const { data: agents } = useQuery(agentsQuery);
const { data: node } = useQuery(nodeQuery);
const name = shallowRef("");
const command = shallowRef("");
// the clock an install's line counts its seconds by
const now = useNow({ scheduler: (tick) => useIntervalFn(tick, 1000) });

// the agents PMT offers, each with the line that says how it stands
const offered = computed(() =>
  (agents.value ?? []).filter((row) => row.version).map((row) => ({ row, line: describeAgent(row) })),
);
const offers = computed(() => (agents.value ?? []).filter((row) => row.register));
const own = computed(() => (agents.value ?? []).filter((row) => !row.version && row.program));

const refresh = () => cache.invalidateQueries({ key: keys.agents });
const {
  mutate: install,
  isLoading: installing,
  variables: installed,
  error: installError,
} = useMutation({ mutation: (id: string) => agentApi.InstallAgent(id), onSettled: refresh });
// a failed install's error: its first line, the alert's title, and npm's output after it
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
    toast.add({ title: "New token made", description: "Register PMT again with each agent you run yourself." });
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

/**
 * describeAgent returns the line under an agent's name: its install and the seconds since it started while npm runs
 * it, else how its sessions here stand, else whether it is installed.
 */
function describeAgent(row: Agent): { text: string; dot: string } {
  if (isInstalling(row)) {
    const started = parseTime(row.installing) ?? now.value;
    // the clock ticks once a second, so it can read just before the start
    const seconds = Math.max(0, Math.round((now.value.getTime() - started.getTime()) / 1000));
    return { text: `${row.installed ? "Updating to" : "Installing"} ${row.version} with npm · ${seconds} s`, dot: "" };
  }
  const live = agent.sessions.filter((one) => one.agent.id === row.id);
  if (live.some((one) => one.state === "sign-in")) return { text: "Needs sign-in", dot: "bg-warning" };
  if (live.some((one) => one.state === "idle" || one.state === "running"))
    return { text: "Signed in", dot: "bg-success" };
  if (!row.installed) return { text: `Not installed; version ${row.version} from the registry`, dot: "" };
  return row.installed === row.version
    ? { text: `Version ${row.installed}, installed`, dot: "" }
    : { text: `Version ${row.installed}; ${row.version} is out`, dot: "bg-primary" };
}

/**
 * isInstalling reports whether npm is installing an agent: Go's list says so, or this view asked for the install and
 * Go's list has not come back since.
 */
function isInstalling(row: Agent): boolean {
  return !!parseTime(row.installing) || (installing.value && installed.value === row.id);
}

/** copy puts a text on the clipboard and says so. */
async function copy(text: string, what: string): Promise<void> {
  await Clipboard.SetText(text);
  toast.add({ title: `${what} copied`, color: "success" });
}

/** mask returns a register text with the token shown as dots. */
function mask(text: string): string {
  return access.value?.token ? text.replaceAll(access.value.token, DOTS) : text;
}

/** splitCommand splits a command line into its program and arguments; double quotes keep spaces inside one. */
function splitCommand(line: string): string[] {
  return [...line.matchAll(/"([^"]*)"|(\S+)/g)].map((match) => match[1] ?? match[2] ?? "");
}

/** quote returns a path as a command line writes it, quoted when it holds a space. */
function quote(path: string): string {
  return path.includes(" ") ? `"${path}"` : path;
}
</script>

<template>
  <div class="flex h-full flex-col gap-4 overflow-y-auto px-8 py-6">
    <div class="flex flex-col gap-0.5">
      <h1 class="text-20">Agents and tools</h1>
      <p class="text-13 text-dimmed">
        The agents the agent pane runs, PMT's tools for coding agents, and the agents you run yourself.
      </p>
    </div>
    <section class="flex flex-col gap-2.5 border-b border-accented pb-5">
      <h2 class="text-13 font-semibold">Agents</h2>
      <p class="text-12 text-dimmed">
        PMT installs each from the Agent Client Protocol's registry into its own folder; nothing changes on your system.
        Each signs in to its own account when a session first needs it, and PMT never sees it.
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
            {{ node?.system ? `Your system has Node ${node.system}.` : "Your system has no Node." }} These agents need
            22 or newer. Download Node needs no admin rights: it goes into PMT's folder, checked against nodejs.org's
            published sums.
          </span>
          <span v-if="nodeError" class="text-12 text-error">{{ nodeError.message }}</span>
        </span>
        <UButton v-if="!node?.path" label="Download Node" :loading="downloading" @click="installNode()" />
      </div>
      <div
        v-for="{ row, line } in offered"
        :key="row.id"
        class="flex flex-wrap items-center gap-3 rounded-md border border-default px-3 py-2.5"
      >
        <UAvatar :text="abbreviate(row.name)" size="sm" class="rounded-md" />
        <span class="flex min-w-0 grow flex-col gap-0.5">
          <span class="text-13 font-medium">{{ row.name }}</span>
          <span class="flex items-center gap-1.5 text-12 text-dimmed">
            <span v-if="line.dot" class="size-1.5 rounded-full" :class="line.dot" />{{ line.text }}
          </span>
        </span>
        <UButton
          v-if="row.installed !== row.version"
          :label="row.installed ? `Update to ${row.version}` : 'Install'"
          :disabled="!node?.path"
          :loading="isInstalling(row)"
          @click="install(row.id)"
        />
        <UAlert
          v-if="installFailure && installed === row.id"
          class="basis-full"
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
              onClick: () => copy(installError?.message ?? '', 'The error'),
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
    <section class="grid grid-cols-[260px_minmax(0,1fr)] gap-8 border-b border-accented pb-5">
      <div class="min-w-0">
        <h2 class="text-13 font-semibold">Token</h2>
        <p class="mt-0.75 text-12 leading-4.5 text-dimmed">
          Kept in PMT's config folder. An agent sends it with every request.
        </p>
      </div>
      <div class="flex items-center gap-2 self-start">
        <UInput :model-value="DOTS + DOTS" readonly class="w-48 font-mono" />
        <UButton label="Copy" icon="i-lucide-copy" @click="copy(access?.token ?? '', 'The token')" />
        <UButton label="New token" icon="i-lucide-refresh-cw" variant="ghost" @click="renewToken()" />
      </div>
    </section>
    <section class="flex flex-col gap-2.5 border-b border-accented pb-5">
      <h2 class="text-13 font-semibold">Register PMT with an agent you run yourself</h2>
      <p class="text-12 text-dimmed">
        Once for each agent, in any terminal or its own settings file. Each carries this computer's port and token, so
        after New token you register again. Sessions in the agent pane need none of this.
      </p>
      <div v-for="row in offers" :key="row.id" class="flex flex-col gap-1">
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
            @click="copy(row.register ?? '', `${row.name}'s text`)"
          />
        </div>
      </div>
    </section>
    <section class="flex flex-col gap-2.5">
      <h2 class="text-13 font-semibold">Your own agents</h2>
      <p class="text-12 text-dimmed">
        Agents PMT does not list, started with your own command. Each must speak the Agent Client Protocol on its input
        and output.
      </p>
      <div v-for="row in own" :key="row.id" class="flex items-center gap-3 border-b border-accented py-2">
        <span class="w-48 truncate text-13 font-medium">{{ row.name }}</span>
        <span class="min-w-0 grow truncate font-mono text-11 text-toned">
          {{ [row.program ?? "", ...(row.args ?? [])].map(quote).join(" ") }}
        </span>
        <UButton
          label="Remove"
          icon="i-lucide-trash-2"
          color="error"
          variant="ghost"
          size="sm"
          @click="remove(row.id)"
        />
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
  </div>
</template>
