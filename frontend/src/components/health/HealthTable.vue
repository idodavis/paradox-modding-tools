<script setup lang="ts">
/**
 * The report's table: the card chosen above picks the rows it lists and the columns it shows. Each
 * column's head sorts by it, and where its values are few, keeps only the ones ticked. Every row is
 * listed, rendered as it scrolls into view; the rows the filters leave are told to the page.
 */
import { computed, h, ref, resolveComponent, shallowRef, useTemplateRef, watch } from "vue";
import type { TableColumn } from "@nuxt/ui";
import type { FilterFn, HeaderContext } from "@tanstack/vue-table";
import type { Contest, Dangling, Dependency, FileRow, Health, LocRow } from "@services/models";
import { originHexByOriginId } from "../../ide/rootDecorations";
import { useWorkspaceStore } from "../../stores/workspace";

export type Card = "contests" | "overrides" | "depends" | "dangling" | "loc" | "files";
/** A loc row, with the language and the gap it is listed under. */
export type Gap = LocRow & { language: string; gap: "missing" | "untranslated" | "orphaned" };
export type Row = Contest | Dependency | Dangling | Gap | FileRow;

const props = defineProps<{ health: Health | null | undefined; card: Card; loading: boolean }>();
const emit = defineEmits<{ select: [row: Row]; shown: [card: Card, rows: Row[]] }>();
const store = useWorkspaceStore();
const UBadge = resolveComponent("UBadge");
const UButton = resolveComponent("UButton");
const UDropdownMenu = resolveComponent("UDropdownMenu");
const table = useTemplateRef<{ tableApi?: { getFilteredRowModel(): { rows: { original: unknown }[] } } }>("table");
const filter = shallowRef<string | undefined>("");
const sorting = ref<{ id: string; desc: boolean }[] | undefined>([]);
const filters = ref<{ id: string; value: unknown }[] | undefined>([]);
const picked = ref<Record<string, boolean> | undefined>({});

// a new card starts unsorted and unfiltered, with no row open
watch(
  () => props.card,
  () => {
    sorting.value = [];
    filters.value = [];
    picked.value = {};
  },
);

// the origin of a row's file, by its number in the report's sources
const originOf = (file: number | undefined) => (file === undefined ? "" : (props.health?.sources?.[file]?.origin ?? ""));

// an origin as its colour's dot, then by name; several as their dots and how many
const dot = (id: string) =>
  h("span", { class: "size-2 shrink-0 rounded-full", style: { backgroundColor: originHexByOriginId(id) } });
const origin = (id: string) =>
  h("span", { class: "inline-flex items-center gap-1.5" }, [dot(id), store.originName(id)]);
const origins = (ids: string[]) =>
  h("span", { class: "inline-flex items-center gap-1", title: ids.map(store.originName).join(", ") }, [
    ...ids.map(dot),
    h("span", { class: "ml-1 text-xs tabular-nums text-muted" }, String(ids.length)),
  ]);
const badge = (label: string, color: string) => h(UBadge, { label, color, variant: "subtle" });
// each loc gap's colour, matching its card
const GAP_COLOR = { missing: "primary", untranslated: "tertiary", orphaned: "success" };

// keeps a row whose value is among those ticked in its column's head, or every row when none is
const among: FilterFn<any> = (row, id, wanted: string[]) =>
  !wanted?.length || wanted.includes(String(row.getValue(id)));

/**
 * A column's head: a menu that sorts by it and, given how its values are named, ticks the ones
 * to keep.
 */
function head(label: string, name?: (value: string) => string) {
  return ({ column, table: rowsOf }: HeaderContext<any, unknown>) => {
    const wanted = (column.getFilterValue() as string[] | undefined) ?? [];
    const values = name
      ? [...new Set(rowsOf.getPreFilteredRowModel().rows.map((row) => String(row.getValue(column.id))))].sort()
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
        label: name!(value) || "(none)",
        // an origin leads with its colour's dot, as in the rows
        slot: name === store.originName ? ("origin" as const) : undefined,
        value,
        checked: wanted.includes(value),
        onUpdateChecked: (checked: boolean) => tick(value, checked),
        onSelect: (event: Event) => event.preventDefault(),
      })),
    ];
    const sorted = column.getIsSorted();
    const icon =
      sorted === "asc" ? "i-lucide-arrow-up" : sorted === "desc" ? "i-lucide-arrow-down" : "i-lucide-chevrons-up-down";
    return h(
      UDropdownMenu,
      { items, content: { align: "start" } },
      {
        default: () =>
          h(UButton, {
            label: wanted.length ? `${label} (${wanted.length})` : label,
            trailingIcon: icon,
            color: wanted.length ? "primary" : "neutral",
            variant: "ghost",
            size: "xs",
            class: "-mx-2 font-medium",
          }),
        "origin-leading": ({ item }: { item: { value: string } }) => dot(item.value),
      },
    );
  };
}

const rows = computed((): Row[] => {
  const health = props.health;
  switch (props.card) {
    case "contests":
      return health?.contests ?? [];
    case "overrides":
      return health?.overrides ?? [];
    case "depends":
      return health?.depends ?? [];
    case "dangling":
      return health?.dangling ?? [];
    case "files":
      return health?.files ?? [];
    case "loc":
      return (health?.loc ?? []).flatMap((language) =>
        (["missing", "untranslated", "orphaned"] as const).flatMap((gap) =>
          (language[gap] ?? []).map((row) => ({ ...row, language: language.language, gap })),
        ),
      );
  }
});

