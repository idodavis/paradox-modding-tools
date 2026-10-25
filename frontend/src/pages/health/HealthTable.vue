<!--
  The findings of one section as a table, one row to a finding. Each column sorts, a column with few values filters,
  and selecting a row opens it.
-->
<script lang="ts">
/** The badge color of a rule by Go's word for it. A rule without a row is information. */
export const RULE_COLORS: Record<string, "warning"> = { FIOS: "warning" };
</script>

<script setup lang="ts">
import { computed, shallowRef, useTemplateRef } from "vue";
import { useQuery } from "@pinia/colada";
import type { TableColumn, TableRow } from "@nuxt/ui";
import type { Column, Table } from "@tanstack/vue-table";
import type { Finding, Section } from "@services/health/models";
import { plural } from "../../lib/format";
import { codesQuery } from "../../queries";
import { useWorkspaceStore } from "../../stores/workspace";
import OriginChip from "../../ui/OriginChip.vue";
import SectionHead from "../../ui/SectionHead.vue";

const { opened = -1 } = defineProps<{
  findings: Finding[];
  section: Section;
  loading: boolean;
  /** The index of the open finding, or -1 for none. */
  opened?: number;
}>();
const emit = defineEmits<{ open: [index: number] }>();

/** The badge color of a code by its severity: 1 is an error, 2 a warning, 3 information and 4 a hint. */
const SEVERITY_COLORS = ["error", "warning", "info", "neutral"] as const;
const SORT_ICONS: Record<string, string> = {
  asc: "i-lucide-arrow-up",
  desc: "i-lucide-arrow-down",
  false: "i-lucide-chevrons-up-down",
};

const workspace = useWorkspaceStore();
const { data: codes } = useQuery(codesQuery);
const filter = shallowRef<string>();
const table = useTemplateRef<{ tableApi?: { getRowModel(): { rows: TableRow<Finding>[] } } }>("table");
// a virtualized row is 30px at the page's text size
const rowHeight = (30 / 16) * parseFloat(getComputedStyle(document.documentElement).fontSize);

const columns = computed((): TableColumn<Finding>[] => [
  {
    id: "code",
    header: "Finding",
    accessorFn: (finding) => findCode(finding.code)?.label ?? finding.code,
    filterFn: matchChecks,
    meta: { class: { th: "w-44", td: "w-44" } },
  },
  {
    id: "name",
    header: "Name",
    accessorFn: (finding) => finding.name,
    enableColumnFilter: false,
  },
  {
    id: "database",
    header: "Database",
    accessorFn: (finding) => finding.database,
    filterFn: matchChecks,
    meta: { class: { th: "w-36", td: "w-36" } },
  },
  {
    id: "rule",
    header: "Rule",
    accessorFn: (finding) => finding.facts?.rule ?? "",
    filterFn: matchChecks,
    meta: { class: { th: "w-22", td: "w-22" } },
  },
  {
    id: "origin",
    header: "Origin",
    accessorFn: (finding) => finding.origin,
    filterFn: matchChecks,
    meta: { class: { th: "w-44", td: "w-44" } },
  },
  {
    id: "sites",
    header: "Sites",
    accessorFn: (finding) => finding.sites?.length ?? 0,
    enableColumnFilter: false,
    meta: { class: { th: "w-24", td: "w-24" } },
  },
]);
const meta = { class: { tr: resolveRowClass } };

/** Finds the code with the given id. It returns nothing for an id Go lists no code for. */
function findCode(id: string) {
  return codes.value?.find((code) => code.id === id);
}

/** Resolves the badge color of a finding's code from the code's severity. */
function resolveColor(finding: Finding) {
  return SEVERITY_COLORS[(findCode(finding.code)?.severity ?? 4) - 1] ?? "neutral";
}

/** Resolves the classes of a row: lit for the open one, else striped by its place as shown. */
function resolveRowClass(row: TableRow<Finding>): string {
  if (row.index === opened) return "bg-(--accent-soft)";
  return (table.value?.tableApi?.getRowModel().rows.indexOf(row) ?? row.index) % 2 ? "bg-elevated/40" : "";
}

/** Lists the origins of a finding's places, each once. */
function listOrigins(finding: Finding): string[] {
  return [...new Set(finding.sites?.map((site) => site.place.origin))];
}

/** Lists the values checked in a column's head. */
function listChecks(column: Column<Finding>): string[] {
  const checks = column.getFilterValue();
  return Array.isArray(checks) ? checks.map(String) : [];
}

