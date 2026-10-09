<script setup lang="ts">
/**
 * The chosen card's table (the Health board's left pane): its title, what it lists and a filter, then its
 * rows. Each column's head sorts by it and, where its values are few, keeps only the ones ticked. Rows are
 * drawn as they scroll into view.
 */
import { computed, h, ref, resolveComponent, shallowRef, useTemplateRef } from "vue";
import type { TableColumn } from "@nuxt/ui";
import type { ColumnFiltersState, HeaderContext, Row as TableRow, SortingState } from "@tanstack/vue-table";
import type { Contest, Dangling, Dependency, Problem } from "@services/models";
import type { File as SourceFile } from "@services/internal/inventory/models";
import { CARDS, typeOf, type Card, type Gap, type Row } from "../../composables/useHealth";
import { useWorkspaceStore } from "../../stores/workspace";
import OriginDot from "../ui/OriginDot.vue";
import { plural } from "../../lib/format";

const { card, rows, sources, loading } = defineProps<{
  card: Card;
  rows: Row[];
  sources: SourceFile[];
  loading: boolean;
}>();
/** The open row. */
const open = defineModel<Row | null>({ required: true });

const store = useWorkspaceStore();
const UBadge = resolveComponent("UBadge");
const UButton = resolveComponent("UButton");
const UDropdownMenu = resolveComponent("UDropdownMenu");
const table = useTemplateRef<{ tableApi?: { getRowModel(): { rows: TableRow<Row>[] } } }>("table");
const filter = shallowRef<string | undefined>("");
const sorting = ref<SortingState | undefined>([]);
const filters = ref<ColumnFiltersState | undefined>([]);

const originOf = (file: number) => sources[file]?.origin ?? "";
const isProblem = (row: Row): row is Problem => "message" in row;

// an origin as its color's dot, then by name, cut short with its whole name on hover; several as their
// dots and how many
const dot = (id: string) => h(OriginDot, { color: store.originColor(id), small: true });
const origin = (id: string) =>
  h("span", { class: "flex min-w-0 items-center gap-1.5", title: store.originName(id) }, [
    dot(id),
    h("span", { class: "truncate" }, store.originName(id)),
  ]);
const origins = (ids: string[]) =>
  h("span", { class: "inline-flex items-center gap-0.75", title: ids.map(store.originName).join(", ") }, [
    ...ids.map(dot),
    h("span", { class: "ms-0.75 text-dimmed tabular-nums" }, String(ids.length)),
  ]);
const badge = (label: string, color = "neutral") => h(UBadge, { label, color, size: "sm" });
const mono = (text: string) => h("span", { class: "font-mono" }, text);
// a problem row's type, in its color; the same column heads each problem card
const typeColumn = <T extends Row>(): TableColumn<T> => ({
  id: "type",
  header: head("Type", String),
  accessorFn: (row) => typeOf(row).label,
  filterFn: among,
  meta: width("w-30"),
  cell: ({ row }) => badge(typeOf(row.original).label, typeOf(row.original).color),
});

/** Keeps a row whose value is among those ticked in its column's head, or every row when none is. */
function among<T>(row: TableRow<T>, id: string, wanted: string[] | undefined): boolean {
  return !wanted?.length || wanted.includes(String(row.getValue(id)));
}

/**
 * A column's head: a button that shows it sorts (and filters, given how its values are named) and opens a
 * menu to sort by it and tick the values to keep.
 */
