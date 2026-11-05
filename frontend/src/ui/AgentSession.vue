<!-- One session with an agent: the transcript, the requests the agent waits on and the prompt box. -->
<script setup lang="ts">
import type {
  SessionConfigOption,
  SessionConfigSelectGroup,
  SessionConfigSelectOption,
} from "@agentclientprotocol/sdk/experimental/v2";
import type { DropdownMenuItem } from "@nuxt/ui";
import { useQuery } from "@pinia/colada";
import { createReusableTemplate, refDebounced, useTimeoutFn } from "@vueuse/core";
import { Clipboard } from "@wailsio/runtime";
import { uniqBy } from "es-toolkit";
import { computed, shallowRef, useTemplateRef, watch } from "vue";

import type { Place } from "@services/workspace/models";
import { fileSearchQuery } from "../queries";
import { toPmtTool, useAgentStore, type Entry, type Session } from "../stores/agent";
import type { FileEdit } from "../stores/edits";
import { useToolViewStore } from "../stores/toolView";
import CompareView from "./CompareView.vue";

/** A Tool is a tool call's title as the transcript shows it. */
type Tool = { text: string; suffix: string };
/** A Url is a link split around its host. */
type Url = { before: string; host: string; after: string; lookalike: boolean };
/** A ShownEntry is one entry of the transcript with what its view needs. */
type ShownEntry = { entry: Entry; tool?: Tool; link?: Url };
/** A SelectOption is one of the agent's settings whose value is one of a list. */
type SelectOption = Extract<SessionConfigOption, { type: "select" }>;
/** A Label names a group of a setting's values. */
type Label = { type: "label"; label: string };

const { session, narrow = false } = defineProps<{ session: Session; narrow?: boolean }>();

