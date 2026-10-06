/**
 * Open a file of a workspace in the IDE, at a line when given one: the IDE page, its tab shown there.
 */
import { toValue, type MaybeRefOrGetter } from "vue";
import { useRouter } from "vue-router";
import { useIdeStore } from "../stores/ide";

export function useOpenInIde() {
  const router = useRouter();
  const ide = useIdeStore();

  /** Open a file at a 1-based line. */
  async function openInIde(workspaceId: MaybeRefOrGetter<string>, path: string, line?: number): Promise<void> {
    const id = toValue(workspaceId);
    if (!id || !path) return;
    await router.push({ name: "workspace-ide", params: { id } });
    ide.open(path, line ? { line: line - 1 } : undefined);
  }

  return { openInIde };
}
