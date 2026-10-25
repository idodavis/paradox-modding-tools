<!--
  The Form view of a definition: one field per input the database offers. A change is written to the file as one edit.
-->
<script setup lang="ts">
import { computed } from "vue";
import { useMutation, useQuery, useQueryCache } from "@pinia/colada";

import { ValueType, type Input } from "@internal/schema/models";
import * as draw from "@services/draw/service";
import type { Range } from "@services/lsp/models";

import { inputsQuery } from "../../queries";
import ValueField from "../../ui/ValueField.vue";
import { applyEdits, syncText, type Model } from "./editor/models";

const { model, database } = defineProps<{ model: Model; database: string }>();

/** What edits each kind of value; the other kinds are typed as text. */
const KINDS: Partial<Record<ValueType, "switch" | "number" | "choice" | "color">> = {
  [ValueType.ValueBool]: "switch",
  [ValueType.ValueNumber]: "number",
  [ValueType.ValueChoice]: "choice",
  [ValueType.ValueColor]: "color",
};

type Row = { input: Input; path: string; depth: number; block: Range };

const queryCache = useQueryCache();
const queryOptions = () => inputsQuery({ path: model.place.path, line: model.caret.line });
const { data: form, error } = useQuery(queryOptions);
// the inputs in order, a block's under it, with the block each is written in; a block not written has none shown
const rows = computed(() => {
  const definition = form.value;
  const flatten = (list: Input[], parent: string, block: Range, depth: number): Row[] =>
    list.flatMap((input) => {
      const path = parent ? `${parent}.${input.key}` : input.key;
      const inner = definition?.blocks?.[path];
      return [{ input, path, depth, block }, ...(inner ? flatten(input.inputs ?? [], path, inner, depth + 1) : [])];
    });
  return definition ? flatten(definition.inputs ?? [], "", definition.range, 0) : [];
});

const { mutate: change } = useMutation({
  mutation: ({ row, value }: { row: Row; value: string }) =>
    draw.EditProperty(model.place.path, row.block, row.input.key, value),
  onSuccess: async (edits) => {
    applyEdits(model, edits ?? []);
    await syncText(model);
    await queryCache.invalidateQueries({ key: queryOptions().key });
  },
});
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col overflow-y-auto bg-(--bg-0)">
    <p v-if="error" class="px-4 py-3 text-12 text-dimmed">No definition is at the cursor.</p>
    <template v-else-if="form">
      <p class="px-4 pt-3 pb-1 font-mono text-12 text-dimmed">{{ database }}</p>
      <div v-for="row in rows" :key="row.path" :style="{ paddingInlineStart: `${row.depth * 1.25}rem` }">
        <p v-if="row.input.type === ValueType.ValueBlock" class="px-4 pt-2 text-12 font-medium text-toned">
          {{ row.input.key }}
        </p>
        <ValueField
          v-else
          :kind="KINDS[row.input.type] ?? 'text'"
          :value="form.values?.[row.path] ?? row.input.default ?? ''"
          :label="row.input.required ? `${row.input.key} *` : row.input.key"
          :help="row.input.blurb"
          :choices="row.input.choices ?? []"
          @update:value="(value) => change({ row, value })"
        />
      </div>
    </template>
  </div>
</template>