// a virtualized row is drawn at the height given here: a badge's line and the cell's padding,
// 2.25rem, in pixels at the page's scale
const rowHeight = () => 2.25 * parseFloat(getComputedStyle(document.documentElement).fontSize);

// the rows the filters leave, taken from the table once it has applied a change
const shown = shallowRef<Row[]>([]);
watch(
  [filter, filters, rows, table],
  () => {
    shown.value = table.value?.tableApi?.getFilteredRowModel().rows.map((row) => row.original as Row) ?? rows.value;
    emit("shown", props.card, shown.value);
  },
  { immediate: true, flush: "post" },
);

const contests: TableColumn<Contest>[] = [
  { accessorKey: "kind", header: head("Kind", String), filterFn: among },
  {
    accessorKey: "name",
    header: head("Name"),
    cell: ({ row }) => h("span", { class: "font-mono" }, row.original.name),
  },
  {
    accessorKey: "rule",
    header: head("Rule", String),
    filterFn: among,
    // a contest the install has no part in is a mod overriding another mod's own definition
    cell: ({ row }) =>
      h("div", { class: "flex gap-1" }, [
        badge(row.original.rule, row.original.firstWins ? "warning" : "info"),
        row.original.sites?.some((site) => originOf(site.file) === store.originVanilla)
          ? ""
          : badge("overrides a mod's own", "error"),
      ]),
  },
  {
    id: "sites",
    header: head("Defined by"),
    accessorFn: (row) => row.sites?.length ?? 0,
    cell: ({ row }) => origins((row.original.sites ?? []).map((site) => originOf(site.file))),
  },
  {
    id: "kept",
    header: head("Kept", store.originName),
    accessorFn: (row) => originOf(row.sites?.[row.winner]?.file),
    filterFn: among,
    cell: ({ row }) => origin(originOf(row.original.sites?.[row.original.winner]?.file)),
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
    accessorKey: "to",
    header: head("Needs", store.originName),
    filterFn: among,
    cell: ({ row }) => origin(row.original.to),
  },
  { id: "count", header: head("Uses"), accessorFn: (row) => row.sites?.length ?? 0 },
];
const dangling: TableColumn<Dangling>[] = [
  { accessorKey: "kind", header: head("Kind", String), filterFn: among },
  { accessorKey: "name", header: head("Name") },
  {
    accessorKey: "declared",
    header: "",
    cell: ({ row }) => (row.original.declared ? badge("the game rejects it", "error") : ""),
  },
  { id: "count", header: head("Written"), accessorFn: (row) => row.sites?.length ?? 0 },
];
const loc: TableColumn<Gap>[] = [
  { accessorKey: "language", header: head("Language", String), filterFn: among },
  {
    accessorKey: "gap",
    header: head("Gap", String),
    filterFn: among,
    cell: ({ row }) => badge(row.original.gap, GAP_COLOR[row.original.gap]),
  },
  { accessorKey: "key", header: head("Key") },
  {
    id: "origin",
    header: head("Origin", store.originName),
    accessorFn: (row) => originOf(row.file),
    filterFn: among,
    cell: ({ row }) => origin(originOf(row.original.file)),
  },
];
const files: TableColumn<FileRow>[] = [
  { accessorKey: "kind", header: head("Kind", String), filterFn: among },
  { accessorKey: "name", header: head("Name") },
  {
    id: "missing",
    header: head("Missing"),
    accessorFn: (row) => row.missing?.length ?? 0,
    cell: ({ row }) => `${row.original.missing?.length ?? 0} of ${row.original.of}`,
  },
  {
    id: "origin",
    header: head("Origin", store.originName),
    accessorFn: (row) => originOf(row.file),
    filterFn: among,
    cell: ({ row }) => origin(originOf(row.original.file)),
  },
];
// each card's columns are typed by its own rows; the table holds whichever the card picks
const columns = computed(
  () => ({ contests, overrides: contests, depends, dangling, loc, files })[props.card] as TableColumn<Row>[],
);
</script>

<template>
  <div class="flex h-full min-h-0 flex-col">
    <div class="flex shrink-0 items-center gap-2 border-b border-default px-3 py-2">
      <slot />
      <div class="flex-1" />
      <span class="text-xs text-muted">{{ shown.length }} {{ shown.length === 1 ? "row" : "rows" }}</span>
      <UInput v-model="filter" icon="i-lucide-search" placeholder="Filter…" size="sm" class="w-56" />
    </div>
    <UTable
      ref="table"
      v-model:global-filter="filter"
      v-model:sorting="sorting"
      v-model:column-filters="filters"
      v-model:row-selection="picked"
      :data="rows"
      :columns="columns"
      :loading="loading"
      sticky
      :virtualize="{ estimateSize: rowHeight() }"
      empty="Nothing to list."
      :ui="{
        th: 'px-3 py-2 text-xs font-medium text-muted',
        td: 'px-3 py-1.5 text-sm text-default',
        tr: 'data-[selected=true]:bg-primary/10',
      }"
      class="min-h-0 flex-1"
      @select="(_event, row) => ((picked = { [row.id]: true }), emit('select', row.original))"
    />
  </div>
</template>
