<!-- Shows conditions as words, one a line, a block's statements under it. -->
<script lang="ts">
import type { Condition } from "@services/graph/models";

/** CODE is the classes of a subject or a value; a long one wraps. */
const CODE = "font-mono text-11 wrap-anywhere";
/** VALUE is the classes that mark a value as colored text on a soft tag. */
const VALUE = "rounded-[4px] bg-(--link-soft) px-1 text-(--link) box-decoration-clone";
/**
 * writeConditions writes conditions as plain text, one a line, a block's
 * statements indented under it.
 */
export function writeConditions(conditions: Condition[], indent = ""): string {
  return conditions
    .map((condition) => {
      const words = [
        condition.negated && "not",
        condition.subject,
        condition.text,
        condition.value && (condition.title || condition.value),
      ];
      const line = indent + words.filter(Boolean).join(" ");
      const inner = condition.conditions?.length ? `\n${writeConditions(condition.conditions, `${indent}  `)}` : "";
      return line + inner;
    })
    .join("\n");
}
</script>

<script setup lang="ts">
import { createReusableTemplate } from "@vueuse/core";

const { conditions } = defineProps<{ conditions: Condition[] }>();
// Code shows a subject or a value by its in-game name, with the text as written on hover.
const [DefineCode, Code] = createReusableTemplate<{ text: string; name?: string; isValue?: boolean }>();

/** splitValue splits a subject or a value into its prefix, such as estate_type:, and the rest. */
function splitValue(value: string): [string, string] {
  const match = /^([\w.]+:)(.+)$/.exec(value);
  return match ? [match[1], match[2]] : ["", value];
}
</script>

<template>
  <ul class="flex min-w-0 flex-col gap-0.5">
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
.head::after {
  content: ":";
}
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
