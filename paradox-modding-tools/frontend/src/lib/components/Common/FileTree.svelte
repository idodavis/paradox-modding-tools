<script lang="ts">
  import Self from "./FileTree.svelte";
  import Icon from "@iconify/svelte";
  import type { TreeNode } from "@services/models";

  let {
    tree,
    filter = "",
    folderColor = "",
    fileColor = "",
    onFileClick,
    class: fileTreeClass = "",
  }: {
    tree: TreeNode[];
    filter?: string;
    folderColor?: string;
    fileColor?: string;
    onFileClick?: (file: TreeNode) => void;
    class?: string;
  } = $props();

  function filterTree(nodes: TreeNode[], text: string): TreeNode[] {
    if (!text) return nodes;
    const t = text.toLowerCase();
    return nodes.flatMap((node) => {
      const isFile = !node.children?.length;
      const name = (node.name || "").toLowerCase();
      const filteredChildren = filterTree(node.children || [], text);
      if (isFile) {
        return name.includes(t) ? [node] : [];
      }
      if (filteredChildren.length > 0) {
        return [{ ...node, children: filteredChildren }];
      }
      return [];
    });
  }

  const filteredTree = $derived(() => {
    if (!filter) return tree;
    return filterTree(tree, filter);
  });
</script>

<ul class="menu w-full {fileTreeClass} [&_summary]:pr-6 [&_summary]:overflow-visible">
  {#each filteredTree() as node}
    {#if node.children.length > 0}
      <li class="overflow-visible">
        <details class="overflow-visible">
          <summary class="overflow-visible"
            ><Icon
              icon="mdi:folder"
              class="h-4 w-4 {folderColor}"
            />{node.name}</summary
          >
          <ul class="pl-4 overflow-visible">
            <Self
              tree={node.children}
              {filter}
              {folderColor}
              {fileColor}
              {onFileClick}
            />
          </ul>
        </details>
      </li>
    {:else}
      <li>
        <button onclick={() => onFileClick?.(node)}
          ><Icon
            icon="mdi:file"
            class="h-4 w-4 {fileColor}"
          />{node.name}</button
        >
      </li>
    {/if}
  {/each}
</ul>
