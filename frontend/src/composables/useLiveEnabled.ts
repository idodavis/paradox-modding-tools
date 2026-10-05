/**
 * Whether a workspace's session is live: the session query, which builds it once. Session reads wait on
 * it, so none runs against a session still being built.
 */
import { computed, toValue, type MaybeRefOrGetter } from "vue";
import { useQuery } from "@pinia/colada";
import { sessionQuery } from "../queries";

export function useLiveEnabled(workspaceId: MaybeRefOrGetter<string>) {
  const { data } = useQuery(() => sessionQuery(toValue(workspaceId)));
  return computed(() => !!data.value?.ready);
}
