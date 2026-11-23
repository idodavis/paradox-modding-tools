<!--
  A thread's transcript, a session's own or a subagent's: the prompts, the
  answers, the tool calls and the requests.
-->
<script lang="ts">
import { toPmtTool } from "../../stores/agent";
import type { Entry } from "../../stores/agentThread";

/** A ToolTitle is a tool call's title as the transcript shows it. */
type ToolTitle = { text: string; suffix: string };

/** A Turn is the record of a turn that ended. */
type Turn = Extract<Entry, { kind: "turn" }>;

/** STATUSES holds the mark of each status a tool call or a checklist's step has. */
export const STATUSES: Record<string, { icon: string; color: string; text?: string }> = {
  pending: { icon: "i-lucide-circle", color: "text-dimmed" },
  in_progress: { icon: "i-lucide-loader-circle", color: "text-primary", text: "text-highlighted" },
  completed: { icon: "i-lucide-check", color: "text-success", text: "line-through" },
  failed: { icon: "i-lucide-x", color: "text-error" },
  cancelled: { icon: "i-lucide-ban", color: "text-dimmed" },
};
// An absolute path in a tool's title, Windows' or Unix's, whose last part, the file's name, is what the row shows.
const ABSOLUTE_PATH = /(?:[A-Za-z]:[\\/]|\/)(?:[^\s`'"\\/]+[\\/])+([^\s`'"\\/]+)/g;

// A file an agent echoes back in the user's prompt as a link, such as [@file.txt](file:///C:/mod/file.txt).
const CHIP_LINK = /\[@([^\]]+)\]\(file:\/\/[^)]*\)/g;

/** describeToolCall returns a tool call's title, naming PMT's server apart from the agent's own tools. */
export function describeToolCall(entry: Extract<Entry, { kind: "tool" }>): ToolTitle {
  const tool = toPmtTool(entry.title);
  return { text: tool || entry.title.replace(ABSOLUTE_PATH, "$1") || "A tool", suffix: tool ? "PMT" : "" };
}

/** splitChips returns a prompt's text without the files it names as links, and the names of those files. */
export function splitChips(text: string): { text: string; names: string[] } {
  const names = [...text.matchAll(CHIP_LINK)].map((match) => match[1] ?? "");
  return { text: text.replace(CHIP_LINK, "").trim(), names };
}
</script>

<script setup lang="ts">
import { createReusableTemplate, useIntervalFn, useTimeoutFn, useTimestamp } from "@vueuse/core";
import { Browser, Clipboard } from "@wailsio/runtime";
import { sumBy } from "es-toolkit";
import { computed, reactive, shallowRef } from "vue";

import type { Review } from "@services/workspace/models";
import { describeDuration, plural } from "../../lib/format";
import { toFixPrompt, useAgentStore, type Session } from "../../stores/agent";
import type { FormProperty } from "../../stores/agentRequests";
import { describeTerminal } from "../../stores/agentThread";
import { describePlan, isPlanFile, type Subagent } from "../../stores/agentWork";
import { listChanges, type FileEdit } from "../../stores/edits";
import { useToolViewStore } from "../../stores/toolView";
import AgentLink from "../../ui/AgentLink.vue";
import LineCounts from "../../ui/LineCounts.vue";
import { TABLES } from "../../ui/RichEditor.vue";

/**
 * A MessagePart is one entry of the transcript with what its view needs: a
 * tool call's title, or the subagent a tool call started.
 */
type MessagePart = { entry: Entry; tool?: ToolTitle; subagent?: Subagent };

const { session, thread } = defineProps<{
  session: Session;
  /** The thread drawn: the session itself, or one of its subagents. */
  thread: Session | Subagent;
}>();
const emit = defineEmits<{
  /** The user asked to see the session's subagents. */
  subagents: [];
}>();

