/**
 * The Health page's state: the load order being previewed (the workspace's own until a drag), the
 * report for that order, saving it as the workspace's, one loc key looked up and the source lines
 * of the open row's sites, for the site pane.
 */
import { computed, onActivated, ref, shallowRef, toValue, watch, type MaybeRefOrGetter } from "vue";
import { useMutation, useQuery, useQueryCache } from "@pinia/colada";
import { GetHealth, GetLines, LookupLoc } from "@services/healthservice";
import type { SourceAt } from "@services/models";
import { ReorderWorkspaceMods } from "@services/workspaceservice";
import { useWorkspaceStore } from "../stores/workspace";
import { sessionKeys, workspaceKeys } from "../queries";

export function useHealth(workspaceId: MaybeRefOrGetter<string>, live: MaybeRefOrGetter<boolean>) {
  const store = useWorkspaceStore();
  const queryCache = useQueryCache();

  // the workspace's order as Go keeps it, never re-sorted here: it is what the report judges by
  const persisted = computed(() => store.workspaceMods.map((mod) => mod.id));
  const order = ref<string[]>([]);
  watch(
    persisted,
    (ids) => {
      if (ids.length !== order.value.length || ids.some((id) => !order.value.includes(id))) order.value = [...ids];
    },
    { immediate: true },
  );
  const dirty = computed(() => order.value.some((id, i) => id !== persisted.value[i]));

  const { data: health, error, isPending, refetch } = useQuery({
    key: () => sessionKeys.health(toValue(workspaceId), order.value),
    query: () => GetHealth(toValue(workspaceId), { order: order.value }),
    enabled: () => toValue(live),
  });
  // the page is kept alive, so a report already shown is asked for again on return: edits made
  // meanwhile were indexed (a first visit's fetch is still running, and a refetch would cancel it)
  onActivated(() => {
    if (health.value) void refetch();
  });
  const { mutateAsync: save, isLoading: saving } = useMutation({
    mutation: async () => {
      await ReorderWorkspaceMods(toValue(workspaceId), order.value);
      await queryCache.invalidateQueries({ key: workspaceKeys.all });
    },
  });

  const locKey = shallowRef("");
  const { data: lookup } = useQuery({
    key: () => sessionKeys.loc(toValue(workspaceId), locKey.value),
    query: () => LookupLoc(toValue(workspaceId), locKey.value),
    enabled: () => toValue(live) && !!locKey.value,
  });

  const places = shallowRef<SourceAt[]>([]);
  const { data: lines } = useQuery({
    key: () => sessionKeys.lines(toValue(workspaceId), places.value.map((at) => `${at.path}:${at.line}:${at.col}`)),
    query: () => GetLines(toValue(workspaceId), places.value),
    enabled: () => toValue(live) && places.value.length > 0,
  });

  return { order, dirty, health, error, isPending, save, saving, locKey, lookup, places, lines };
}
