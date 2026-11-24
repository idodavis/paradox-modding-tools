<!-- Conditions in words: one a line, the subject and the value in mono, a block's conditions indented under it. -->
<script setup lang="ts">
import { createReusableTemplate } from "@vueuse/core";

import type { Condition } from "@services/atlas/models";

defineProps<{ conditions: Condition[] }>();

const CODE = "font-mono text-11 wrap-anywhere";
const VALUE = "rounded-[4px] bg-(--link-soft) px-1 text-(--link) box-decoration-clone";

const [DefineCode, Code] = createReusableTemplate<{ text: string; name?: string; isValue?: boolean }>();

/** splitPrefix returns a written value's prefix with its colon, as estate_type:, and what follows it. */
function splitPrefix(text: string): [prefix: string, rest: string] {
  const end = text.indexOf(":") + 1;
  return [text.slice(0, end), text.slice(end)];
}
</script>

<template>
  <ul class="flex min-w-0 flex-col gap-0.5">
    <DefineCode v-slot="{ text, name, isValue }">
      <code :class="[CODE, isValue ? VALUE : 'text-(--fg-1)']" :title="name ? text : undefined">
        <template v-if="name">{{ name }}</template>
        <template v-else>
          <span class="opacity-60">{{ splitPrefix(text)[0] }}</span>
          <span>{{ splitPrefix(text)[1] }}</span>
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
        <Code v-if="condition.value" :text="condition.value" :name="condition.title" is-value />
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
