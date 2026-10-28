<!--
  One session with an agent, in a chat palette: its transcript, the requests it waits on, and the prompt box with the
  page's chip, the files added with @, the agent's commands picked with / and the agent's settings.
  The agent pane shows it narrow, and over the whole window or in a window of its own wide.
-->
<script setup lang="ts">
import type {
  AvailableCommand,
  SessionConfigOption,
  SessionConfigSelectGroup,
  SessionConfigSelectOption,
} from "@agentclientprotocol/sdk/experimental/v2";
import type { DropdownMenuItem } from "@nuxt/ui";
import { useQuery } from "@pinia/colada";
import { refDebounced, useTimeoutFn } from "@vueuse/core";
import { Clipboard } from "@wailsio/runtime";
import { uniqBy } from "es-toolkit";
import { computed, shallowRef, useTemplateRef, watch } from "vue";

import type { Place } from "@services/workspace/models";
import { fileSearchQuery, tasksQuery } from "../queries";
import { useAgentStore, type Change, type Entry, type Session } from "../stores/agent";
import CompareView from "./CompareView.vue";

/** A Tool is a tool call's title as the transcript shows it, and PMT's name after it when it is PMT's own. */
type Tool = { text: string; suffix: string };
/** A Url is a link split around its host, and whether the host is written with look-alike letters. */
type Url = { before: string; host: string; after: string; lookalike: boolean };
/** A Part is one entry of the transcript with what its view reads from it. */
type Part = { entry: Entry; tool?: Tool; link?: Url };
/** A SelectOption is one of the agent's settings whose value is one of a list, such as its model. */
type SelectOption = Extract<SessionConfigOption, { type: "select" }>;
/** A Label is the name a group of a setting's values is listed under. */
type Label = { type: "label"; label: string };

const { session, narrow = false } = defineProps<{ session: Session; narrow?: boolean }>();

// the icon and color of a tool call and of a plan's step, by status, and the look of a plan step's text
const STATUSES: Record<string, { icon: string; color: string; text?: string }> = {
  pending: { icon: "i-lucide-circle", color: "text-dimmed" },
  in_progress: { icon: "i-lucide-loader-circle", color: "text-primary", text: "text-highlighted" },
  completed: { icon: "i-lucide-check", color: "text-success", text: "line-through" },
  failed: { icon: "i-lucide-x", color: "text-error" },
  cancelled: { icon: "i-lucide-ban", color: "text-dimmed" },
};
// a permission's options, each drawn by its kind
const OPTIONS: Record<string, { color: "primary" | "neutral"; variant: "solid" | "outline" | "ghost" }> = {
  allow_once: { color: "primary", variant: "solid" },
  allow_always: { color: "neutral", variant: "outline" },
};
// the kinds of setting drawn as a picker each, in this order; the agent's other settings are in one menu
const CATEGORIES = ["model", "thought_level", "mode"];
// the palette's padding around the transcript and the prompt, narrow in the pane and wide elsewhere
const FRAMES = {
  narrow: { content: "px-3.5 py-4", prompt: "border-t-0 px-3 pb-3" },
  wide: { content: "px-7 py-5", prompt: "border-t-0 px-7 pb-4.5" },
};

const agent = useAgentStore();
const { data: tasks } = useQuery(tasksQuery);
const box = useTemplateRef("box");
const text = shallowRef("");
const files = shallowRef<Place[]>([]);
const picking = shallowRef(false);
const pickingCommand = shallowRef(false);
const term = shallowRef("");
const search = refDebounced(term, 150);
const { data: places } = useQuery(() => ({
  ...fileSearchQuery({ query: search.value, limit: 30 }),
  enabled: picking.value,
}));
const comparing = shallowRef<Change | null>(null);
// the answer just copied, whose button shows a tick for a moment
const copied = shallowRef("");
const { start: clearCopied } = useTimeoutFn(() => (copied.value = ""), 1500, { immediate: false });

