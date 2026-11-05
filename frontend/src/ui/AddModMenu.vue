<!-- A menu around its button that adds an existing mod to the open workspace, as the user's own or as external. -->
<script setup lang="ts">
import { shallowRef } from "vue";
import { useMutation } from "@pinia/colada";
import { Dialogs } from "@wailsio/runtime";
import type { DropdownMenuItem } from "@nuxt/ui";

import { useWorkspaceStore } from "../stores/workspace";
import WorkshopPicker from "./WorkshopPicker.vue";
import * as workspace from "@services/workspace/service";
import type { Mod } from "@services/workspace/models";

const ROW = "h-auto py-1.5";
// A closing menu must not take the focus back from the dialog it opened.
const CONTENT = { align: "end" as const, onCloseAutoFocus: (event: Event) => event.preventDefault() };

const store = useWorkspaceStore();
const picking = shallowRef(false);

const { mutate: addFolder } = useMutation({
  mutation: async (external: boolean) => {
    const dir = await Dialogs.OpenFile({ CanChooseFiles: false, CanChooseDirectories: true });
    if (dir) await workspace.AddMod(store.activeId(), dir, external);
  },
});
const { mutate: addWorkshop } = useMutation({
  mutation: async (mods: Mod[]) => {
    for (const mod of mods) await workspace.AddMod(store.activeId(), mod.dir, true);
  },
});

const items: DropdownMenuItem[][] = [
  [
    {
      label: "A mod of yours…",
      description: "You edit and publish it",
      icon: "i-lucide-folder",
      class: ROW,
      onSelect: () => addFolder(false),
    },
    {
      label: "An external mod…",
      description: "Someone else's, to check yours against",
      icon: "i-lucide-package",
      class: ROW,
      onSelect: () => addFolder(true),
    },
  ],
  [
    {
      label: "From the Workshop…",
      description: "Your downloaded Workshop mods",
      icon: "i-lucide-download",
      class: ROW,
      onSelect: () => (picking.value = true),
    },
  ],
];
</script>

<template>
  <!-- not modal: inside the Settings dialog, a modal menu never shows its items -->
  <UDropdownMenu :items="items" :content="CONTENT" :modal="false" :ui="{ content: 'w-72' }">
    <slot />
  </UDropdownMenu>
  <WorkshopPicker
    v-model:open="picking"
    :install-id="store.active()?.record.install ?? ''"
    :taken="store.listMods().map((mod) => mod.dir)"
    @add="addWorkshop"
  />
</template>
