<!--
  Conditions in words: each on its line, a block's head ending in a colon with the conditions it holds under it, or
  all on one line, a block's conditions shown as "…". Its module also sorts conditions into the ones that must be true
  and the ones that must be false, and puts the blocks a firing is written in into words.
-->
<script lang="ts">
import type { Condition, Step } from "@services/graph/models";

/** The classes of a subject or a value in a line of words; a long one wraps. */
const CODE = "font-mono text-11 wrap-anywhere";
/** The classes that mark a value, the thing a condition checks for: one color, on a soft tag of it. */
const VALUE = "rounded-[4px] bg-(--link-soft) px-1 text-(--link) box-decoration-clone";
/** The phrase each kind of step starts with; a block says nothing. */
const STEP_PHRASES: Record<string, string> = {
  branch: "if",
  weight: "weight",
  list: "for",
  scope: "in",
  option: "option",
};

/**
 * Sorts the conditions a trigger, a limit or a when holds under two heads, "must be true" and "must be false", the
 * second's no longer marked negated, where something must be false. A list with nothing that must be false is returned
 * as it is.
 */
export function groupConditions(conditions: Condition[]): Condition[] {
  const failed = conditions.filter((condition) => condition.negated);
  if (!failed.length) return conditions;
  return [
    { text: "must be true", conditions: conditions.filter((condition) => !condition.negated), line: 0 },
    { text: "must be false", conditions: failed.map((condition) => ({ ...condition, negated: false })), line: 0 },
  ].filter((group) => group.conditions.length);
}

/**
 * Describes one block a firing is written in as conditions: an if with its conditions under it, "for random_player"
 * with "where" and the list's limit under it, an option by its name where it has one, chosen.
 */
export function describeStep(step: Step): Condition[] {
  const phrase = STEP_PHRASES[step.kind];
  if (!phrase) return [];
  const { line } = step;
  const conditions = groupConditions(step.when ?? []);
  if (step.kind === "branch") return [{ text: step.key === "else" ? "else" : "if", conditions, line }];
  const when = conditions.length ? [{ text: step.kind === "list" ? "where" : "when", conditions, line }] : [];
  if (step.kind === "option")
    return [{ text: `${phrase} ${step.title ? `“${step.title}” ` : ""}chosen`, line }, ...when];
  const value = step.kind === "weight" ? `${step.weight}${step.modifiers ? "±" : ""} in ${step.key}` : step.key;
  return [{ text: phrase, value, line }, ...when];
}
</script>

<script setup lang="ts">
import { createReusableTemplate } from "@vueuse/core";

const { conditions, inline = false } = defineProps<{ conditions: Condition[]; inline?: boolean }>();
// a subject or a value: its in-game name where it has one, the text as written on hover, else the text; a value
// marked as one
const [DefineCode, Code] = createReusableTemplate<{ text: string; name?: string; isValue?: boolean }>();

/**
 * Splits a subject or a value as written into the prefix that names its kind, such as estate_type:, and the name after
 * it. One with no such prefix is all name.
 */
function splitValue(value: string): [string, string] {
  const match = /^([\w.]+:)(.+)$/.exec(value);
  return match ? [match[1], match[2]] : ["", value];
}
</script>

<template>
  <span v-if="inline" class="min-w-0 truncate">
    <template v-for="(condition, position) in conditions" :key="position">
      <template v-if="position"> · </template>
      <template v-if="condition.negated">not </template>
      <code v-if="condition.subject" class="font-mono">{{ condition.subject }}</code>
      {{ condition.text }}
      <code v-if="condition.value" class="font-mono">{{ condition.title || condition.value }}</code>
      <template v-if="condition.conditions?.length"> …</template>
    </template>
  </span>
  <ul v-else class="flex min-w-0 flex-col gap-0.5">
    <DefineCode v-slot="{ text, name, isValue }">
      <code :class="[CODE, isValue ? VALUE : 'text-(--fg-1)']" :title="name ? text : undefined">
        <template v-if="name">{{ name }}</template>
        <template v-else>
          <span class="opacity-60">{{ splitValue(text)[0] }}</span>
          <span>{{ splitValue(text)[1] }}</span>
        </template>
      </code>
    </DefineCode>
    <li v-for="(condition, position) in conditions" :key="position" class="flex min-w-0 flex-col gap-0.5">
      <span
        class="block min-w-0 [&>*+*]:ms-1.5"
        :class="condition.conditions?.length ? 'head text-(--fg-1)' : 'item text-(--fg-2)'"
      >
        <span v-if="condition.negated" class="text-(--fg-3)">not</span>
        <Code
          v-if="condition.subject"
          :text="condition.subject"
          :name="condition.value ? '' : condition.title"
          :is-value="!condition.value && !!condition.title"
        />
        <span v-if="condition.text">{{ condition.text }}</span>
        <Code v-if="condition.value" :text="condition.value" :name="condition.title" :is-value="true" />
      </span>
      <ConditionList
        v-if="condition.conditions?.length"
        :conditions="condition.conditions"
        class="ms-1 border-s border-(--line-2) ps-3"
      />
    </li>
  </ul>
</template>

<style scoped>
/* a block's head ends in a colon, the conditions it holds under it */
.head::after {
  content: ":";
}
/* a condition that holds no others is a bullet point */
.item {
  position: relative;
  padding-inline-start: 0.875rem;
}
.item::before {
  content: "•";
  position: absolute;
  inset-inline-start: 0.125rem;
  color: var(--fg-3);
}
</style>
