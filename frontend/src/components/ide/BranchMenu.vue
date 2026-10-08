<script setup lang="ts">
/**
 * The branch control and its menu (the IdeDiff board's): switch to a branch, or type a name to create one; create
 * one from another branch; check out a tag or commit; the local and remote branches; then fetch, pull, pull from,
 * push, merge into the current branch and delete. A "…" action lists what it takes in place of the branches.
 * Remotes… opens the repository's remotes: each one's address, changed or removed, and one added.
 */
import { computed, ref, shallowRef } from "vue";
import { useQuery } from "@pinia/colada";
import { formatTimeAgo } from "@vueuse/core";
import type { CommandPaletteGroup, CommandPaletteItem } from "@nuxt/ui";
import { AddRemote, Checkout, DeleteBranch, Fetch, Merge, Pull, Push, RemoveRemote, SetRemoteURL } from "@services/gitservice";
import type { GitBranch, GitRemote } from "@services/models";
import { gitRemotesQuery } from "../../queries";
import { useGitStore } from "../../stores/git";

const git = useGitStore();
const toast = useToast();
const open = shallowRef(false);
const term = shallowRef("");
// what the list holds: the branches, or what a "…" action picks from
type Mode = "main" | "name" | "from" | "tag" | "pull" | "merge" | "delete";
const mode = shallowRef<Mode>("main");
const base = shallowRef("");

const current = computed(() => git.status?.branch || "HEAD");
const local = computed(() => git.branches?.local ?? []);
const remote = computed(() => git.branches?.remote ?? []);
const remotes = computed(() => [...new Set(remote.value.map((one) => one.name.split("/")[0]))]);

/** Do one thing and close the menu. */
function act(work: (root: string) => Promise<unknown>): void {
  open.value = false;
  git.run(work);
}
function show(next: Mode, from = ""): void {
  mode.value = next;
  base.value = from;
  term.value = "";
}

// a local branch says if it is the current one and how far from its upstream, else how old it is
function localItem(branch: GitBranch): CommandPaletteItem {
  const far = branch.ahead || branch.behind ? `, ${branch.ahead} up, ${branch.behind} down` : "";
  return {
    label: branch.name,
    icon: branch.current ? "i-lucide-check" : "i-lucide-git-branch",
    suffix: branch.current ? `current${far}` : formatTimeAgo(new Date(branch.date)),
    onSelect: () => act((root) => Checkout(root, branch.name, "")),
  };
}
// a remote branch switches to the local one tracking it, or checks out as a new local branch
function remoteItem(branch: GitBranch): CommandPaletteItem {
  const tracking = local.value.find((one) => one.upstream === branch.name);
  const behind = tracking?.behind ? `${tracking.behind} commit${tracking.behind === 1 ? "" : "s"} you don't have` : "";
  return {
    label: branch.name,
    icon: "i-lucide-cloud",
    suffix: tracking ? behind : "checks out as a new local branch",
    onSelect: () => act((root) => (tracking ? Checkout(root, tracking.name, "") : Checkout(root, branch.name, branch.name.slice(branch.name.indexOf("/") + 1)))),
  };
}
const picked = (branch: GitBranch, onSelect: () => void): CommandPaletteItem => ({ label: branch.name, icon: "i-lucide-git-branch", onSelect });