function head<T>(label: string, name?: (value: string) => string) {
  return ({ column, table }: HeaderContext<T, unknown>) => {
    const wanted = (column.getFilterValue() as string[] | undefined) ?? [];
    const values = name
      ? [...new Set(table.getPreFilteredRowModel().rows.map((row) => String(row.getValue(column.id))))].sort()
      : [];
    const tick = (value: string, checked: boolean) =>
      column.setFilterValue(checked ? [...wanted, value] : wanted.filter((kept) => kept !== value));
    const items = [
      [
        { label: "Sort ascending", icon: "i-lucide-arrow-up", onSelect: () => column.toggleSorting(false) },
        { label: "Sort descending", icon: "i-lucide-arrow-down", onSelect: () => column.toggleSorting(true) },
      ],
      values.map((value) => ({
        type: "checkbox" as const,
        label: name?.(value) || "(none)",
        // an origin leads with its color's dot, as in the rows
        slot: name === store.originName ? ("origin" as const) : undefined,
        value,
        checked: wanted.includes(value),
        onUpdateChecked: (checked: boolean) => tick(value, checked),
        onSelect: (event: Event) => event.preventDefault(),
      })),
    ];
    const sorted = column.getIsSorted();
    return h(
      UDropdownMenu,
      { items, content: { align: "start" } },
      {
        default: () =>
          h(UButton, {
            label: wanted.length ? `${label} (${wanted.length})` : label,
            icon: name ? "i-lucide-list-filter" : undefined,
            trailingIcon:
              sorted === "asc"
                ? "i-lucide-arrow-up"
                : sorted === "desc"
                  ? "i-lucide-arrow-down"
                  : "i-lucide-chevrons-up-down",
            color: wanted.length ? "primary" : "neutral",
            variant: "ghost",
            size: "xs",
            class: "-mx-2 font-medium",
            ui: { leadingIcon: "size-3.5 text-dimmed", trailingIcon: "size-3.5 text-dimmed" },
          }),
        "origin-leading": ({ item }: { item: { value: string } }) => dot(item.value),
      },
    );
  };
}

/** A fixed column width, as the board's colgroup sets it. */
const width = (cls: string) => ({ class: { th: cls, td: cls } });

const contests: TableColumn<Contest>[] = [
  { accessorKey: "kind", header: head("Kind", String), filterFn: among, meta: width("w-32.5") },
  { accessorKey: "name", header: head("Name"), cell: ({ row }) => mono(row.original.name) },
  {
    accessorKey: "rule",
    header: head("Rule", String),
    filterFn: among,
    meta: width("w-22"),
    cell: ({ row }) => badge(row.original.rule, row.original.firstWins ? "warning" : "info"),
  },
  {
    id: "sites",
    header: head("Defined by"),
    accessorFn: (row) => row.sites?.length ?? 0,
    meta: width("w-25"),
    cell: ({ row }) => origins((row.original.sites ?? []).map((site) => originOf(site.file))),
  },
  {
    id: "kept",
    header: head("Kept", store.originName),
    accessorFn: (row) => originOf(row.sites?.[row.winner]?.file ?? -1),
    filterFn: among,
    meta: width("w-44"),
    cell: ({ getValue }) => origin(String(getValue())),
  },
];
const depends: TableColumn<Dependency>[] = [
  {
    accessorKey: "from",
    header: head("Mod", store.originName),
    filterFn: among,
    cell: ({ row }) => origin(row.original.from),
  },
  {
    id: "relation",
    header: head("Relation", String),
    accessorFn: (row) => (row.affected ? "affected by" : "depends on"),
    filterFn: among,
    meta: width("w-28"),
    cell: ({ row }) =>
      badge(row.original.affected ? "affected by" : "depends on", row.original.affected ? "warning" : "info"),
  },
  {
    accessorKey: "to",
    header: head("Other mod", store.originName),
    filterFn: among,
    cell: ({ row }) => origin(row.original.to),
  },
  { id: "uses", header: head("Uses"), accessorFn: (row) => row.sites?.length ?? 0, meta: width("w-20") },
];
// a finding the card lists beside its own rows takes its message in the name's column
const dangling: TableColumn<Dangling | Problem>[] = [
  typeColumn(),
  {
    id: "name",
    header: head("Name"),
    accessorFn: (row) => (isProblem(row) ? row.message : row.name),
    cell: ({ row }) => (isProblem(row.original) ? row.original.message : mono(row.original.name)),
  },
  {
    id: "kind",
    header: head("Kind", String),
    accessorFn: (row) => (isProblem(row) ? "" : row.kind),
    filterFn: among,
    meta: width("w-32.5"),
  },
  {
    id: "origins",
    header: head("Written by"),
    accessorFn: (row) => (isProblem(row) ? 1 : (row.sites?.length ?? 0)),
    meta: width("w-25"),
    cell: ({ row }) =>
      origins(
        isProblem(row.original)
          ? [originOf(row.original.site.file)]
          : (row.original.sites ?? []).map((site) => originOf(site.file)),
      ),
  },
];
// a row's language and file are where it is fixed: a gap's, the language it is missing in and its file there
const loc: TableColumn<Gap | Problem>[] = [
  typeColumn(),
  {
    id: "key",
    header: head("Key or problem"),
    accessorFn: (row) => (isProblem(row) ? row.message : row.key),
    cell: ({ row }) => (isProblem(row.original) ? row.original.message : mono(row.original.key)),
  },
  {
    id: "language",
    header: head("Language", String),
    accessorFn: (row) => row.language ?? "",
    filterFn: among,
    meta: width("w-28"),
  },
  {
    id: "file",
    header: head("File"),
    accessorFn: (row) =>
      isProblem(row) ? `${sources[row.site.file]?.rel}:${row.site.line + 1}` : (sources[row.target]?.rel ?? ""),
    cell: ({ getValue }) => mono(String(getValue())),
  },
  {
    id: "origin",
    header: head("Origin", store.originName),
    accessorFn: (row) => originOf(isProblem(row) ? row.site.file : row.file),
    filterFn: among,
    meta: width("w-44"),
    cell: ({ getValue }) => origin(String(getValue())),
  },
];
const script: TableColumn<Problem>[] = [
  typeColumn(),
  { accessorKey: "message", header: head("Finding") },
  {
    id: "origin",
    header: head("Origin", store.originName),
    accessorFn: (row) => originOf(row.site.file),
    filterFn: among,
    meta: width("w-44"),
    cell: ({ getValue }) => origin(String(getValue())),
  },
];
// each card's columns are typed by its own rows; the table holds whichever the card picks
const columns = computed(
  () => (({ contests, overrides: contests, depends, dangling, loc, script }) as Record<Card, TableColumn<Row>[]>)[card],
);