/** Keeps a row whose value is one of those checked in its column's head, or every row when none is checked. */
function matchChecks(row: TableRow<Finding>, id: string, checks: string[] | undefined): boolean {
  return !checks?.length || checks.includes(String(row.getValue(id)));
}

/** Opens the clicked row, by its index among the findings given. */
function select(_event: Event, row: TableRow<Finding>): void {
  emit("open", row.index);
}

/** Describes a column's head: its title and, when values are checked in it, how many. */
function describeHead(column: Column<Finding>): string {
  const title = String(column.columnDef.header);
  const count = listChecks(column).length;
  return count ? `${title} (${count})` : title;
}

/** Builds the menu of a column's head: sorting it and, where it filters, checking the values to keep. */
function buildMenu(column: Column<Finding>, rowTable: Table<Finding>) {
  const checks = listChecks(column);
  const isOrigin = column.id === "origin";
  const options = column.getCanFilter()
    ? [...new Set(rowTable.getPreFilteredRowModel().rows.map((row) => String(row.getValue(column.id))))].sort()
    : [];
  const values = options.map((option) => ({
    type: "checkbox" as const,
    label: (isOrigin ? workspace.originName(option) : option) || "(none)",
    slot: undefined,
    value: option,
    checked: checks.includes(option),
    onUpdateChecked: (checked: boolean) =>
      column.setFilterValue(checked ? [...checks, option] : checks.filter((entry) => entry !== option)),
    onSelect: (event: Event) => event.preventDefault(),
  }));
  return [
    [
      { label: "Sort ascending", icon: "i-lucide-arrow-up", onSelect: () => column.toggleSorting(false) },
      { label: "Sort descending", icon: "i-lucide-arrow-down", onSelect: () => column.toggleSorting(true) },
    ],
    // an origin leads with its color's dot, in a slot of its own
    isOrigin ? values.map((item) => ({ ...item, slot: "origin" as const })) : values,
  ].filter((group) => group.length);
}
</script>

<template>
  <section class="flex min-h-0 min-w-0 flex-col bg-muted">
    <SectionHead :title="section.label" :subtitle="section.note">
      <UInput
        v-model="filter"
        icon="i-lucide-filter"
        size="sm"
        :placeholder="`Filter ${plural(findings.length, 'row')}`"
        class="w-45 shrink-0"
      />
    </SectionHead>
    <UTable
      ref="table"
      v-model:global-filter="filter"
      :data="findings"
      :columns="columns"
      :meta="meta"
      :loading="loading"
      sticky
      :virtualize="{ estimateSize: rowHeight }"
      empty="Nothing to list."
      :ui="{ td: 'truncate', tr: 'cursor-pointer' }"
      class="min-h-0 flex-1"
      @select="select"
    >
      <template v-for="{ id } in columns" :key="id" #[`${id}-header`]="{ column, table: rowTable }">
        <UDropdownMenu :items="buildMenu(column, rowTable)" :content="{ align: 'start' }">
          <UButton
            :label="describeHead(column)"
            :icon="column.getCanFilter() ? 'i-lucide-list-filter' : undefined"
            :trailing-icon="SORT_ICONS[String(column.getIsSorted())]"
            :color="listChecks(column).length ? 'primary' : 'neutral'"
            variant="ghost"
            size="xs"
            class="-mx-2 font-medium"
            :ui="{ leadingIcon: 'size-3.5 text-dimmed', trailingIcon: 'size-3.5 text-dimmed' }"
          />
          <template #origin-leading="{ item }">
            <OriginChip :origin="item.value" dot />
          </template>
        </UDropdownMenu>
      </template>
      <template #code-cell="{ row, getValue }">
        <UBadge :label="String(getValue())" :color="resolveColor(row.original)" size="sm" />
      </template>
      <template #name-cell="{ row, getValue }">
        <span :class="{ 'font-mono': row.original.name }">{{ getValue() }}</span>
      </template>
      <template #rule-cell="{ getValue }">
        <UBadge
          v-if="getValue()"
          :label="String(getValue())"
          :color="RULE_COLORS[String(getValue())] ?? 'info'"
          size="sm"
        />
      </template>
      <template #origin-cell="{ row }">
        <OriginChip v-if="row.original.origin" :origin="row.original.origin" />
      </template>
      <template #sites-cell="{ row, getValue }">
        <span class="inline-flex items-center gap-0.75 tabular-nums">
          <OriginChip v-for="origin in listOrigins(row.original)" :key="origin" :origin="origin" dot />
          <span class="ms-0.75 text-dimmed">{{ getValue() }}</span>
        </span>
      </template>
    </UTable>
  </section>
</template>
