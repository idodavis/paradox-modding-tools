/**
 * The Health page's report: the load order previewed (the workspace's own until a drag), the report
 * for it, each card's rows in it, and saving the order as the workspace's.
 */
import { computed, onActivated, ref, toValue, watch, type MaybeRefOrGetter } from "vue";
import { useMutation, useQuery, useQueryCache } from "@pinia/colada";
import type { Contest, Dangling, Dependency, Health, LocRow, Problem } from "@services/models";
import { ReorderWorkspaceMods } from "@services/workspaceservice";
import { healthQuery, sessionKeys, workspaceKeys } from "../queries";
import { useWorkspaceStore } from "../stores/workspace";

/** A card: three the load order changes, three fixed in the files. */
export type Card = "contests" | "overrides" | "depends" | "dangling" | "loc" | "script";
/** An untranslated key, with the language it is missing in. */
export type Gap = LocRow & { language: string };
export type Row = Contest | Dependency | Dangling | Gap | Problem;

/** The editor's findings Dangling refs and Loc gaps list beside their own rows; Script errors lists the rest. */
const CODES = {
  dangling: ["unresolved-ref", "unknown-event"],
  loc: ["missing-bom", "loc-header-mismatch", "wrong-localization-folder"],
};

/** Each kind of finding's color: syntax red, references orange, scope and values blue, duplicates and
 * localization files teal. */
const CHECK_COLORS: Record<string, "error" | "warning" | "info" | "secondary"> = {
  "unclosed-brace": "error", "stray-close": "error", "unterminated-string": "error", "missing-value": "error",
  "loc-no-header": "error", "loc-bad-entry": "error", "loc-tab-indent": "error", "loc-unterminated-value": "error",
  "loc-content-before-header": "error",
  "unresolved-ref": "warning", "unknown-event": "warning", "missing-file": "warning", "inject-without-target": "warning",
  "wrong-scope": "info", "wrong-value-type": "info", "wrong-declared-value": "info", "missing-key": "info",
  "duplicate-definition": "secondary", "missing-bom": "secondary", "loc-header-mismatch": "secondary",
  "wrong-localization-folder": "secondary",
};
export function checkColor(code: string): string {
  return CHECK_COLORS[code] ?? "neutral";
}

/** A card's rows in a report. */
export function rowsOf(health: Health | null | undefined, card: Card): Row[] {
  const problems = health?.problems ?? [];
  const coded = (codes: string[]) => problems.filter((problem) => codes.includes(problem.code));
  switch (card) {
    case "contests":
      return health?.contests ?? [];
    case "overrides":
      return health?.overrides ?? [];
    case "depends":
      return health?.depends ?? [];
    case "dangling":
      return [...(health?.dangling ?? []), ...coded(CODES.dangling)];
    case "loc":
      return [
        ...(health?.loc ?? []).flatMap((gaps) =>
          (gaps.untranslated ?? []).map((row) => ({ ...row, language: gaps.language })),
        ),
        ...coded(CODES.loc),
      ];
    case "script":
      return problems.filter((problem) => !CODES.dangling.includes(problem.code) && !CODES.loc.includes(problem.code));
  }
}

function sameOrder(first: readonly string[], second: readonly string[]): boolean {
  return first.length === second.length && first.every((id, i) => id === second[i]);
}

export function useHealth(workspaceId: MaybeRefOrGetter<string>, live: MaybeRefOrGetter<boolean>) {
  const store = useWorkspaceStore();
  const queryCache = useQueryCache();

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
  const { data: health, error, isPending, refetch } = useQuery(() =>
    healthQuery({ id: toValue(workspaceId), order: dirty.value ? order.value : [], live: toValue(live) }),
  );
  // the page is kept alive, so a report already shown is asked for again on return: edits made
  // meanwhile were indexed (a first visit's fetch is still running, and a refetch would cancel it)
  onActivated(() => {
    if (health.value) void refetch();
  });

  const { mutate: save, isLoading: saving } = useMutation({
    mutation: () => ReorderWorkspaceMods(toValue(workspaceId), order.value),
    onSettled: () => {
      void queryCache.invalidateQueries({ key: workspaceKeys.all });
      void queryCache.invalidateQueries({ key: sessionKeys.live(toValue(workspaceId)) });
    },
  });

  return { order, dirty, health, error, isPending, save, saving };
}
