<script lang="ts">
import type { Contest, Dangling, Dependency, Health, LocRow, Problem } from "@services/models";

/** The editor's checks' cards, each the codes it lists; syntax errors are every other code. */
export const PROBLEM_CARDS = {
  scope: { label: "Wrong scope", note: "commands used where they can't run", codes: ["wrong-scope"] },
  values: {
    label: "Values",
    note: "values a key doesn't take; required keys missing",
    codes: ["wrong-value-type", "wrong-declared-value", "missing-key"],
  },
  references: { label: "References", note: "names and events that don't exist", codes: ["unresolved-ref", "unknown-event"] },
  missingFiles: { label: "Missing files", note: "file paths that don't exist", codes: ["missing-file"] },
  locFiles: {
    label: "Localization files",
    note: "wrong header, folder or encoding",
    codes: ["missing-bom", "loc-header-mismatch", "wrong-localization-folder"],
  },
  duplicates: { label: "Duplicates", note: "entries defined twice where the game keeps one", codes: ["duplicate-definition"] },
  parse: { label: "Syntax errors", note: "script or loc that doesn't parse", codes: [] },
} as const satisfies Record<string, { label: string; note: string; codes: readonly string[] }>;
export type ProblemCard = keyof typeof PROBLEM_CARDS;

export type Card = "contests" | "overrides" | "depends" | "dangling" | "loc" | ProblemCard;
/** A loc row, with the language and the gap it is listed under. */
export type Gap = LocRow & { language: string; gap: "untranslated" };
export type Row = Contest | Dependency | Dangling | Gap | Problem;

/** A problem card's rows: the problems of its codes; parse errors, those of no card's code. */
export function problemsOf(health: Health | null | undefined, card: ProblemCard): Problem[] {
  const carded = Object.values(PROBLEM_CARDS).flatMap((group) => group.codes as readonly string[]);
  const codes = PROBLEM_CARDS[card].codes as readonly string[];
  return (health?.problems ?? []).filter((problem) =>
    card === "parse" ? !carded.includes(problem.code) : codes.includes(problem.code),
  );
}
</script>

<script setup lang="ts">
/**
 * The report's table: the card chosen above picks the rows it lists and the columns it shows. Each
 * column's head sorts by it, and where its values are few, keeps only the ones ticked. Every row is
 * listed, rendered as it scrolls into view; the rows the filters leave are told to the page.
 */
import { computed, h, ref, resolveComponent, shallowRef, useTemplateRef, watch } from "vue";
import type { TableColumn } from "@nuxt/ui";
import type { FilterFn, HeaderContext } from "@tanstack/vue-table";
import { useWorkspaceStore } from "../../stores/workspace";

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

// an origin as its color's dot, then by name; several as their dots and how many
const dot = (id: string) =>
  h("span", { class: "size-2 shrink-0 rounded-full", style: { backgroundColor: store.originColor(id) } });
const origin = (id: string) =>
  h("span", { class: "inline-flex items-center gap-1.5" }, [dot(id), store.originName(id)]);
const origins = (ids: string[]) =>
  h("span", { class: "inline-flex items-center gap-1", title: ids.map(store.originName).join(", ") }, [
    ...ids.map(dot),
    h("span", { class: "ml-1 text-xs tabular-nums text-muted" }, String(ids.length)),
  ]);
const badge = (label: string, color: string) => h(UBadge, { label, color, variant: "subtle" });
// each loc gap's color, matching its card
const GAP_COLOR = { untranslated: "tertiary" };

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
        // an origin leads with its color's dot, as in the rows
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
    case "loc":
      return (health?.loc ?? []).flatMap((language) =>
        (language.untranslated ?? []).map((row) => ({ ...row, language: language.language, gap: "untranslated" as const })),
      );
    default:
      return problemsOf(health, props.card);
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
  { accessorKey: "database", header: head("Database", String), filterFn: among },
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
    id: "relation",
    header: head("Relation", String),
    accessorFn: (row) => (row.affected ? "affected by" : "depends on"),
    filterFn: among,
    cell: ({ row }) => badge(row.original.affected ? "affected by" : "depends on", row.original.affected ? "warning" : "info"),
  },
  {
    accessorKey: "to",
    header: head("Other mod", store.originName),
    filterFn: among,
    cell: ({ row }) => origin(row.original.to),
  },
  { id: "count", header: head("Uses"), accessorFn: (row) => row.sites?.length ?? 0 },
];
const dangling: TableColumn<Dangling>[] = [
  { accessorKey: "database", header: head("Database", String), filterFn: among },
  { accessorKey: "name", header: head("Name") },
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
const problems: TableColumn<Problem>[] = [
  { accessorKey: "code", header: head("Check", String), filterFn: among, cell: ({ row }) => badge(row.original.code, "neutral") },
  { accessorKey: "message", header: head("Finding") },
  {
    id: "origin",
    header: head("Origin", store.originName),
    accessorFn: (row) => originOf(row.site.file),
    filterFn: among,
    cell: ({ row }) => origin(originOf(row.original.site.file)),
  },
];
// each card's columns are typed by its own rows; the table holds whichever the card picks
const columns = computed(
  () =>
    (({ contests, overrides: contests, depends, dangling, loc }) as Record<string, TableColumn<any>[]>)[props.card] ??
    (problems as TableColumn<Row>[]),
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