// the open row, by its place in the rows
const selection = computed(() => {
  const at = open.value ? rows.indexOf(open.value) : -1;
  return at < 0 ? {} : { [at]: true };
});

// rows striped faintly by their place as shown, sorted and filtered
function stripe(row: TableRow<Row>): string {
  return (table.value?.tableApi?.getRowModel().rows.indexOf(row) ?? row.index) % 2 ? "bg-elevated/40" : "";
}

function select(_event: Event, row: { original: Row }): void {
  open.value = row.original;
}

// a virtualized row is drawn at the board's 30px, at the page's scale
const rowHeight = () => (30 / 16) * parseFloat(getComputedStyle(document.documentElement).fontSize);
</script>

<template>
  <section class="flex min-h-0 min-w-0 flex-col bg-muted">
    <div class="flex h-11 shrink-0 items-center gap-2.5 border-b border-default px-3">
      <span class="text-13 font-semibold">{{ CARDS[card].label }}</span>
      <span class="truncate text-12 text-dimmed">{{ CARDS[card].blurb }}</span>
      <span class="grow" />
      <UInput
        v-model="filter"
        icon="i-lucide-filter"
        size="sm"
        :placeholder="`Filter ${plural(rows.length, 'row')}`"
        class="w-45 shrink-0"
      />
    </div>
    <UTable
      ref="table"
      v-model:global-filter="filter"
      v-model:sorting="sorting"
      v-model:column-filters="filters"
      :data="rows"
      :columns="columns"
      :loading="loading"
      sticky
      :virtualize="{ estimateSize: rowHeight() }"
      empty="Nothing to list."
      :ui="{ base: 'w-full table-fixed', td: 'truncate', tr: 'cursor-pointer' }"
      :row-selection="selection"
      :meta="{ class: { tr: stripe } }"
      class="min-h-0 flex-1"
      @select="select"
    />
  </section>
</template>