const groups = computed<CommandPaletteGroup[]>(() => {
  const name = term.value.trim();
  const others = [...local.value, ...remote.value].filter((one) => !one.current);
  switch (mode.value) {
    case "name":
    case "main": {
      const from = mode.value === "name" ? base.value : current.value;
      const create = name && !local.value.some((one) => one.name === name)
        ? [{ label: `Create branch ${name} from ${from}`, icon: "i-lucide-plus", onSelect: () => act((root) => Checkout(root, from === current.value ? "" : from, name)) }]
        : [];
      if (mode.value === "name") return [{ id: "create", items: create, ignoreFilter: true }];
      return [
        {
          id: "actions",
          ignoreFilter: true,
          items: [
            ...create,
            ...(name
              ? []
              : [
                  { label: `Create a branch from ${current.value}…`, icon: "i-lucide-plus", onSelect: () => show("name", current.value) },
                  { label: "Create a branch from another branch…", icon: "i-lucide-plus", onSelect: () => show("from") },
                  { label: "Check out a tag or commit…", icon: "i-lucide-git-commit-horizontal", onSelect: () => show("tag") },
                ]),
          ],
        },
        { id: "local", label: "Local branches", items: local.value.map(localItem) },
        { id: "remote", label: remotes.value.length === 1 ? `Remote branches on ${remotes.value[0]}` : "Remote branches", items: remote.value.map(remoteItem) },
      ];
    }
    case "from":
      return [{ id: "from", label: "Make the new branch from", items: [...local.value, ...remote.value].map((one) => picked(one, () => show("name", one.name))) }];
    case "tag":
      return [
        { id: "tags", label: "Tags", items: (git.branches?.tags ?? []).map((tag) => ({ label: tag.name, icon: "i-lucide-tag", onSelect: () => act((root) => Checkout(root, tag.name, "")) })) },
        {
          id: "commits",
          label: "Commits",
          items: (git.log ?? []).map((commit) => ({
            label: commit.subject,
            suffix: commit.short,
            icon: "i-lucide-git-commit-horizontal",
            onSelect: () => act((root) => Checkout(root, commit.hash, "")),
          })),
        },
      ];
    case "pull":
      return [
        {
          id: "pull",
          label: "Pull from",
          items: remote.value.map((one) => picked(one, () => act((root) => Pull(root, one.name.split("/")[0] ?? "", one.name.slice(one.name.indexOf("/") + 1))))),
        },
      ];
    case "merge":
      return [{ id: "merge", label: `Merge into ${current.value}`, items: others.map((one) => picked(one, () => act((root) => Merge(root, one.name)))) }];
    case "delete":
      return [{ id: "delete", label: "Delete a branch", items: local.value.filter((one) => !one.current).map((one) => picked(one, () => remove(one.name))) }];
  }
  return [];
});

const PLACEHOLDERS: Record<Mode, string> = {
  main: "Switch branch, or type a name to create one",
  name: "Name the new branch",
  from: "The branch to make it from",
  tag: "A tag or commit",
  pull: "The remote branch to pull",
  merge: "The branch to merge",
  delete: "The branch to delete",
};

// the remotes, read while their dialog is open: the one whose address is being changed, and the one to add (its
// name origin when left empty)
const remoting = shallowRef(false);
const { data: addresses } = useQuery(() => gitRemotesQuery({ root: git.root, enabled: remoting.value }));
const change = ref({ name: "", url: "" });
const added = ref({ name: "", url: "" });
function showRemotes(): void {
  open.value = false;
  change.value = { name: "", url: "" };
  added.value = { name: "", url: "" };
  remoting.value = true;
}
function editURL(remote: GitRemote): void {
  change.value = { name: remote.name, url: remote.url };
}
function saveURL(): void {
  const { name, url } = change.value;
  if (!url.trim()) return;
  change.value = { name: "", url: "" };
  git.run((root) => SetRemoteURL(root, name, url.trim()));
}
function addRemote(): void {
  const { name, url } = added.value;
  if (!url.trim()) return;
  added.value = { name: "", url: "" };
  git.run((root) => AddRemote(root, name.trim() || "origin", url.trim()));
}

// a branch with commits merged nowhere is deleted only when asked again
function remove(name: string): void {
  open.value = false;
  git.run(async (root) => {
    try {
      await DeleteBranch(root, name, false);
    } catch (err) {
      if (!String(err).includes("not fully merged")) throw err;
      toast.add({
        title: `${name} has commits no other branch has`,
        actions: [{ label: "Delete anyway", color: "error", variant: "soft", onClick: () => git.run((at) => DeleteBranch(at, name, true)) }],
      });
    }
  });
}
</script>