// each entry is a message of its own, with what its view reads from it worked out once
const messages = computed(() =>
  session.entries.map((entry) => ({
    id: entry.id,
    role: entry.kind === "user" ? "user" : "assistant",
    parts: [
      {
        entry,
        tool: entry.kind === "tool" ? describeTool(entry) : undefined,
        link: entry.kind === "link" ? splitUrl(entry.url) : undefined,
      },
    ],
  })),
);
const status = computed(() => (session.state === "running" ? "streaming" : "ready"));
const busy = computed(() => session.state === "running");
const waiting = computed(() => agent.isWaiting(session));
const empty = computed(() => session.state === "idle" && !session.entries.length);
const placeholder = computed(() =>
  session.commands.length
    ? `Ask ${session.agent.name}, type @ to add a file or / for a command`
    : `Ask ${session.agent.name}, or type @ to add a file`,
);
// the agent's commands, as the command list shows them: each on one line, its description dimmed after its name
const commands = computed(() => [
  {
    id: "commands",
    items: session.commands.map((command) => ({
      label: `/${command.name}`,
      suffix: command.description,
      onSelect: () => pickCommand(command),
    })),
  },
]);
// the agent's main settings, a picker each with its values, and its other settings as one menu lists them
const pickers = computed(() =>
  CATEGORIES.flatMap((category) =>
    session.options.filter(
      (option): option is SelectOption => option.type === "select" && option.category === category,
    ),
  ).map((option) => ({
    option,
    items: listValues(option, (value) => ({ label: value.name, value: value.value, hint: value.description ?? "" })),
  })),
);
const settings = computed(() =>
  session.options
    .filter((option) => option.type === "select" || option.type === "boolean")
    .filter((option) => !pickers.value.some((picker) => picker.option.configId === option.configId))
    .map(describeSetting),
);

// typing @ picks a file, as the File button does, and typing / in an empty box picks one of the agent's commands
watch(text, (next, previous) => {
  if (next.length !== previous.length + 1) return;
  if (next.endsWith("@")) {
    text.value = next.slice(0, -1);
    picking.value = true;
  } else if (next === "/" && session.commands.length) {
    text.value = "";
    pickingCommand.value = true;
  }
});

/** send sends the prompt box's text with the files picked, then empties the box. */
function send(): void {
  const prompt = text.value.trim();
  if (!prompt || session.state !== "idle") return;
  void agent.prompt(session, prompt, files.value);
  text.value = "";
  files.value = [];
}

/** pick adds a file to the next prompt. */
function pick(place: Place): void {
  if (!files.value.some((file) => file.key === place.key)) files.value = [...files.value, place];
  picking.value = false;
  term.value = "";
}

/** pickCommand starts the prompt with one of the agent's commands, for the user to add its input and send. */
function pickCommand(command: AvailableCommand): void {
  text.value = `/${command.name} `;
  pickingCommand.value = false;
}

/** copyAnswer puts an answer of the agent's on the clipboard, and ticks its button for a moment. */
async function copyAnswer(entry: Entry): Promise<void> {
  if (entry.kind !== "agent") return;
  await Clipboard.SetText(entry.text);
  copied.value = entry.id;
  clearCopied();
}

/** focusBox puts the caret back in the prompt box when the command list closes, in place of the Command button. */
function focusBox(event: Event): void {
  event.preventDefault();
  box.value?.textareaRef?.focus();
}

/**
 * listValues returns the values of one of the agent's settings, each made an item by the given function, in their
 * groups, each group led by its name. Values in no group make one group with no name. A value an agent sends twice
 * is listed once.
 */
function listValues<T>(option: SelectOption, toItem: (value: SessionConfigSelectOption) => T): (T | Label)[][] {
  const values = option.options as (SessionConfigSelectOption | SessionConfigSelectGroup)[];
  // a group holds values, and a value holds none; the versions name a group's id apart
  const groups: Pick<SessionConfigSelectGroup, "name" | "options">[] = values.some((value) => "options" in value)
    ? (values as SessionConfigSelectGroup[])
    : [{ name: "", options: values as SessionConfigSelectOption[] }];
  return groups.map((group) => [
    ...(group.name ? [{ type: "label" as const, label: group.name }] : []),
    ...uniqBy(group.options, (value) => value.value).map(toItem),
  ]);
}

