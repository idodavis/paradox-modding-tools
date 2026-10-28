<!--
  Settings' Agents and tools: the port and token of PMT's tools for coding agents, the text that registers them with
  each agent run outside PMT, and the agents of the user's own.
-->
<script setup lang="ts">
import { useMutation, useQuery, useQueryCache } from "@pinia/colada";
import { Clipboard } from "@wailsio/runtime";
import { computed, shallowRef } from "vue";

import * as agentApi from "@services/agent/service";
import * as mcp from "@services/mcp/service";
import { accessQuery, agentsQuery, keys } from "../../queries";
import FileSelector from "../../ui/FileSelector.vue";
import SettingsSection from "../../ui/SettingsSection.vue";

// a register text shows the token as dots; Copy copies it whole
const DOTS = "••••••••";
// where Codex reads the register text from, since it is a config entry, not a command
const NOTES: Record<string, string> = { "codex-acp": "# add to ~/.codex/config.toml" };

const toast = useToast();
const cache = useQueryCache();
const { data: access } = useQuery(accessQuery);
const { data: agents } = useQuery(agentsQuery);
const name = shallowRef("");
const command = shallowRef("");

const offers = computed(() => (agents.value ?? []).filter((row) => row.register));
const own = computed(() => (agents.value ?? []).filter((row) => !row.version && row.program));

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
      <p class="text-13 text-dimmed">PMT's tools for coding agents, and the agents you run yourself.</p>
    </div>
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
        after New token you register again. Sessions in the Agents page need none of this.
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
        <UInput v-model="name" placeholder="Its name in the Agents list" class="w-56" />
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