const STATUSES: Record<string, { icon: string; color: string; text?: string }> = {
  pending: { icon: "i-lucide-circle", color: "text-dimmed" },
  in_progress: { icon: "i-lucide-loader-circle", color: "text-primary", text: "text-highlighted" },
  completed: { icon: "i-lucide-check", color: "text-success", text: "line-through" },
  failed: { icon: "i-lucide-x", color: "text-error" },
  cancelled: { icon: "i-lucide-ban", color: "text-dimmed" },
};
const PATCH_LINE_COLORS: Record<string, string> = { "+": "text-success", "-": "text-error" };
// An answer's text: inline code in the theme's orange softened toward its text color, on a faint tint, a code block on the darkest panel, and a quote
// set off by a bar.
const ANSWER = [
  "w-full py-0 text-13 text-highlighted [&_code]:text-12 [&_pre]:text-12",
  "[&_:not(pre)>code]:rounded-sm [&_:not(pre)>code]:bg-(--orange)/6 [&_:not(pre)>code]:px-1",
  "[&_:not(pre)>code]:text-[color-mix(in_oklab,var(--orange)_80%,var(--fg-2))]",
  "[&_:not(pre)>code]:before:content-none [&_:not(pre)>code]:after:content-none",
  "[&_pre]:rounded-md [&_pre]:border [&_pre]:border-default [&_pre]:bg-(--bg-0) [&_pre]:px-3 [&_pre]:py-2",
  "[&_blockquote]:border-s-2 [&_blockquote]:border-accented [&_blockquote]:ps-3 [&_blockquote]:text-toned",
].join(" ");
// Tool calls and the agent's reasoning are the session's machinery, drawn dimmer than the prompts and the answers.
const MACHINERY = { trigger: "text-dimmed hover:text-toned" };
// A file an agent echoes back in the user's prompt as a link, such as [@file.txt](file:///C:/mod/file.txt).
const MENTION = /\[@([^\]]+)\]\(file:\/\/[^)]*\)/g;
// An absolute path in a tool's title, Windows' or Unix's, whose last part, the file's name, is what the row shows.
const ABSOLUTE_PATH = /(?:[A-Za-z]:[\\/]|\/)(?:[^\s`'"\\/]+[\\/])+([^\s`'"\\/]+)/g;
// A tool row, a run of tool calls or the reasoning stands apart from what comes before it.
const MACHINERY_ROW = "max-w-180 not-first:mt-5";
const OPTIONS: Record<string, { color: "primary" | "neutral"; variant: "solid" | "outline" | "ghost" }> = {
  allow_once: { color: "primary", variant: "solid" },
  allow_always: { color: "neutral", variant: "outline" },
};
// The kinds of setting drawn as a picker each. The agent's other settings are in one menu.
const CATEGORIES = ["model", "thought_level", "mode"];
// The prompt box is an inset card whose border turns lighter, not colored, while it has the focus.
const PROMPT_CARD =
  "self-center rounded-lg border-t-0 px-2.5 focus-within:ring-accented has-[textarea:focus-visible]:outline-0 has-[textarea:focus-visible]:ring-accented";
// A wide pane keeps the transcript and the prompt box to a readable column; a narrow one fills them.
const FRAMES = {
  narrow: { content: "px-3.5 py-4", column: "", prompt: `${PROMPT_CARD} mb-3 w-[calc(100%-1.5rem)]` },
  wide: {
    content: "px-7 py-5",
    column: "mx-auto w-full max-w-200",
    prompt: `${PROMPT_CARD} mb-4.5 w-[calc(100%-3.5rem)] max-w-200`,
  },
};
// The option menus show a choice's description under its name, and hover as the other footer controls do.
const SELECT_UI = {
  base: "hover:bg-accented focus:bg-transparent focus-visible:bg-accented",
  content:
    "w-auto min-w-(--reka-select-trigger-width) max-w-80 max-h-[min(26rem,var(--reka-select-content-available-height,26rem))]",
  item: "h-auto py-1.5",
  itemLabel: "text-12",
  itemDescription: "text-11 text-dimmed whitespace-normal",
};

const agent = useAgentStore();
const [DefineTool, ShowTool] = createReusableTemplate<{ entry: Entry; tool?: Tool }>();
const toolView = useToolViewStore();
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
const comparing = shallowRef<FileEdit | null>(null);
const copied = shallowRef("");
const { start: clearCopied } = useTimeoutFn(() => (copied.value = ""), 1500, { immediate: false });

// Each user entry is a message of its own; the entries after it, the agent's texts and tool calls, share one message.
const messages = computed(() =>
  session.entries.reduce<{ id: string; role: "user" | "assistant"; parts: ShownEntry[] }[]>((list, entry) => {
    const part = {
      entry,
      tool: entry.kind === "tool" ? describeTool(entry) : undefined,
      link: entry.kind === "link" ? splitUrl(entry.url) : undefined,
    };
    const last = list.at(-1);
    if (entry.kind !== "user" && last?.role === "assistant") last.parts.push(part);
    else list.push({ id: entry.id, role: entry.kind === "user" ? "user" : "assistant", parts: [part] });
    return list;
  }, []),
);
const frame = computed(() => (narrow ? FRAMES.narrow : FRAMES.wide));
const status = computed(() => (session.state === "running" ? "streaming" : "ready"));
const busy = computed(() => session.state === "running");
const waiting = computed(() => agent.isWaiting(session));
const empty = computed(() => session.state === "idle" && !session.entries.length);
const placeholder = computed(() =>
  session.commands.length
    ? `Ask ${session.agent.name}, type @ to add a file or / for a command`
    : `Ask ${session.agent.name}, or type @ to add a file`,
);
const fileGroups = computed(() => [
  { id: "files", items: (places.value ?? []).map((place) => ({ label: place.rel, onSelect: () => pick(place) })) },
]);
const commands = computed(() => [
  {
    id: "commands",
    items: session.commands.map((command) => ({
      label: `/${command.name}`,
      suffix: command.description,
      onSelect: () => {
        text.value = `/${command.name} `;
        pickingCommand.value = false;
      },
    })),
  },
]);
const pickers = computed(() =>
  CATEGORIES.flatMap((category) =>
    session.options.filter(
      (option): option is SelectOption => option.type === "select" && option.category === category,
    ),
  ).map((option) => ({
    option,
    items: listValues(option, (value) => ({
      label: value.name,
      value: value.value,
      description: value.description ?? "",
    })),
  })),
);
// The settings menu holds Follow, then the agent's other settings.
const settings = computed<DropdownMenuItem[][]>(() => {
  const others = session.options
    .filter((option) => option.type === "select" || option.type === "boolean")
    .filter((option) => !pickers.value.some((picker) => picker.option.configId === option.configId))
    .map(describeSetting);
  const follow: DropdownMenuItem = {
    type: "checkbox",
    label: "Follow the files the agent opens",
    checked: agent.follow,
    onUpdateChecked: (checked) => (agent.follow = checked),
  };
  return others.length ? [[follow], others] : [[follow]];
});

// Typing @ picks a file, and typing / in an empty box picks a command.
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

watch(
  [() => agent.isPromptWanted, () => session.state, box],
  ([isWanted, state, field]) => {
    if (!isWanted || !field || (state !== "idle" && state !== "running")) return;
    field.textareaRef?.focus();
    agent.isPromptWanted = false;
  },
  { immediate: true, flush: "post" },
);

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

/** copyAnswer puts a message's answer, the last text the agent wrote in it, on the clipboard, and marks it copied. */
async function copyAnswer(id: string, parts: ShownEntry[]): Promise<void> {
  const answer = parts.findLast(({ entry }) => entry.kind === "agent")?.entry;
  if (answer?.kind !== "agent") return;
  await Clipboard.SetText(answer.text);
  copied.value = id;
  clearCopied();
}

/** focusBox puts the caret back in the prompt box when the command list closes. */
function focusBox(event: Event): void {
  event.preventDefault();
  box.value?.textareaRef?.focus();
}

/** listValues returns the values of an agent's setting as items, one list for each group. */
function listValues<T>(option: SelectOption, toItem: (value: SessionConfigSelectOption) => T): (T | Label)[][] {
  const values = option.options as (SessionConfigSelectOption | SessionConfigSelectGroup)[];
  // A group has `options` and a plain value has none.
  const groups: Pick<SessionConfigSelectGroup, "name" | "options">[] = values.some((value) => "options" in value)
    ? (values as SessionConfigSelectGroup[])
    : [{ name: "", options: values as SessionConfigSelectOption[] }];
  return groups.map((group) => [
    ...(group.name ? [{ type: "label" as const, label: group.name }] : []),
    ...uniqBy(group.options, (value) => value.value).map(toItem),
  ]);
}

/** groupTools returns a message's parts as runs. Consecutive tool calls share a run, and any other part has its own. */
function groupTools(parts: ShownEntry[]): ShownEntry[][] {
  return parts.reduce<ShownEntry[][]>((runs, part) => {
    const last = runs.at(-1);
    if (part.tool && last?.at(-1)?.tool) last.push(part);
    else runs.push([part]);
    return runs;
  }, []);
}

/**
 * classifyRun returns the status a run of tool calls shows: failed if any call failed, in progress while one runs,
 * completed once every call has, and pending otherwise.
 */
function classifyRun(run: ShownEntry[]): string {
  const statuses = run.flatMap(({ entry }) => (entry.kind === "tool" ? [entry.status] : []));
  if (statuses.includes("failed")) return "failed";
  if (statuses.includes("in_progress")) return "in_progress";
  return statuses.every((status) => status === "completed") ? "completed" : "pending";
}

/** summarizeTools returns the label of a run of tool calls: how many, then each tool used once, with its count. */
function summarizeTools(run: ShownEntry[]): string {
  const counts = new Map<string, number>();
  for (const { tool } of run) if (tool) counts.set(tool.text, (counts.get(tool.text) ?? 0) + 1);
  const used = [...counts].map(([title, count]) => (count > 1 ? `${title} ×${count}` : title));
  return `Used ${run.length} tools · ${used.join(", ")}`;
}

/** describeSetting returns one of the agent's other settings as the settings menu lists it. */
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

/** describeTool returns a tool call's title, naming PMT's server apart from the agent's own tools. */
function describeTool(entry: Extract<Entry, { kind: "tool" }>): Tool {
  const tool = toPmtTool(entry.title);
  return { text: tool || entry.title.replace(ABSOLUTE_PATH, "$1") || "A tool", suffix: tool ? "PMT" : "" };
}

/** splitMentions returns a prompt's text without the files it names as links, and the names of those files. */
function splitMentions(text: string): { text: string; names: string[] } {
  const names = [...text.matchAll(MENTION)].map((match) => match[1] ?? "");
  return { text: text.replace(MENTION, "").trim(), names };
}

/** splitUrl splits a link around its host, so the host can stand out. */
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

/** stripControls returns a terminal's output without its color and cursor codes. */
function stripControls(output: string): string {
  return output.replace(/\u001b\[[0-9;?]*[ -/]*[@-~]|\u001b\][^\u0007]*\u0007/g, "");
}
</script>

<template>
  <UChatPalette :ui="frame">
    <div class="flex flex-col" :class="frame.column">
      <DefineTool v-slot="{ entry, tool }">
        <UChatTool
          v-if="entry.kind === 'tool'"
          :text="tool?.text"
          :suffix="tool?.suffix"
          :icon="STATUSES[entry.status]?.icon ?? 'i-lucide-wrench'"
          :loading="entry.status === 'in_progress'"
          :ui="{ ...MACHINERY, leadingIcon: STATUSES[entry.status]?.color }"
          class="max-w-180"
        >
          <div class="flex flex-col gap-1.5 ps-6 text-12">
            <p v-if="entry.text" class="font-mono text-11 whitespace-pre-wrap text-toned">{{ entry.text }}</p>
            <pre
              v-for="terminal in entry.terminals"
              :key="terminal"
              class="max-h-60 overflow-auto rounded-sm bg-(--bg-0) px-2 py-1.5 font-mono text-11 leading-4.5 text-toned"
              >{{ stripControls(session.terminals[terminal] ?? "") }}</pre>
            <span v-for="change in entry.edits" :key="change.path" class="flex items-center gap-2">
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
      </DefineTool>

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
          v-for="task in toolView.tasks"
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
          <template v-for="run in groupTools(message.parts as ShownEntry[])" :key="run[0]?.entry.id">
            <UChatTool
              v-if="run.length > 1"
              :text="summarizeTools(run)"
              :icon="STATUSES[classifyRun(run)]?.icon"
              :loading="classifyRun(run) === 'in_progress'"
              :ui="{ ...MACHINERY, leadingIcon: STATUSES[classifyRun(run)]?.color }"
              :class="MACHINERY_ROW"
            >
              <div class="flex flex-col gap-1.5 ps-6">
                <ShowTool v-for="part in run" :key="part.entry.id" :entry="part.entry" :tool="part.tool" />
              </div>
            </UChatTool>
            <template v-else>
              <template v-for="{ entry, tool, link } in run" :key="entry.id">
                <div v-if="entry.kind === 'user'" class="flex flex-col items-end gap-1.5">
                  <span class="text-13 leading-5 whitespace-pre-wrap">{{ splitMentions(entry.text).text }}</span>
                  <span
                    v-for="chip in [...entry.chips, ...splitMentions(entry.text).names]"
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
                  :ui="{ base: ANSWER }"
                  class="max-w-180 not-first:mt-4 not-last:mb-4"
                />
                <UChatReasoning
                  v-else-if="entry.kind === 'thought'"
                  :text="entry.text"
                  :streaming="busy"
                  :ui="MACHINERY"
                  :class="MACHINERY_ROW"
                />
                <div v-else-if="entry.kind === 'tool'" :class="MACHINERY_ROW">
                  <ShowTool :entry="entry" :tool="tool" />
                </div>
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
          </template>
        </template>
        <template #actions="{ message }">
          <UButton
            v-if="(message.parts as ShownEntry[]).some(({ entry }) => entry.kind === 'agent')"
            :icon="copied === message.id ? 'i-lucide-check' : 'i-lucide-copy'"
            variant="ghost"
            color="neutral"
            size="xs"
            square
            aria-label="Copy"
            @click="copyAnswer(message.id, message.parts as ShownEntry[])"
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
            :class="PATCH_LINE_COLORS[line.charAt(0)] ?? 'text-toned'"
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
          <div v-if="toolView.promptChip || files.length" class="flex flex-wrap gap-1.5">
            <span
              v-if="toolView.promptChip"
              class="flex h-5.5 items-center gap-1.5 rounded-sm bg-accented ps-1.75 pe-1 text-11"
            >
              <UIcon
                :name="toolView.promptChip.path ? 'i-lucide-file' : 'i-lucide-scan-text'"
                class="size-3 text-dimmed"
              />
              <span :class="toolView.promptChip.path && 'font-mono'">{{ toolView.promptChip.label }}</span>
              <span v-if="toolView.promptChip.startLine" class="text-dimmed"
                >{{ toolView.promptChip.startLine }}–{{ toolView.promptChip.endLine }}</span
              >
              <UButton
                icon="i-lucide-x"
                variant="ghost"
                size="xs"
                square
                aria-label="Leave it out"
                @click="toolView.chipLeftOut = true"
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
            <UTooltip text="Add a file (@)">
              <UButton icon="i-lucide-at-sign" variant="ghost" size="sm" square aria-label="Add a file" />
            </UTooltip>
            <template #content>
              <UCommandPalette
                v-model:search-term="term"
                :groups="fileGroups"
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
            <UTooltip text="Run a command (/)">
              <UButton icon="i-lucide-square-slash" variant="ghost" size="sm" square aria-label="Run a command" />
            </UTooltip>
            <template #content>
              <!-- Every command is listed, where the palette would list only its first 12. -->
              <UCommandPalette
                :groups="commands"
                :fuse="{ resultLimit: session.commands.length }"
                placeholder="Find a command of the agent"
                class="h-72 w-96"
              />
            </template>
          </UPopover>
          <USelect
            v-for="{ option, items } in pickers"
            :key="option.configId"
            :model-value="option.currentValue"
            :items="items"
            :aria-label="option.name"
            variant="ghost"
            size="sm"
            :ui="SELECT_UI"
            @update:model-value="(value) => agent.configure(session, option.configId, String(value))"
          />
          <UDropdownMenu :items="settings" :content="{ align: 'start', side: 'top' }">
            <UButton icon="i-lucide-sliders-horizontal" variant="ghost" size="sm" square aria-label="More settings" />
          </UDropdownMenu>
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
          <UChatPromptSubmit v-else :disabled="session.state !== 'idle' || !text.trim()" size="sm" square />
        </template>
      </UChatPrompt>
    </template>
  </UChatPalette>
</template>
