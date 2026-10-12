/**
 * The Health page's report: the load order previewed (the workspace's own until a drag), the report
 * for it, each card's rows in it, and saving the order as the workspace's.
 */
import { computed, onActivated, ref, toValue, watch, type MaybeRefOrGetter } from "vue";
import { useMutation, useQuery } from "@pinia/colada";
import type {
  Contest,
  Dangling,
  Dependency,
  HealthReport,
  LocRow,
  Problem,
  Site,
} from "@services/internal/pages/models";
import { ReorderWorkspaceMods } from "@services/workspaceservice";
import { healthQuery } from "../queries";
import { useWorkspaceStore } from "../stores/workspace";

/** A card: three the load order changes, three fixed in the files. */
export type Card = "contests" | "overrides" | "depends" | "dangling" | "loc" | "script";
/** An untranslated key, with the language it is missing in. */
export type Gap = LocRow & { language: string };
export type Row = Contest | Dependency | Dangling | Gap | Problem;

/** Each card: its name, its dot's color, what its tile counts, and what its table lists. */
export const CARDS: Record<Card, { label: string; dot: string; note?: string; blurb: string }> = {
  contests: {
    label: "Contests",
    dot: "var(--orange)",
    blurb: "Objects defined by more than one mod. Load order decides which one the game keeps.",
  },
  overrides: {
    label: "Overrides",
    dot: "var(--blue)",
    note: "Mods replacing game objects",
    blurb: "Game objects a mod replaces or adds to.",
  },
  depends: {
    label: "Dependencies",
    dot: "var(--fg-3)",
    note: "Mods reading other mods",
    blurb: "Where a mod uses what only another mod defines, or what another mod changes.",
  },
  // the problems in the error colors: red, orange, and a yellow between orange and the accent
  dangling: {
    label: "Dangling Refs",
    dot: "var(--red)",
    note: "Used, never defined",
    blurb: "Names, events and files a mod uses that nothing defines, one row per name.",
  },
  loc: {
    label: "Localization Issues",
    dot: "color-mix(in oklab, var(--orange), var(--accent))",
    blurb: "Keys left untranslated, and localization files the game reads wrong, one row per place to fix.",
  },
  script: {
    label: "Script Issues",
    dot: "var(--orange)",
    note: "Syntax, scopes, values",
    blurb: "What the editor's checks find in the mods' script files, one row per place to fix.",
  },
};

/** Where a row is written: each of its sites, or the one line a finding or a gap names (a gap's source). */
export function sitesOf(row: Row): Site[] {
  return "sites" in row ? (row.sites ?? []) : "site" in row ? [row.site] : [{ file: row.file, line: row.line, col: 0 }];
}

/** Each type a problem row is: its color (red the game cannot read it, orange something missing, blue
 * written wrong, teal a file or a duplicate to tidy), its card, and the editor's checks that find it
 * ("untranslated" is a gap's; a check named nowhere is Script Issues'). */
const TYPES: [label: string, color: "error" | "warning" | "info" | "secondary", card: Card, codes: string[]][] = [
  ["Syntax", "error", "script", ["unclosed-brace", "stray-close", "unterminated-string", "missing-value"]],
  [
    "Syntax",
    "error",
    "loc",
    ["loc-no-header", "loc-bad-entry", "loc-tab-indent", "loc-unterminated-value", "loc-content-before-header"],
  ],
  ["Undefined", "warning", "dangling", ["unresolved-ref"]],
  ["Unknown event", "warning", "dangling", ["unknown-event"]],
  ["Missing file", "warning", "dangling", ["missing-file"]],
  ["No target", "warning", "dangling", ["inject-without-target"]],
  ["Missing key", "warning", "script", ["missing-key"]],
  ["Untranslated", "warning", "loc", ["untranslated"]],
  ["Scope", "info", "script", ["wrong-scope"]],
  ["Value", "info", "script", ["wrong-value-type", "wrong-declared-value"]],
  ["Duplicate", "secondary", "script", ["duplicate-definition"]],
  ["No BOM", "secondary", "loc", ["missing-bom"]],
  ["Wrong header", "secondary", "loc", ["loc-header-mismatch"]],
  ["Wrong folder", "secondary", "loc", ["wrong-localization-folder"]],
];
const typeOfCode = (code: string) => TYPES.find(([, , , codes]) => codes.includes(code));
/** A problem row's type and color: a gap's, else its check's. */
export function typeOf(row: Row): { label: string; color: string } {
  const code = "key" in row ? "untranslated" : "code" in row ? row.code : "";
  const type = typeOfCode(code);
  return type ? { label: type[0], color: type[1] } : { label: code, color: "neutral" };
}

/** A card's rows in a report. */
export function rowsOf(health: HealthReport | null | undefined, card: Card): Row[] {
  const problems = health?.problems ?? [];
  const coded = (of: Card) => problems.filter((problem) => (typeOfCode(problem.code)?.[2] ?? "script") === of);
  switch (card) {
    case "contests":
      return health?.contests ?? [];
    case "overrides":
      return health?.overrides ?? [];
    case "depends":
      return health?.depends ?? [];
    case "dangling":
      return [...(health?.dangling ?? []), ...coded("dangling")];
    case "loc":
      return [
        ...(health?.loc ?? []).flatMap((gaps) =>
          (gaps.untranslated ?? []).map((row) => ({ ...row, language: gaps.language })),
        ),
        ...coded("loc"),
      ];
    case "script":
      return coded("script");
  }
}

function sameOrder(first: readonly string[], second: readonly string[]): boolean {
  return first.length === second.length && first.every((id, i) => id === second[i]);
}

export function useHealth(workspaceId: MaybeRefOrGetter<string>, live: MaybeRefOrGetter<boolean>) {
  const store = useWorkspaceStore();

  // the workspace's order as Go keeps it; a drag survives a refresh that leaves it as it was
  const persisted = computed(() => store.workspaceMods.map((mod) => mod.id));
  const order = ref<string[]>([]);
  watch(
    persisted,
    (ids, previous) => {
      const sameMods = ids.length === order.value.length && ids.every((id) => order.value.includes(id));
      if (!previous || !sameMods || sameOrder(order.value, previous)) order.value = [...ids];
    },
    { immediate: true },
  );
  const dirty = computed(() => !sameOrder(order.value, persisted.value));

  // the workspace's own order is asked for as Library and Settings ask, so the three share one report
  const {
    data: health,
    error,
    isPending,
    refetch,
  } = useQuery(() =>
    healthQuery({ id: toValue(workspaceId), order: dirty.value ? order.value : [], live: toValue(live) }),
  );
  // the page is kept alive, so a report already shown is asked for again on return: edits made
  // meanwhile were indexed (a first visit's fetch is still running, and a refetch would cancel it)
  onActivated(() => {
    if (health.value) void refetch();
  });

  const { mutate: save, isLoading: saving } = useMutation({
    mutation: () => ReorderWorkspaceMods(toValue(workspaceId), order.value),
  });

  return { order, dirty, health, error, isPending, save, saving };
}