/** describeSetting returns one of the agent's other settings as the settings menu lists it: a switch, or its values. */
function describeSetting(option: SessionConfigOption): DropdownMenuItem {
  const configure = (value: string | boolean): void => void agent.configure(session, option.configId, value);
  if (option.type === "boolean")
    return {
      type: "checkbox",
      label: option.name,
      checked: option.currentValue as boolean,
      onUpdateChecked: configure,
    };
  const select = option as SelectOption;
  return {
    label: option.name,
    children: listValues(select, (value) => ({
      type: "checkbox" as const,
      label: value.name,
      checked: value.value === select.currentValue,
      onUpdateChecked: () => configure(value.value),
    })),
  };
}

/** describeTool returns a tool call's title, with PMT's server named apart from the agent's own tools. */
function describeTool(entry: Extract<Entry, { kind: "tool" }>): Tool {
  const own = /^mcp__pmt__|^pmt[_:/]/.exec(entry.title);
  return { text: own ? entry.title.slice(own[0].length) : entry.title || "A tool", suffix: own ? "PMT" : "" };
}

/** splitUrl splits a link into what comes before its host, its host, and the rest, so the host can stand out. */
function splitUrl(url: string): Url {
  try {
    const parsed = new URL(url);
    const start = url.indexOf(parsed.host);
    return {
      before: url.slice(0, start),
      host: parsed.host,
      after: url.slice(start + parsed.host.length),
      lookalike: parsed.hostname.split(".").some((label) => label.startsWith("xn--")),
    };
  } catch {
    return { before: "", host: url, after: "", lookalike: false };
  }
}

/** stripControls returns a terminal's output without its color and cursor codes, for a view that only shows text. */
function stripControls(output: string): string {
  return output.replace(/\u001b\[[0-9;?]*[ -/]*[@-~]|\u001b\][^\u0007]*\u0007/g, "");
}
</script>

