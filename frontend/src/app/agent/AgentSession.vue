<!--
  One session with an agent: its transcript, what runs beside its answer and
  the prompt box.
-->
<script setup lang="ts">
import type { SessionConfigOption } from "@agentclientprotocol/sdk/experimental/v2";
import type { DropdownMenuItem } from "@nuxt/ui";
import { useQuery } from "@pinia/colada";
import { refDebounced, until, useEventBus, useEventListener, useIntervalFn, useTimestamp } from "@vueuse/core";
import { groupBy } from "es-toolkit";
import { computed, shallowRef, useTemplateRef, watch } from "vue";

import type { Make } from "@services/app/models";
import type { Chip } from "@services/mcp/models";
import type { Place } from "@services/workspace/models";
import { MakeAgentFile } from "@services/workspace/service";
import { reportError } from "../../lib/feedback";
import { describeCost, describeDuration, plural } from "../../lib/format";
import { fileSearchQuery, loadQuery, placeQuery, symbolsQuery } from "../../queries";
import { CHOSEN, isUnaskedMode, listChoices, useAgentStore, type Image, type Session } from "../../stores/agent";
import { useEditorStore } from "../../stores/editor";
import { useToolViewStore } from "../../stores/toolView";
import { FILE_DRAG } from "../../stores/workspace";
import AgentSignIn from "../../ui/AgentSignIn.vue";
import { COMMAND_BUS } from "../../stores/windows";
import AgentPlan from "./AgentPlan.vue";
import AgentSetting from "./AgentSetting.vue";
import AgentTranscript, { splitMentions } from "./AgentTranscript.vue";
import AgentUsage from "./AgentUsage.vue";
import AgentWork from "./AgentWork.vue";

/** A SelectOption is one of the agent's settings whose value is one of a list. */
type SelectOption = Extract<SessionConfigOption, { type: "select" }>;

const { session, narrow = false } = defineProps<{ session: Session; narrow?: boolean }>();

// The kinds of setting drawn as a picker each. The agent's other settings are in one menu.
const CATEGORIES = ["model", "thought_level", "mode"];
// The id of the setting whose choices are the agent's subagents, as Claude
// names it.
const SUBAGENTS = "agent";
// The name of a new skill, command or subagent: letters, digits and dashes.
const NAME = /^[a-z0-9][a-z0-9-]*$/i;
// The prompt box is an inset card whose border turns lighter, not colored, while it has the focus.
const PROMPT_CARD =
  "rounded-lg px-2.5 focus-within:ring-accented has-[textarea:focus-visible]:outline-0 has-[textarea:focus-visible]:ring-accented";