<template>
  <UPopover v-model:open="open" :content="{ align: 'start' }" @update:open="(on) => on && show('main')">
    <UButton
      icon="i-lucide-git-branch"
      trailing-icon="i-lucide-chevron-down"
      color="neutral"
      variant="soft"
      size="sm"
      class="min-w-0 grow"
      :ui="{ label: 'truncate font-mono', trailingIcon: 'ms-auto text-dimmed' }"
      :label="git.status?.branch || `detached at ${git.status?.commit ?? ''}`"
    />
    <template #content>
      <UCommandPalette
        v-model:search-term="term"
        :groups="groups"
        :placeholder="PLACEHOLDERS[mode]"
        class="h-100 w-110"
        :ui="{ itemLabelSuffix: 'text-11 text-dimmed' }"
      >
        <template #footer>
          <div v-if="mode === 'main'" class="flex flex-wrap gap-1.5">
            <UButton label="Fetch" icon="i-lucide-cloud-download" size="sm" @click="act(Fetch)" />
            <UButton label="Pull" icon="i-lucide-arrow-down-to-line" size="sm" @click="act((root) => Pull(root, '', ''))" />
            <UButton label="Pull from…" size="sm" @click="show('pull')" />
            <UButton label="Push" icon="i-lucide-arrow-up-from-line" size="sm" @click="act(Push)" />
            <UButton :label="`Merge into ${current}…`" size="sm" @click="show('merge')" />
            <UButton label="Remotes…" icon="i-lucide-server" size="sm" @click="showRemotes" />
            <UButton label="Delete…" color="error" variant="ghost" size="sm" @click="show('delete')" />
          </div>
          <UButton v-else label="Back" icon="i-lucide-arrow-left" variant="ghost" size="sm" @click="show('main')" />
        </template>
      </UCommandPalette>
    </template>
  </UPopover>
  <UModal
    v-model:open="remoting"
    title="Remotes"
    description="Where this repository fetches from and pushes to."
    :ui="{ content: 'h-90 sm:max-w-xl', body: 'overflow-y-auto' }"
  >
    <template #body>
      <div class="flex flex-col gap-2 text-12">
        <p v-if="!addresses?.length" class="text-dimmed">This repository has no remote yet.</p>
        <form v-for="one in addresses" :key="one.name" class="flex h-7 items-center gap-1.5" @submit.prevent="saveURL">
          <span class="w-16 shrink-0 truncate font-mono text-highlighted">{{ one.name }}</span>
          <template v-if="change.name === one.name">
            <UInput v-model="change.url" size="sm" autofocus class="min-w-0 grow font-mono" />
            <UButton type="submit" label="Save" size="sm" :disabled="!change.url.trim()" />
            <UButton label="Cancel" variant="ghost" size="sm" @click="change.name = ''" />
          </template>
          <template v-else>
            <span class="min-w-0 grow truncate font-mono text-toned">{{ one.url }}</span>
            <UButton icon="i-lucide-pencil" variant="ghost" size="xs" square :aria-label="`Change ${one.name}'s address`" @click="editURL(one)" />
            <UButton
              icon="i-lucide-trash-2"
              color="error"
              variant="ghost"
              size="xs"
              square
              :aria-label="`Remove ${one.name}`"
              @click="git.run((root) => RemoveRemote(root, one.name))"
            />
          </template>
        </form>
        <form class="flex items-center gap-1.5 border-t border-default pt-3" @submit.prevent="addRemote">
          <UInput v-model="added.name" placeholder="origin" size="sm" class="w-16 shrink-0 font-mono" />
          <UInput v-model="added.url" placeholder="https://github.com/you/my-mod.git" size="sm" class="min-w-0 grow font-mono" />
          <UButton type="submit" label="Add" icon="i-lucide-plus" size="sm" :disabled="!added.url.trim()" />
        </form>
      </div>
    </template>
  </UModal>
</template>