<template>
  <UChatPalette :ui="narrow ? FRAMES.narrow : FRAMES.wide">
    <div class="flex flex-col">
      <div v-if="session.state === 'starting'" class="flex items-center gap-2 text-12 text-dimmed">
        <UIcon name="i-lucide-loader-circle" class="size-4 animate-spin" />{{ session.agent.name }} is starting…
      </div>
      <div v-else-if="session.state === 'sign-in'" class="flex max-w-180 flex-col gap-2.5">
        <span class="text-13 font-medium">Sign in to {{ session.agent.name }}</span>
        <span class="text-12 text-toned">
          {{ session.agent.name }} signs in to its own account, and PMT never sees it. A sign-in in PMT's Terminal comes
          back here once it is done.
        </span>
        <button
          v-for="method in session.methods"
          :key="method.id"
          type="button"
          class="flex h-11 items-center gap-2.5 rounded-md border border-accented bg-elevated px-3 text-start"
          @click="agent.signIn(session, method)"
        >
          <UIcon :name="method.command.length ? 'i-lucide-terminal' : 'i-lucide-log-in'" class="size-4 text-toned" />
          <span class="flex min-w-0 grow flex-col">
            <span class="text-12 font-medium">{{ method.name }}</span>
            <span class="truncate text-11 text-dimmed">{{ method.description }}</span>
          </span>
          <UIcon name="i-lucide-chevron-right" class="size-4 text-dimmed" />
        </button>
        <UButton label="I have signed in" icon="i-lucide-refresh-cw" class="self-start" @click="agent.retry(session)" />
      </div>
      <div v-else-if="session.state === 'ended'" class="flex max-w-180 flex-col gap-2.5">
        <span class="text-13 font-medium">The session ended</span>
        <span v-if="session.error" class="text-12 whitespace-pre-wrap text-error">{{ session.error }}</span>
        <UButton label="Start it again" icon="i-lucide-refresh-cw" class="self-start" @click="agent.retry(session)" />
      </div>
      <div v-else-if="empty" class="flex flex-col gap-2">
        <span class="text-12 text-dimmed">Ask {{ session.agent.name }} anything, or start with a task.</span>
        <UButton
          v-for="task in tasks ?? []"
          :key="task.name"
          :label="task.title"
          icon="i-lucide-sparkles"
          variant="outline"
          class="self-start"
          @click="agent.prompt(session, task.text, [])"
        />
      </div>

      <UChatMessages
        v-if="session.entries.length"
        :messages="messages"
        :status="status"
        :should-auto-scroll="true"
        compact
        :user="{ side: 'right', variant: 'soft' }"
        :assistant="{ side: 'left', variant: 'naked' }"
        :ui="{ root: 'gap-3.5' }"
      >
        <template #content="{ message }">
          <template v-for="{ entry, tool, link } in message.parts as Part[]" :key="entry.id">
            <div v-if="entry.kind === 'user'" class="flex flex-col items-end gap-1.5">
              <span class="text-13 leading-5 whitespace-pre-wrap">{{ entry.text }}</span>
              <span
                v-for="chip in entry.chips"
                :key="chip"
                class="flex items-center gap-1 font-mono text-11 text-toned"
              >
                <UIcon name="i-lucide-file" class="size-3 text-dimmed" />{{ chip }}
              </span>
            </div>
            <UEditor
              v-else-if="entry.kind === 'agent'"
              :model-value="entry.text"
              content-type="markdown"
              :editable="false"
              :mention="false"
              :ui="{ base: 'w-full py-0 text-13 [&_code]:text-12 [&_pre]:text-12' }"
              class="max-w-180"
            />
            <UChatReasoning v-else-if="entry.kind === 'thought'" :text="entry.text" :streaming="busy" />
            <UChatTool
              v-else-if="entry.kind === 'tool'"
              :text="tool?.text"
              :suffix="tool?.suffix"
              :icon="STATUSES[entry.status]?.icon ?? 'i-lucide-wrench'"
              :loading="entry.status === 'in_progress'"
              :ui="{ leadingIcon: STATUSES[entry.status]?.color }"
              class="max-w-180"
            >
              <div class="flex flex-col gap-1.5 ps-6 text-12">
                <p v-if="entry.text" class="font-mono text-11 whitespace-pre-wrap text-toned">{{ entry.text }}</p>
                <pre
                  v-for="terminal in entry.terminals"
                  :key="terminal"
                  class="max-h-60 overflow-auto rounded-sm bg-(--bg-0) px-2 py-1.5 font-mono text-11 leading-4.5 text-toned"
                  >{{ stripControls(session.terminals[terminal] ?? "") }}</pre>
                <span v-for="change in entry.changes" :key="change.path" class="flex items-center gap-2">
                  <span class="truncate font-mono text-11">{{ change.label }}</span>
                  <template v-if="change.patch">
                    <span class="text-11 text-success">+{{ change.added }}</span>
                    <span class="text-11 text-error">−{{ change.removed }}</span>
                  </template>
                  <UButton
                    label="Open in compare"
                    icon="i-lucide-git-compare"
                    variant="ghost"
                    size="xs"
                    @click="comparing = change"
                  />
                  <UButton
                    v-if="change.hunks.length"
                    :label="change.isUndone ? 'Undone' : 'Undo'"
                    icon="i-lucide-undo-2"
                    color="error"
                    variant="ghost"
                    size="xs"
                    :disabled="change.isUndone"
                    @click="agent.undo([change])"
                  />
                </span>
              </div>
            </UChatTool>
            <div v-else-if="entry.kind === 'plan'" class="flex max-w-180 flex-col gap-1">
              <span class="text-11 font-semibold tracking-wide text-dimmed uppercase">Plan</span>
              <span
                v-for="(step, index) in entry.steps"
                :key="index"
                class="flex h-6 items-center gap-2 text-12 text-toned"
              >
                <UIcon
                  :name="STATUSES[step.status]?.icon ?? 'i-lucide-circle'"
                  :class="STATUSES[step.status]?.color"
                  class="size-3.5"
                />
                <span :class="STATUSES[step.status]?.text">{{ step.text }}</span>
              </span>
            </div>
            <div
              v-else-if="entry.kind === 'permission'"
              class="flex max-w-180 flex-col gap-2.5 rounded-lg border border-primary bg-(--accent-soft) px-3.5 py-3"
            >
              <span class="flex items-center gap-2.5 text-13 font-medium">
                <UIcon name="i-lucide-shield" class="size-4 text-primary" />{{ entry.title }}
              </span>
              <div class="flex flex-wrap gap-2">
                <UButton
                  v-for="option in entry.options"
                  :key="option.id"
                  :label="option.name"
                  :color="OPTIONS[option.kind]?.color ?? 'neutral'"
                  :variant="OPTIONS[option.kind]?.variant ?? 'ghost'"
                  size="sm"
                  @click="agent.answer(session, entry.id, option.id)"
                />
              </div>
            </div>
            <div
              v-else-if="entry.kind === 'link'"
              class="flex max-w-120 flex-col gap-2 rounded-lg border border-default bg-elevated p-3"
            >
              <span class="flex items-center gap-2 text-12 font-medium">
                {{ session.agent.name }} asks to open a link
                <span class="grow" />
                <UButton
                  icon="i-lucide-x"
                  variant="ghost"
                  size="xs"
                  square
                  aria-label="Close"
                  @click="agent.answer(session, entry.id, null)"
                />
              </span>
              <span class="text-12">{{ entry.message }}</span>
              <span class="font-mono text-11 break-all text-toned">
                {{ link?.before
                }}<span class="rounded-xs bg-(--accent-soft) px-0.5 text-highlighted">{{ link?.host }}</span
                >{{ link?.after }}
              </span>
              <span v-if="link?.lookalike" class="text-12 text-warning">
                The host is written with look-alike letters, so it may not be the site it seems to be.
              </span>
              <div class="flex gap-1.5">
                <UButton
                  :label="link?.lookalike ? 'Open anyway' : 'Open'"
                  icon="i-lucide-external-link"
                  :color="link?.lookalike ? 'warning' : 'primary'"
                  class="grow justify-center"
                  @click="agent.answer(session, entry.id, 'accept')"
                />
                <UButton
                  label="Decline"
                  class="grow justify-center"
                  @click="agent.answer(session, entry.id, 'decline')"
                />
              </div>
              <span class="text-11 text-dimmed">Opens in your browser. Nothing is fetched before you choose.</span>
            </div>
            <p v-else-if="entry.kind === 'notice'" class="text-12 text-dimmed">{{ entry.text }}</p>
          </template>
        </template>
        <!-- an answer of the agent's can be copied; the other entries have no actions -->
        <template #actions="{ message }">
          <UButton
            v-if="(message.parts as Part[])[0]?.entry.kind === 'agent'"
            :icon="copied === message.id ? 'i-lucide-check' : 'i-lucide-copy'"
            variant="ghost"
            color="neutral"
            size="xs"
            square
            aria-label="Copy"
            @click="copyAnswer((message.parts as Part[])[0]!.entry)"
          />
        </template>
      </UChatMessages>
    </div>

    <UModal :open="!!comparing" :title="comparing?.label ?? ''" fullscreen @update:open="comparing = null">
      <template #body>
        <CompareView
          v-if="comparing && comparing.after !== null"
          :left="comparing.before ?? ''"
          :right="comparing.after"
          language="plaintext"
          left-label="Before"
          :right-label="`After ${session.agent.name}'s edit`"
        />
        <pre v-else-if="comparing" class="font-mono text-12 leading-5"><span
            v-for="(line, index) in comparing.patch.split('\n')"
            :key="index"
            :class="line.startsWith('+') ? 'text-success' : line.startsWith('-') ? 'text-error' : 'text-toned'"
          >{{ line }}