// What sits under the transcript: the row of what runs, over the prompt box.
const DOCK = "flex flex-col gap-1.5 self-center border-t-0";
// A wide pane keeps the transcript and the prompt box to a readable column; a narrow one fills them.
const FRAMES = {
  narrow: { content: "px-3.5 py-4", column: "", prompt: `${DOCK} mb-3 w-[calc(100%-1.5rem)]` },
  wide: {
    content: "px-7 py-5",
    column: "mx-auto w-full max-w-200",
    prompt: `${DOCK} mb-4.5 w-[calc(100%-3.5rem)] max-w-200`,
  },
};
// What PMT says of each kind of setting that has a picker: the picker's name
// where the agent has no such setting, and its panel's title and line.
// {agent} is the agent's name.
const PANELS: Record<string, { name: string; title: string; description: string }> = {
  model: {
    name: "Model",
    title: "Model",
    description: "The model {agent} answers with in this session. A change takes effect with your next message.",
  },
  thought_level: {
    name: "Effort",
    title: "Effort",
    description:
      "How long {agent} thinks before it answers and acts. More effort is slower and uses more of your plan.",
  },
  mode: {
    name: "Mode",
    title: "What {agent} may do without asking",
    description:
      "PMT refuses a change to the game's files and to an external mod whenever it is asked, and asks before a " +
      "file outside the workspace changes.",
  },
};
// PMT's line for each effort level, for an agent that sends none of its own.
const EFFORTS: Record<string, string> = {
  default: "The level the model picks for itself.",
  low: "Answers fastest and thinks least. For a small, clear change.",
  medium: "Thinks a little before it acts. For everyday work.",
  high: "Thinks longer and checks more. For a change across several files.",
  xhigh: "Thinks much longer. Slow, for a hard problem.",
  max: "Thinks as long as it can. The slowest, and the most of your plan.",
};
// A choice's value that means plan first: Claude's and Codex's "plan", and
// GitHub Copilot's, which ends in "#plan".
const PLAN = /(^|#)plan$/;
// A panel is as wide as the usage one and scrolls inside the room it has.
const PANEL = "flex max-h-(--reka-popover-content-available-height) w-90 flex-col overflow-y-auto";
// Where the list of commands puts each: the agent's own, the user's, a plugin.
const YOURS = "Yours";

const agent = useAgentStore();
const commandBus = useEventBus(COMMAND_BUS);
const editor = useEditorStore();
const toast = useToast();
const toolView = useToolViewStore();
const box = useTemplateRef("box");
// The prompt box's text is the session's, so it outlives the box.
const text = computed({ get: () => session.draft, set: (draft) => agent.writeDraft(session, draft) });
const picked = shallowRef<Chip[]>([]);
const images = shallowRef<Image[]>([]);
const picking = shallowRef(false);
const pickingCommand = shallowRef(false);
// Which list the tray of what runs shows, "" while it is closed.
const tray = shallowRef<"" | "tasks" | "subagents" | "background">("");
const term = shallowRef("");
// What the list of commands is searched for, which also names what New makes.
const commandTerm = shallowRef("");
const search = refDebounced(term, 150);
const { data: places } = useQuery(() => ({
  ...fileSearchQuery({ query: search.value, limit: 30 }),
  enabled: picking.value,
}));
const { data: definitions } = useQuery(() => ({
  ...symbolsQuery({ query: search.value, localization: false }),
  enabled: picking.value && !!search.value,
}));
const now = useTimestamp({ scheduler: (tick) => useIntervalFn(tick, 1000) });
// The prompt box's card takes a file dragged from the explorer and a picture
// pasted from the clipboard.
const card = (): HTMLElement | undefined => box.value?.$el as HTMLElement | undefined;
useEventListener(card, "keydown", recall);
useEventListener(card, "dragover", (event: DragEvent) => {
  if (event.dataTransfer?.types.includes(FILE_DRAG)) event.preventDefault();
});
useEventListener(card, "drop", (event: DragEvent) => void dropFile(event));
useEventListener(card, "paste", (event: ClipboardEvent) => void pasteImages(event));
const frame = computed(() => (narrow ? FRAMES.narrow : FRAMES.wide));
const busy = computed(() => session.state === "running");
const waiting = computed(() => agent.isWaiting(session));
const empty = computed(() => session.state === "idle" && !session.entries.length);
// The request to start on a plan, which the prompt box holds: the agent's
// own choices to start, and its own to go on planning.
const asking = computed(() => session.entries.find((entry) => entry.kind === "permission" && !!entry.plan));
const choices = computed(() => (asking.value?.kind === "permission" ? asking.value.options : []));
const approvals = computed(() => choices.value.filter((option) => option.kind.startsWith("allow")));
const refusals = computed(() => choices.value.filter((option) => !option.kind.startsWith("allow")));
const otherApprovals = computed<DropdownMenuItem[]>(() =>
  approvals.value.slice(1).map((option) => ({ label: option.name, onSelect: () => answer(option.id) })),
);
// While a turn runs, the box says what a message does: an agent that steers
// takes it into the turn, and for any other it waits in the queue.
const placeholder = computed(() => {
  const { name, steers } = session.agent;
  if (asking.value) return `Or tell ${name} what to change`;
  if (busy.value) return steers ? `Tell ${name} something while it works` : "Queue a message for when this turn ends";

  return session.commands.length
    ? `Ask ${name}, type @ to add a file or / for a command`
    : `Ask ${name}, or type @ to add a file`;
});
// How long the running turn has run, "" while none runs.
const elapsed = computed(() => (session.startedAt ? describeDuration(now.value - session.startedAt) : ""));
// The chips of the next prompt: the page's own, then the ones picked.
const chips = computed(() => [
  ...(toolView.promptChip ? [{ chip: toolView.promptChip, isPage: true }] : []),
  ...picked.value.map((chip) => ({ chip, isPage: false })),
]);
// What @ finds: the workspace's files, and its definitions by name, each
// attached as its file at its lines.
const fileGroups = computed(() => [
  {
    id: "files",
    label: "Files",
    items: (places.value ?? []).map((place) => ({ label: place.rel, onSelect: () => pick(toChip(place)) })),
  },
  {
    id: "definitions",
    label: "Definitions",
    items: (definitions.value ?? []).map(({ name, database, site }) => ({
      label: name,
      suffix: `${database} · ${site.place.rel}:${site.line}`,
      onSelect: () => pick(toChip(site.place, name, site.line, site.range.endLineNumber || site.line)),
    })),
  },
]);
// What the agent offers the session: its commands, its skills where it sends
// them apart, and its subagents, which Claude lists as the choices of its
// Agent setting. Then what PMT can make of each for the agent, under the name
// typed in the box.
const commands = computed(() => {
  const own = `${session.agent.name}'s own`;
  // A plugin's command is named plugin:command, and Claude ends the line of
  // one installed for the user's account with "(user)".
  const listed = session.commands.map((command) => {
    const plugin = command.name.includes(":") ? (command.name.split(":")[0] ?? "") : "";
    const source = plugin || (/\(user\)\s*$/.test(command.description) ? YOURS : own);
    return {
      label: `/${command.name}`,
      suffix: command.description,
      source,
      isPlugin: !!plugin,
      onSelect: () => write(`/${command.name} `),
    };
  });
  const bySource = groupBy(listed, (item) => item.source);
  // With nothing typed, a plugin is one row, which lists its commands.
  const plugins = Object.entries(
    groupBy(
      listed.filter((item) => item.isPlugin),
      (item) => item.source,
    ),
  ).map(([plugin, items]) => ({
    label: plugin,
    suffix: plural(items.length, "command"),
    source: plugin,
    onSelect: () => (commandTerm.value = `${plugin}:`),
  }));
  const subagents = (personas.value ? listChoices(personas.value) : [])
    .filter((choice) => choice.value !== "default")
    .map((choice) => ({
      label: choice.name,
      suffix: choice.description ?? "",
      source: "Subagents",
      onSelect: () => write(`Use the ${choice.name} subagent to `),
    }));
  const isName = NAME.test(commandTerm.value);
  const makes = session.agent.makes ?? [];
  const made = makes.map((make) => ({
    label: isName ? `New ${make.kind}: ${commandTerm.value}` : `New ${make.kind}: type its name above`,
    icon: "i-lucide-plus",
    disabled: !isName,
    onSelect: () => void makeFile(make, commandTerm.value),
  }));
  // An agent whose folders PMT does not know keeps the row, off, with why.
  const none = {
    label: `New: PMT cannot make ${session.agent.name}'s files yet`,
    icon: "i-lucide-plus",
    disabled: true,
  };

  return [
    { id: "own", label: own, items: bySource[own] ?? [] },
    { id: "yours", label: YOURS, items: bySource[YOURS] ?? [] },
    {
      id: "plugins",
      label: "Plugins",
      items: commandTerm.value ? listed.filter((item) => item.isPlugin) : plugins,
    },
    { id: "subagents", label: "Subagents", items: subagents },
    { id: "new", label: "New", ignoreFilter: true, items: makes.length ? made : [none] },
  ].filter((group) => group.items.length);
});
// The row of the list the keys or the pointer are on, read whole under it.
const pointed = shallowRef<{ label: string; suffix?: string; source?: string }>();

const selects = computed(() => session.options.filter((option): option is SelectOption => option.type === "select"));
// The setting whose choices are the agent's subagents, which is no setting
// of the session's own: the list of commands shows them.
const personas = computed(() => selects.value.find((option) => option.configId === SUBAGENTS));
// The mode the session had before Plan was turned on, to go back to.
const lastMode = shallowRef("");
// Plan as its own switch: the setting that holds it, the value that turns it
// on and the one that turns it off. Where Plan is one of the agent's modes,
// off is the mode the session had before; where it is a setting of its own,
// off is its other choice. It is undefined for an agent with no plan mode.
const plan = computed(() => {
  const option = selects.value.find((row) => listChoices(row).some((choice) => PLAN.test(choice.value)));
  if (!option) return undefined;

  const others = listChoices(option).filter((choice) => !PLAN.test(choice.value));
  const on = listChoices(option).find((choice) => PLAN.test(choice.value))?.value ?? "";
  const off = (others.find((choice) => choice.value === lastMode.value) ?? others[0])?.value ?? "";

  return { option, on, off, isOn: option.currentValue === on };
});
watch(
  () => plan.value?.option.currentValue,
  (value) => {
    if (typeof value === "string" && !PLAN.test(value)) lastMode.value = value;
  },
  { immediate: true },
);
// The box's pickers, one for each kind of setting in PANELS. A kind the agent
// has no setting of keeps its picker, off. While Plan is on, the picker of
// the setting that holds it shows the choice the session goes back to.
const pickers = computed(() =>
  CATEGORIES.map((category) => {
    const option = selects.value.find((row) => row.category === category);
    const panel = PANELS[category] ?? { name: category, title: "", description: "" };
    const holdsPlan = !!option && plan.value?.option.configId === option.configId;
    const shown = holdsPlan && plan.value?.isOn ? plan.value.off : option?.currentValue;
    const quick = option ? agent.listKept(session.agent.id, true)[option.configId] : undefined;

    return {
      category,
      name: panel.name,
      option,
      title: panel.title.replace("{agent}", session.agent.name),
      description: panel.description.replace("{agent}", session.agent.name),
      label: (option && listChoices(option).find((choice) => choice.value === shown)?.name) || panel.name,
      isPlanned: holdsPlan && !!plan.value?.isOn,
      hidden: holdsPlan && plan.value ? [plan.value.on] : [],
      marks: typeof quick === "string" && CHOSEN.includes(category) ? { [quick]: "Quick" } : {},
    };
  }),
);
// The agent's settings that have no picker and are not the one Plan's switch
// sets: a switch each where the setting is on or off, else a list.
const others = computed(() =>
  session.options.filter(
    (option) =>
      !pickers.value.some((picker) => picker.option?.configId === option.configId) &&
      option.configId !== plan.value?.option.configId,
  ),
);
const switches = computed(() => [
  {
    key: "follow",
    name: "Follow the files the agent opens",
    description: "The editor shows each file as the agent reads or changes it.",
    isOn: agent.follow,
    isRed: false,
    set: (isOn: boolean) => (agent.follow = isOn),
  },
  ...others.value.filter(isSwitch).map((option) => ({
    key: option.configId,
    name: option.name,
    description: option.description ?? "",
    isOn: option.currentValue === true || option.currentValue === "on",
    isRed: isUnaskedMode(session.agent, option, "on"),
    set: (isOn: boolean) =>
      void agent.configure(session, option.configId, option.type === "boolean" ? isOn : isOn ? "on" : "off"),
  })),
]);
// The model a new session starts on and the one a quick session uses, by
// name, for the model's panel. Each is "" until the user has chosen one.
const starts = computed(() => {
  const model = selects.value.find((option) => option.category === "model");
  const find = (isQuick: boolean): string => {
    const kept = model && agent.listKept(session.agent.id, isQuick)[model.configId];
    return (model && listChoices(model).find((choice) => choice.value === kept)?.name) || "";
  };

  return { chat: find(false), quick: find(true) };
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

/**
 * send empties the prompt box and sends its text with the chips picked.
 * While a turn runs, the store steers the agent with it or queues it.
 *
 * Words sent while the agent asks to start on its plan say what to change in
 * it. The request is refused first, and the words follow once that turn has
 * ended, or after five seconds if it goes on: Claude ends a turn with an
 * error if a prompt reaches it while the refusal is still being taken.
 */
async function send(): Promise<void> {
  const prompt = text.value.trim();
  if (!prompt || (session.state !== "idle" && session.state !== "running")) return;
  const [chosen, pictures] = [picked.value, images.value];
  text.value = "";
  picked.value = [];
  images.value = [];
  if (asking.value) {
    answer(refusals.value[0]?.id ?? null);
    await until(() => session.state).not.toBe("running", { timeout: 5000 });
  }

  await agent.prompt(session, prompt, chosen, pictures);
}

/**
 * answer answers the request to start on a plan with one of the agent's
 * choices, or cancels it for null.
 */
function answer(choice: string | null): void {
  if (asking.value) agent.answer(session, asking.value.id, choice);
}

/**
 * toChip returns a file as a chip: the whole file under its path in its
 * root, or the given lines of it under the given label.
 */
function toChip(place: Place, label = place.rel || place.name, startLine = 0, endLine = 0): Chip {
  return { label, path: place.path, startLine, endLine, text: "" };
}

/** dropFile adds a file dragged from the explorer to the next prompt. */
async function dropFile(event: DragEvent): Promise<void> {
  const path = event.dataTransfer?.getData(FILE_DRAG);
  if (!path) return;
  event.preventDefault();
  const place = await loadQuery(placeQuery(path));
  if (place) pick(toChip(place));
}

/**
 * pasteImages adds the pictures on the clipboard to the next prompt. For an
 * agent that takes no pictures it adds none and says so.
 */
async function pasteImages(event: ClipboardEvent): Promise<void> {
  const files = [...(event.clipboardData?.files ?? [])].filter((file) => file.type.startsWith("image/"));
  if (!files.length) return;
  event.preventDefault();
  if (!session.images) {
    toast.add({ title: `${session.agent.name} takes no pictures in a prompt` });
    return;
  }

  const pasted = await Promise.all(
    files.map(async (file): Promise<Image> => {
      const data = new Uint8Array(await file.arrayBuffer()).toBase64();
      return { type: "image", mimeType: file.type, data };
    }),
  );
  images.value = [...images.value, ...pasted];
}

/** pick adds a chip to the next prompt, once. */
function pick(chip: Chip): void {
  const isPicked = picked.value.some((other) => other.path === chip.path && other.startLine === chip.startLine);
  if (!isPicked) picked.value = [...picked.value, chip];
  picking.value = false;
  term.value = "";
}

/** leaveOut takes a chip off the next prompt. */
function leaveOut(chip: Chip, isPage: boolean): void {
  if (isPage) toolView.chipLeftOut = true;
  else picked.value = picked.value.filter((other) => other !== chip);
}

/**
 * stop stops the running turn when Esc is pressed in the prompt box. The key
 * then does nothing more, so the view over the whole window stays open.
 */
function stop(event: Event): void {
  if (!busy.value) return;
  event.preventDefault();
  agent.cancel(session);
}

/**
 * recall puts the session's last prompt back in the prompt box when the Up
 * key is pressed in an empty box.
 */
function recall(event: KeyboardEvent): void {
  const last = session.entries.findLast((entry) => entry.kind === "user");
  if (event.key !== "ArrowUp" || text.value || last?.kind !== "user") return;
  event.preventDefault();
  text.value = splitMentions(last.text).text;
}

/** write puts a text in the prompt box and closes the list of commands. */
function write(draft: string): void {
  text.value = draft;
  pickingCommand.value = false;
}

/**
 * makeFile makes a skill, a command or a subagent of the given name for the
 * session's agent in the workspace's folder, and opens its file in the
 * editor. A file that is there already is opened as it is.
 */
async function makeFile(make: Make, name: string): Promise<void> {
  pickingCommand.value = false;
  try {
    editor.show(await MakeAgentFile(make.path.replaceAll("%s", name), make.text.replaceAll("%s", name)));
  } catch (error) {
    reportError(error, "agent");
  }
}

/** focusBox puts the caret back in the prompt box when the command list closes. */
function focusBox(event: Event): void {
  event.preventDefault();
  box.value?.textareaRef?.focus();
}

/**
 * point keeps the row of the list of commands that the keys or the pointer
 * are on. The list hands out a new object for the same row each time it is
 * drawn, so a row with the label already kept changes nothing.
 */
function point(row: typeof pointed.value): void {
  if (row?.label !== pointed.value?.label) pointed.value = row;
}

/**
 * isSwitch reports whether one of the agent's settings is on or off: a
 * switch of the protocol's, or a list whose two choices are "on" and "off".
 */
function isSwitch(option: SessionConfigOption): boolean {
  const values = listChoices(option).map((choice) => choice.value);

  return option.type === "boolean" || (values.length === 2 && values.includes("on") && values.includes("off"));
}

/**
 * describeSource returns what the list of commands says of where a row comes
 * from: the user's own account, a plugin, or nothing for the agent's own.
 */
function describeSource(source = ""): string {
  if (source === YOURS) return `Installed for your user account, so every session of ${session.agent.name} has it.`;
  if (source === "Subagents") return `One of ${session.agent.name}'s subagents.`;

  return !source || source.endsWith("'s own") ? "" : `From the plugin ${source}.`;
}
</script>

<template>
  <AgentPlan :session="session" />
  <UChatPalette :ui="frame">
    <div class="flex flex-col" :class="frame.column">
      <div v-if="session.state === 'starting'" class="flex items-center gap-2 text-12 text-dimmed">
        <UIcon name="i-lucide-loader-circle" class="size-4 animate-spin" />{{ session.agent.name }} is starting…
      </div>
      <div v-else-if="session.state === 'sign-in'" class="flex max-w-180 flex-col gap-2.5">
        <span class="text-13 font-medium">Sign in to {{ session.agent.name }}</span>
        <span class="text-12 text-toned">
          {{ session.agent.name }} signs in to its own account, and PMT never sees it. A sign-in in PMT's Terminal comes
          back here once it is done.
        </span>
        <AgentSignIn
          :methods="session.methods"
          @choose="(method) => agent.signIn(session, method)"
          @done="agent.retry(session)"
        />
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

      <AgentTranscript
        v-if="session.entries.length"
        :session="session"
        :thread="session"
        @subagents="tray = 'subagents'"
      />
    </div>

    <template #prompt>
      <div>
        <AgentWork v-model:open="tray" :session="session" />
        <UChatPrompt
          ref="box"
          v-model="text"
          :placeholder="placeholder"
          :disabled="session.state !== 'idle' && session.state !== 'running'"
          variant="subtle"
          :rows="2"
          :class="[PROMPT_CARD, { 'ring-primary': asking }]"
          :ui="{ footer: 'flex-wrap', header: 'flex-col items-stretch' }"
          @submit="send"
          @close="stop"
        >
          <template #header>
            <!-- The plan's own text is never here: the pinned row opens it. -->
            <div v-if="asking" class="flex flex-col gap-2 border-b border-default pb-2">
              <span class="flex items-center gap-2 text-13 font-medium">
                <UIcon name="i-lucide-file-text" class="size-3.5 shrink-0 text-primary" />
                {{ session.agent.name }} finished its plan. Start on it?
              </span>
              <div class="flex flex-wrap items-center gap-1.5">
                <UFieldGroup v-if="approvals[0]" size="sm">
                  <UButton :label="approvals[0].name" @click="answer(approvals[0].id)" />
                  <UDropdownMenu
                    v-if="otherApprovals.length"
                    :items="otherApprovals"
                    :content="{ align: 'start', side: 'top' }"
                  >
                    <UButton icon="i-lucide-chevron-up" square :aria-label="`${session.agent.name}'s other choices`" />
                  </UDropdownMenu>
                </UFieldGroup>
                <UButton
                  v-for="option in refusals"
                  :key="option.id"
                  :label="option.name"
                  variant="ghost"
                  color="neutral"
                  size="sm"
                  @click="answer(option.id)"
                />
              </div>
            </div>
            <div v-if="session.queue.length" class="flex flex-col gap-1">
              <div
                v-for="queued in session.queue"
                :key="queued.id"
                class="flex h-6.5 items-center gap-1.5 rounded-sm bg-elevated ps-2 pe-1 text-12"
              >
                <UIcon name="i-lucide-clock" class="size-3 shrink-0 text-dimmed" />
                <span class="shrink-0 text-dimmed">Queued</span>
                <span class="min-w-0 grow truncate" :title="queued.text">{{ queued.text }}</span>
                <UButton
                  :label="busy ? 'Stop and send' : 'Send'"
                  variant="ghost"
                  size="xs"
                  @click="agent.sendQueued(session, queued.id)"
                />
                <UButton
                  icon="i-lucide-x"
                  color="error"
                  variant="ghost"
                  size="xs"
                  square
                  aria-label="Remove"
                  @click="agent.unqueue(session, queued.id)"
                />
              </div>
            </div>
            <div v-if="chips.length || images.length" class="flex flex-wrap gap-1.5">
              <span
                v-for="{ chip, isPage } in chips"
                :key="`${isPage}${chip.path}${chip.startLine}`"
                class="flex h-5.5 items-center gap-1.5 rounded-sm bg-accented ps-1.75 pe-1 text-11"
              >
                <UIcon :name="chip.path ? 'i-lucide-file' : 'i-lucide-scan-text'" class="size-3 text-dimmed" />
                <span :class="chip.path && 'font-mono'">{{ chip.label }}</span>
                <span v-if="chip.startLine" class="text-dimmed">
                  {{ chip.endLine > chip.startLine ? `${chip.startLine}–${chip.endLine}` : chip.startLine }}
                </span>
                <UButton
                  icon="i-lucide-x"
                  variant="ghost"
                  size="xs"
                  square
                  aria-label="Leave it out"
                  @click="leaveOut(chip, isPage)"
                />
              </span>
              <span
                v-for="image in images"
                :key="image.data"
                class="flex h-5.5 items-center gap-1.5 rounded-sm bg-accented ps-1 pe-1 text-11"
              >
                <img
                  :src="`data:${image.mimeType};base64,${image.data}`"
                  alt="A pasted picture"
                  class="size-4 rounded-xs object-cover"
                />
                Picture
                <UButton
                  icon="i-lucide-x"
                  variant="ghost"
                  size="xs"
                  square
                  aria-label="Leave it out"
                  @click="images = images.filter((other) => other !== image)"
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
                  placeholder="Find a file or a definition of the workspace"
                  class="h-72 w-96"
                />
              </template>
            </UPopover>
            <UPopover
              v-if="commands.length"
              v-model:open="pickingCommand"
              :content="{ align: 'start', side: 'top', onCloseAutoFocus: focusBox }"
            >
              <UTooltip text="Commands, skills and subagents (/)">
                <UButton
                  icon="i-lucide-square-slash"
                  variant="ghost"
                  size="sm"
                  square
                  aria-label="Commands, skills and subagents"
                />
              </UTooltip>
              <template #content>
                <!-- The palette would list only the first 12 commands. The
                     row the keys are on is read whole under the list. -->
                <UCommandPalette
                  v-model:search-term="commandTerm"
                  :groups="commands"
                  :fuse="{ resultLimit: session.commands.length + 50 }"
                  placeholder="Find a command, a skill or a subagent, or name a new one"
                  class="h-96 w-112"
                  @highlight="(payload) => point(payload?.value as typeof pointed)"
                >
                  <!-- The slot is always there: one that came and went
                       would draw the list again and move its highlight. -->
                  <template #footer>
                    <div
                      v-if="pointed?.suffix"
                      class="flex max-h-36 flex-col gap-1 overflow-y-auto bg-muted px-3 py-2.5"
                    >
                      <span class="font-mono text-12 font-medium">{{ pointed.label }}</span>
                      <p class="text-12 leading-4.5 text-toned">{{ pointed.suffix }}</p>
                      <p v-if="describeSource(pointed.source)" class="text-11 text-dimmed">
                        {{ describeSource(pointed.source) }}
                      </p>
                    </div>
                  </template>
                </UCommandPalette>
              </template>
            </UPopover>
            <template v-for="picker in pickers" :key="picker.category">
              <!-- A setting the agent lacks keeps its picker, off. -->
              <UTooltip
                v-if="!picker.option"
                :text="`${session.agent.name} has no ${picker.name.toLowerCase()} setting`"
              >
                <UButton :label="picker.name" variant="ghost" color="neutral" size="sm" disabled />
              </UTooltip>
              <UPopover v-else :content="{ align: 'start', side: 'top' }">
                <UButton
                  :label="picker.label"
                  trailing-icon="i-lucide-chevron-down"
                  variant="ghost"
                  color="neutral"
                  size="sm"
                  :disabled="picker.isPlanned"
                  :aria-label="picker.option.name"
                  :ui="{ trailingIcon: 'text-dimmed' }"
                />
                <template #content>
                  <div :class="PANEL">
                    <AgentSetting
                      :session="session"
                      :option="picker.option"
                      :title="picker.title"
                      :description="picker.description"
                      :hidden="picker.hidden"
                      :lines="EFFORTS"
                      :marks="picker.marks"
                    >
                      <div
                        v-if="picker.category === 'model'"
                        class="flex flex-col items-start gap-1.5 border-t border-default px-3 py-2.5"
                      >
                        <span class="text-13 font-semibold">This session</span>
                        <p v-if="session.usage?.cost" class="text-12 text-toned tabular-nums">
                          {{ describeCost(session.usage.cost) }} at API prices so far
                        </p>
                        <p v-if="starts.chat || starts.quick" class="text-11 leading-4 text-dimmed">
                          <template v-if="starts.chat">New sessions start on {{ starts.chat }}. </template>
                          <template v-if="starts.quick">
                            Quick sessions, the inline ask and a drafted commit message use {{ starts.quick }}.
                          </template>
                        </p>
                        <UButton
                          label="Change in Settings"
                          variant="outline"
                          color="neutral"
                          size="xs"
                          @click="commandBus.emit('settings.open', 'agents')"
                        />
                      </div>
                      <p
                        v-else-if="picker.category === 'mode' && session.agent.unasked?.length"
                        class="border-t border-default px-3 py-2.5 text-11 leading-4 text-dimmed"
                      >
                        A red choice lets {{ session.agent.name }} work without asking PMT. It asks you first and lasts
                        for this session only.
                      </p>
                    </AgentSetting>
                  </div>
                </template>
              </UPopover>
            </template>
            <!-- Plan is its own switch, whichever setting the agent keeps
                 it in. An agent with no plan mode keeps the switch, off. -->
            <UTooltip v-if="!plan" :text="`${session.agent.name} has no plan mode`">
              <USwitch label="Plan" size="xs" disabled />
            </UTooltip>
            <USwitch
              v-else
              :model-value="plan.isOn"
              label="Plan"
              size="xs"
              @update:model-value="
                (isOn) => plan && agent.configure(session, plan.option.configId, isOn ? plan.on : plan.off, false)
              "
            />
            <UPopover :content="{ align: 'start', side: 'top' }">
              <UButton icon="i-lucide-sliders-horizontal" variant="ghost" size="sm" square aria-label="More settings" />
              <template #content>
                <div :class="PANEL">
                  <label
                    v-for="row in switches"
                    :key="row.key"
                    class="flex items-center gap-3 border-b border-default px-3 py-2.5"
                  >
                    <span class="flex min-w-0 grow flex-col gap-0.5">
                      <span class="text-13 font-semibold" :class="row.isRed && 'text-error'">{{ row.name }}</span>
                      <span v-if="row.description" class="text-12 leading-4.5 text-toned">{{ row.description }}</span>
                    </span>
                    <USwitch :model-value="row.isOn" size="sm" :aria-label="row.name" @update:model-value="row.set" />
                  </label>
                  <AgentSetting
                    v-for="option in others.filter((row) => !isSwitch(row))"
                    :key="option.configId"
                    :session="session"
                    :option="option"
                  />
                </div>
              </template>
            </UPopover>
            <AgentUsage :session="session" />
            <span class="grow" />
            <span v-if="waiting" class="text-11 text-dimmed">Waiting for you</span>
            <span
              v-else-if="busy"
              class="text-11 text-dimmed tabular-nums"
              :aria-label="`${session.agent.name} has worked for ${elapsed}`"
              >{{ elapsed }}</span
            >
            <UButton
              v-if="busy"
              label="Stop"
              icon="i-lucide-square"
              color="error"
              variant="soft"
              size="sm"
              @click="agent.cancel(session)"
            />
            <UTooltip
              v-if="!busy || text.trim()"
              :text="busy && !session.agent.steers ? 'Queue it for when this turn ends' : 'Send'"
            >
              <UChatPromptSubmit :disabled="(session.state !== 'idle' && !busy) || !text.trim()" size="sm" square />
            </UTooltip>
          </template>
        </UChatPrompt>
      </div>
    </template>
  </UChatPalette>
</template>