// An answer's text: inline code in the theme's orange softened toward its text color, on a faint tint, a code block on
// the darkest panel, and a quote set off by a bar.
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
// A link in an answer to a file: an absolute path or a file:// address, with
// an optional line, as C:\mod\a.txt:12 or file:///C:/mod/a.txt#L12.
const FILE_LINK = /^(?:file:\/\/)?\/?((?:[A-Za-z]:)?[\\/].*?)(?:#L(\d+).*|:(\d+)(?::\d+)?)?$/;
// The editor blanks a link it does not allow, and it allows no file link.
const LINKS = {
  link: {
    isAllowedUri: (url: string, context: { defaultValidate: (url: string) => boolean }) =>
      context.defaultValidate(url) || FILE_LINK.test(url),
  },
};
// A tool row, a run of tool calls or the reasoning stands apart from what comes before it.
const MACHINERY_ROW = "max-w-180 not-first:mt-5";
const OPTION_STYLES: Record<string, { color: "primary" | "neutral"; variant: "solid" | "outline" | "ghost" }> = {
  allow_once: { color: "primary", variant: "solid" },
  allow_always: { color: "neutral", variant: "outline" },
};

const agent = useAgentStore();
const toolView = useToolViewStore();
// What the user has filled in of each open form, by the form's id.
const contents = reactive<Record<string, Record<string, unknown>>>({});
const [DefineTool, ShowTool] = createReusableTemplate<{ entry: Entry; tool?: ToolTitle }>();
const [DefineEdit, ShowEdit] = createReusableTemplate<{ edit: FileEdit }>();
const [DefineEdits, ShowEdits] = createReusableTemplate<{ edits: FileEdit[]; turn?: Turn }>();
const [DefineTurn, ShowTurn] = createReusableTemplate<{ turn: Turn }>();
const TOKENS = new Intl.NumberFormat(undefined, { notation: "compact", maximumFractionDigits: 1 });
const now = useTimestamp({ scheduler: (tick) => useIntervalFn(tick, 1000) });
const copiedId = shallowRef("");
const { start: clearCopied } = useTimeoutFn(() => (copiedId.value = ""), 1500, { immediate: false });
// Each user entry is a message of its own; the entries after it, the agent's texts and tool calls, share one message.
// The call that asks to start on a plan is left out: the prompt box asks it.
const messages = computed(() =>
  thread.entries.reduce<{ id: string; role: "user" | "assistant"; parts: MessagePart[] }[]>((list, entry) => {
    if (entry.kind === "tool" && entry.type === "switch_mode") return list;
    const subagent = entry.kind === "subagent" ? session.subagents.find((other) => other.id === entry.id) : undefined;
    const part = { entry, subagent, tool: entry.kind === "tool" ? describeToolCall(entry) : undefined };
    const last = list.at(-1);
    if (entry.kind !== "user" && last?.role === "assistant") last.parts.push(part);
    else list.push({ id: entry.id, role: entry.kind === "user" ? "user" : "assistant", parts: [part] });
    return list;
  }, []),
);
// The files each message's tool calls changed, by the message's id. A plan
// file is no change to review, so it is left out.
const messageEdits = computed(
  () =>
    new Map(
      messages.value.map((message) => [
        message.id,
        listChanges(message.parts.map(({ entry }) => entry)).filter((edit) => !isPlanFile(session, edit.path)),
      ]),
    ),
);
// A resumed session's replay arrives while it starts, so the chat follows it as it follows a reply.
const status = computed(() => (session.state === "running" || session.state === "starting" ? "streaming" : "ready"));
const busy = computed(() => session.state === "running");

/** copyAnswer puts a message's answer, the last text the agent wrote in it, on the clipboard, and marks it copied. */
async function copyAnswer(id: string, parts: MessagePart[]): Promise<void> {
  const answer = parts.findLast(({ entry }) => entry.kind === "agent")?.entry;
  if (answer?.kind !== "agent") return;
  await Clipboard.SetText(answer.text);
  copiedId.value = id;
  clearCopied();
}

/** findTurn returns the record of the turn that ended in a message, if any. */
function findTurn(parts: MessagePart[]): Turn | undefined {
  return parts.map(({ entry }) => entry).findLast((entry) => entry.kind === "turn");
}

/**
 * describeTurn returns a turn's line: how long it took, then the tokens it
 * used and what it cost where the agent reports them, such as
 * "Worked 2 min · 18.4K tokens · $0.31".
 */
function describeTurn(turn: Turn): string {
  const tokens = turn.tokens ? `${TOKENS.format(turn.tokens)} tokens` : "";

  return [`Worked ${describeDuration(turn.duration)}`, tokens, turn.cost].filter(Boolean).join(" · ");
}

/** listReviews returns the files a turn changed that are under review. */
function listReviews(turn: Turn): Review[] {
  return (agent.reviews ?? []).filter((review) => turn.paths.includes(review.path));
}

/** groupToolCalls returns a message's parts as runs. Consecutive tool calls share a run, and any other part has its own. */
function groupToolCalls(parts: MessagePart[]): MessagePart[][] {
  return parts.reduce<MessagePart[][]>((runs, part) => {
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
function classifyRun(run: MessagePart[]): string {
  const statuses = run.flatMap(({ entry }) => (entry.kind === "tool" ? [entry.status] : []));
  if (statuses.includes("failed")) return "failed";
  if (statuses.includes("in_progress")) return "in_progress";
  return statuses.every((status) => status === "completed") ? "completed" : "pending";
}

/** describeRun returns the label of a run of tool calls: how many, then each tool used once, with its count. */
function describeRun(run: MessagePart[]): string {
  const counts = new Map<string, number>();
  for (const { tool } of run) if (tool) counts.set(tool.text, (counts.get(tool.text) ?? 0) + 1);
  const labels = [...counts].map(([title, count]) => (count > 1 ? `${title} ×${count}` : title));
  return `Used ${run.length} tools · ${labels.join(", ")}`;
}

/**
 * describeRunTime returns how long a subagent has run so far, or ran before
 * it stopped.
 */
function describeRunTime(subagent: Subagent): string {
  return describeDuration((subagent.endedAt || now.value) - subagent.startedAt);
}

/**
 * lookupContent returns what the user has filled in so far of the form with
 * the given id, by question.
 */
function lookupContent(id: string): Record<string, unknown> {
  return contents[id] ?? {};
}

/** replyField stores the user's reply to one question of a form. */
function replyField(id: string, key: string, value: unknown): void {
  contents[id] = { ...contents[id], [key]: value };
}

/**
 * isPicked reports whether a choice is among the replies to a form's
 * question.
 */
function isPicked(id: string, property: FormProperty, value: string): boolean {
  const reply = lookupContent(id)[property.key];

  return Array.isArray(reply) ? reply.includes(value) : reply === value;
}

/**
 * pick replies to a form's question with a choice. A question that takes
 * several adds the choice to its replies, or takes it out if it is there.
 */
function pick(id: string, property: FormProperty, value: string): void {
  const choices = (lookupContent(id)[property.key] as string[] | undefined) ?? [];
  const several = choices.includes(value) ? choices.filter((other) => other !== value) : [...choices, value];
  replyField(id, property.key, property.type === "array" ? several : value);
}

/**
 * isFilled reports whether a form can be sent: every question it requires
 * has a reply, and at least one question has.
 */
function isFilled(id: string, properties: FormProperty[]): boolean {
  const replies = lookupContent(id);
  const has = (property: FormProperty): boolean =>
    ![undefined, ""].includes(replies[property.key] as string | undefined);

  return properties.some(has) && properties.every((property) => !property.isRequired || has(property));
}

/**
 * describeDocument returns the title of the session's plan document with the
 * given id, "" if it has none.
 */
function describeDocument(id: string): string {
  const document = session.documents.find((other) => other.id === id);

  return document ? describePlan(document) : "";
}

/** openLink opens a link in an answer: a web address in the browser, a file in the IDE at its line. */
function openLink(event: MouseEvent): void {
  const link = event.target instanceof Element ? event.target.closest(".ProseMirror a") : null;
  if (!link) return;
  // The link's own click would open a window of the app.
  event.preventDefault();
  const href = link.getAttribute("href") ?? "";
  const [, path, hashLine, colonLine] = FILE_LINK.exec(decodeURI(href)) ?? [];
  if (/^https?:/i.test(href)) void Browser.OpenURL(href);
  else if (path) void toolView.followFile(path, Number(hashLine ?? colonLine ?? 1));
}
</script>

<template>
  <div class="flex flex-col" @click.capture="openLink" @auxclick.capture="openLink">
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
          <UButton
            v-if="entry.canCancel && entry.status === 'in_progress'"
            label="Stop this tool call"
            icon="i-lucide-square"
            color="error"
            variant="soft"
            size="xs"
            class="self-start"
            @click="agent.cancelToolCall(session, thread, entry.id)"
          />
          <p v-if="entry.text" class="font-mono text-11 whitespace-pre-wrap text-toned">{{ entry.text }}</p>
          <pre
            v-for="terminal in entry.terminals"
            :key="terminal"
            class="max-h-60 overflow-auto rounded-sm bg-(--bg-0) px-2 py-1.5 font-mono text-11 leading-4.5 text-toned"
            >{{ thread.terminals[terminal] ? describeTerminal(thread.terminals[terminal]) : "" }}</pre>
          <ShowEdit v-for="edit in entry.edits" :key="edit.path" :edit="edit" />
        </div>
      </UChatTool>
    </DefineTool>
    <DefineEdit v-slot="{ edit }">
      <div class="flex flex-col gap-0.5">
        <span class="flex items-center gap-2">
          <span class="truncate font-mono text-11">{{ edit.label }}</span>
          <LineCounts v-if="edit.patch" :added="edit.added" :removed="edit.removed" />
          <UButton
            :label="agent.findReview(edit.path) ? 'Review' : 'Open'"
            icon="i-lucide-git-compare"
            variant="ghost"
            size="xs"
            @click="agent.showFile(edit.path)"
          />
        </span>
        <span v-if="edit.reviewError" class="text-11 text-error">
          This change can't be reviewed in the editor: {{ edit.reviewError }}
        </span>
      </div>
    </DefineEdit>
    <DefineEdits v-slot="{ edits, turn }">
      <UCollapsible class="max-w-180 rounded-md border border-default">
        <template #default="{ open }">
          <div class="flex h-7 cursor-pointer items-center gap-2 ps-2.5 pe-1 text-12 hover:bg-elevated">
            <UIcon :name="open ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'" class="size-3.5 text-dimmed" />
            <span class="grow">{{ plural(edits.length, "file") }} changed</span>
            <LineCounts :added="sumBy(edits, (edit) => edit.added)" :removed="sumBy(edits, (edit) => edit.removed)" />
            <!-- The button keeps its click, so the list does not open too. -->
            <UButton
              v-if="turn && listReviews(turn).length"
              label="Undo this turn"
              icon="i-lucide-undo-2"
              color="error"
              variant="ghost"
              size="xs"
              @click.stop="agent.undo(listReviews(turn), turn.id)"
            />
          </div>
        </template>
        <template #content>
          <div class="flex flex-col gap-1 px-2.5 pb-2 ps-8">
            <ShowEdit v-for="edit in edits" :key="edit.path" :edit="edit" />
          </div>
        </template>
      </UCollapsible>
    </DefineEdits>
    <DefineTurn v-slot="{ turn }">
      <div
        v-for="{ place, problems } in turn.problems"
        :key="place.path"
        class="mt-1.5 flex h-7 max-w-180 items-center gap-2 rounded-md border border-default ps-2.5 pe-1 text-12"
      >
        <UIcon name="i-lucide-circle-alert" class="size-3.5 shrink-0 text-warning" />
        <span class="min-w-0 grow truncate" :title="place.rel || place.path">
          {{ plural(problems.length, "new problem") }} in <span class="font-mono text-11">{{ place.name }}</span>
        </span>
        <UButton
          :label="`Fix with ${session.agent.name}`"
          icon="i-lucide-sparkles"
          variant="ghost"
          size="xs"
          @click="agent.ask(toFixPrompt(place, problems))"
        />
      </div>
      <div class="mt-1.5 flex h-6 max-w-180 items-center gap-1.5 text-11 text-dimmed tabular-nums">
        <span class="grow">{{ describeTurn(turn) }}</span>
        <UButton
          v-if="turn.isUnfinished && session.last && !busy && thread.entries.at(-1)?.id === turn.id"
          label="Retry"
          icon="i-lucide-rotate-cw"
          variant="ghost"
          color="neutral"
          size="xs"
          @click="agent.retry(session)"
        />
      </div>
    </DefineTurn>

    <UChatMessages
      :messages="messages"
      :status="status"
      :should-auto-scroll="true"
      compact
      :user="{ side: 'right', variant: 'soft' }"
      :assistant="{ side: 'left', variant: 'naked' }"
      :ui="{ root: 'gap-3.5' }"
    >
      <template #content="{ message }">
        <template v-for="run in groupToolCalls(message.parts as MessagePart[])" :key="run[0]?.entry.id">
          <UChatTool
            v-if="run.length > 1"
            :text="describeRun(run)"
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
            <template v-for="{ entry, tool, subagent } in run" :key="entry.id">
              <!-- A long path has no place to break, so the text wraps anywhere. -->
              <div v-if="entry.kind === 'user'" class="flex max-w-full min-w-0 flex-col items-end gap-1.5">
                <span class="max-w-full text-13 leading-5 wrap-anywhere whitespace-pre-wrap">{{
                  splitChips(entry.text).text
                }}</span>
                <span
                  v-for="chip in [...entry.chips, ...splitChips(entry.text).names]"
                  :key="chip"
                  class="flex items-center gap-1 font-mono text-11 text-toned"
                >
                  <UIcon name="i-lucide-file" class="size-3 text-dimmed" />{{ chip }}
                </span>
                <!-- Edit copies the session up to this prompt. -->
                <UButton
                  v-if="entry.cursor && session.forkable"
                  label="Edit"
                  icon="i-lucide-pencil"
                  variant="ghost"
                  color="neutral"
                  size="xs"
                  @click="agent.fork(session, entry)"
                />
              </div>
              <UEditor
                v-else-if="entry.kind === 'agent'"
                :model-value="entry.text"
                content-type="markdown"
                :extensions="TABLES"
                :starter-kit="LINKS"
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
              <button
                v-else-if="subagent"
                type="button"
                class="flex w-full items-center gap-1.5 text-start text-12 text-dimmed hover:text-toned"
                :class="MACHINERY_ROW"
                @click="emit('subagents')"
              >
                <UIcon
                  name="i-lucide-bot"
                  class="size-3.5 shrink-0"
                  :class="subagent.isRunning ? 'text-primary' : 'text-success'"
                />
                <span class="shrink-0 font-medium text-highlighted">{{ subagent.description || "Subagent" }}</span>
                <span class="min-w-0 grow truncate">{{ subagent.title }}</span>
                <span class="shrink-0 text-11 tabular-nums">{{ describeRunTime(subagent) }}</span>
                <UIcon name="i-lucide-chevron-right" class="size-3.5 shrink-0" />
              </button>
              <div v-else-if="entry.kind === 'tool'" :class="MACHINERY_ROW">
                <ShowTool :entry="entry" :tool="tool" />
              </div>
              <!-- A plan is named here and opened from the pinned row. -->
              <button
                v-else-if="entry.kind === 'plan'"
                type="button"
                class="flex items-center gap-1.5 text-start text-12 text-dimmed hover:text-toned"
                :class="MACHINERY_ROW"
                @click="agent.showPlan(session, entry.plan)"
              >
                <UIcon name="i-lucide-file-text" class="size-3.5 shrink-0" />
                <span class="shrink-0">{{ entry.isUpdate ? "Updated the plan" : "Wrote the plan" }}</span>
                <span v-if="session.documents.length > 1" class="min-w-0 truncate font-medium text-highlighted">
                  {{ describeDocument(entry.plan) }}
                </span>
              </button>
              <!-- A request to start on a plan is in the prompt box. -->
              <div
                v-else-if="entry.kind === 'permission' && !entry.plan"
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
                    :color="OPTION_STYLES[option.kind]?.color ?? 'neutral'"
                    :variant="OPTION_STYLES[option.kind]?.variant ?? 'ghost'"
                    size="sm"
                    @click="agent.reply(session, entry.id, option.id)"
                  />
                </div>
              </div>
              <AgentLink
                v-else-if="entry.kind === 'link'"
                :agent="session.agent.name"
                :message="entry.message"
                :url="entry.url"
                @answer="(action) => agent.reply(session, entry.id, action)"
              />
              <form
                v-else-if="entry.kind === 'form'"
                class="flex max-w-180 flex-col gap-3 rounded-lg border border-primary bg-(--accent-soft) px-3.5 py-3"
                @submit.prevent="agent.reply(session, entry.id, lookupContent(entry.id))"
              >
                <span class="flex items-center gap-2.5 text-13 font-medium">
                  <UIcon name="i-lucide-circle-help" class="size-4 shrink-0 text-primary" />{{ entry.message }}
                </span>
                <div v-for="property in entry.properties" :key="property.key" class="flex flex-col gap-1.5">
                  <span
                    v-if="entry.properties.length > 1 || property.title !== entry.message"
                    class="text-12 font-medium"
                  >
                    {{ property.title }}
                  </span>
                  <span v-if="property.description && !property.choices.length" class="text-11 text-toned">
                    {{ property.description }}
                  </span>
                  <div v-if="property.choices.length" class="flex flex-col gap-1">
                    <span v-for="choice in property.choices" :key="choice.value" class="flex items-center gap-2">
                      <UButton
                        :label="choice.title"
                        :color="isPicked(entry.id, property, choice.value) ? 'primary' : 'neutral'"
                        :variant="isPicked(entry.id, property, choice.value) ? 'solid' : 'outline'"
                        size="sm"
                        class="shrink-0"
                        @click="pick(entry.id, property, choice.value)"
                      />
                      <span class="min-w-0 text-11 text-toned">{{ choice.description }}</span>
                    </span>
                  </div>
                  <USwitch
                    v-else-if="property.type === 'boolean'"
                    :model-value="!!lookupContent(entry.id)[property.key]"
                    size="sm"
                    :aria-label="property.title"
                    @update:model-value="(value) => replyField(entry.id, property.key, value)"
                  />
                  <UInput
                    v-else
                    :model-value="String(lookupContent(entry.id)[property.key] ?? '')"
                    :type="property.type === 'string' ? 'text' : 'number'"
                    size="sm"
                    :aria-label="property.title"
                    class="w-full"
                    @update:model-value="
                      (value) => replyField(entry.id, property.key, property.type === 'string' ? value : Number(value))
                    "
                  />
                </div>
                <div class="flex gap-2">
                  <UButton type="submit" label="Send" size="sm" :disabled="!isFilled(entry.id, entry.properties)" />
                  <UButton
                    label="Skip"
                    variant="ghost"
                    color="neutral"
                    size="sm"
                    @click="agent.reply(session, entry.id, null)"
                  />
                </div>
              </form>
              <div
                v-else-if="entry.kind === 'compaction' && entry.status === 'in_progress'"
                class="flex items-center gap-1.5 text-12 text-dimmed"
                :class="MACHINERY_ROW"
              >
                <UIcon name="i-lucide-loader-circle" class="size-3.5 animate-spin text-primary" />
                Compacting the conversation…
              </div>
              <UChatTool
                v-else-if="entry.kind === 'compaction'"
                :text="
                  entry.status === 'completed' ? 'Compacted the conversation' : 'The conversation was not compacted'
                "
                :icon="STATUSES[entry.status]?.icon ?? 'i-lucide-x'"
                :ui="{ ...MACHINERY, leadingIcon: STATUSES[entry.status]?.color ?? 'text-error' }"
                :class="MACHINERY_ROW"
              >
                <p class="ps-6 text-12 whitespace-pre-wrap text-toned">{{ entry.text }}</p>
              </UChatTool>
              <p v-else-if="entry.kind === 'notice'" class="text-12 text-dimmed">{{ entry.text }}</p>
            </template>
          </template>
        </template>
        <template v-for="turn in [findTurn(message.parts as MessagePart[])]" :key="turn?.id ?? ''">
          <ShowEdits
            v-if="messageEdits.get(message.id)?.length"
            :edits="messageEdits.get(message.id) ?? []"
            :turn="turn"
          />
          <ShowTurn v-if="turn" :turn="turn" />
        </template>
      </template>
      <template #actions="{ message }">
        <UButton
          v-if="(message.parts as MessagePart[]).some(({ entry }) => entry.kind === 'agent')"
          :icon="copiedId === message.id ? 'i-lucide-check' : 'i-lucide-copy'"
          variant="ghost"
          color="neutral"
          size="xs"
          square
          aria-label="Copy"
          @click="copyAnswer(message.id, message.parts as MessagePart[])"
        />
      </template>
    </UChatMessages>
  </div>
</template>