</span></pre>
      </template>
    </UModal>

    <template #prompt>
      <UChatPrompt
        ref="box"
        v-model="text"
        :placeholder="placeholder"
        :disabled="session.state !== 'idle' && session.state !== 'running'"
        variant="subtle"
        :rows="2"
        :ui="{ footer: 'flex-wrap' }"
        @submit="send"
      >
        <template #header>
          <div v-if="(agent.chip && !agent.chipLeftOut) || files.length" class="flex flex-wrap gap-1.5">
            <span
              v-if="agent.chip && !agent.chipLeftOut"
              class="flex h-5.5 items-center gap-1.5 rounded-sm bg-accented ps-1.75 pe-1 text-11"
            >
              <UIcon :name="agent.chip.path ? 'i-lucide-file' : 'i-lucide-scan-text'" class="size-3 text-dimmed" />
              <span :class="agent.chip.path && 'font-mono'">{{ agent.chip.label }}</span>
              <span v-if="agent.chip.startLine" class="text-dimmed"
                >{{ agent.chip.startLine }}–{{ agent.chip.endLine }}</span
              >
              <UButton
                icon="i-lucide-x"
                variant="ghost"
                size="xs"
                square
                aria-label="Leave it out"
                @click="agent.chipLeftOut = true"
              />
            </span>
            <span
              v-for="file in files"
              :key="file.key"
              class="flex h-5.5 items-center gap-1.5 rounded-sm bg-accented ps-1.75 pe-1 text-11"
            >
              <UIcon name="i-lucide-file" class="size-3 text-dimmed" />
              <span class="font-mono">{{ file.rel }}</span>
              <UButton
                icon="i-lucide-x"
                variant="ghost"
                size="xs"
                square
                aria-label="Leave it out"
                @click="files = files.filter((other) => other.key !== file.key)"
              />
            </span>
          </div>
        </template>
        <template #footer>
          <UPopover v-model:open="picking" :content="{ align: 'start', side: 'top' }">
            <UButton label="File" icon="i-lucide-at-sign" variant="ghost" size="xs" />
            <template #content>
              <UCommandPalette
                v-model:search-term="term"
                :groups="[
                  {
                    id: 'files',
                    items: (places ?? []).map((place) => ({ label: place.rel, onSelect: () => pick(place) })),
                  },
                ]"
                placeholder="Find a file of the workspace"
                class="h-72 w-96"
              />
            </template>
          </UPopover>
          <UPopover
            v-if="session.commands.length"
            v-model:open="pickingCommand"
            :content="{ align: 'start', side: 'top', onCloseAutoFocus: focusBox }"
          >
            <UButton label="Command" icon="i-lucide-square-slash" variant="ghost" size="xs" />
            <template #content>
              <!-- every command is listed, where the palette would list its first 12 -->
              <UCommandPalette
                :groups="commands"
                :fuse="{ resultLimit: session.commands.length }"
                placeholder="Find a command of the agent"
                class="h-72 w-96"
              />
            </template>
          </UPopover>
          <!-- a picker's list is as wide as its values, each on one line with its description dimmed after it -->
          <USelect
            v-for="{ option, items } in pickers"
            :key="option.configId"
            :model-value="option.currentValue"
            :items="items"
            :aria-label="option.name"
            variant="ghost"
            size="xs"
            :ui="{
              content:
                'w-auto min-w-(--reka-select-trigger-width) max-w-112 max-h-[min(26rem,var(--reka-select-content-available-height,26rem))]',
            }"
            @update:model-value="(value) => agent.configure(session, option.configId, String(value))"
          >
            <template #item-label="{ item }">
              {{ item.label }}
              <span v-if="'hint' in item && item.hint" class="ms-1.5 text-dimmed">{{ item.hint }}</span>
            </template>
          </USelect>
          <UDropdownMenu v-if="settings.length" :items="settings" :content="{ align: 'start', side: 'top' }">
            <UButton icon="i-lucide-sliders-horizontal" variant="ghost" size="xs" square aria-label="More settings" />
          </UDropdownMenu>
          <USwitch v-if="narrow" v-model="agent.follow" label="Follow" size="sm" />
          <span class="grow" />
          <span v-if="waiting" class="text-11 text-dimmed">Waiting for you</span>
          <UChatShimmer v-else-if="busy" :text="`${session.agent.name} is working…`" class="text-11" />
          <UButton
            v-if="busy"
            label="Stop"
            icon="i-lucide-square"
            color="error"
            variant="soft"
            size="sm"
            @click="agent.cancel(session)"
          />
          <UChatPromptSubmit v-else :disabled="session.state !== 'idle' || !text.trim()" size="sm" />
        </template>
      </UChatPrompt>
    </template>
  </UChatPalette>
</template>
