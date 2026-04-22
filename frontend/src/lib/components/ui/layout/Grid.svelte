<script lang="ts">
  import { onMount } from "svelte";
  import {
    createGrid,
    ModuleRegistry,
    AllCommunityModule,
    type GridOptions,
    type GridApi,
    themeQuartz,
  } from "ag-grid-community";

  ModuleRegistry.registerModules([AllCommunityModule]);

  let {
    columnDefs,
    rowData,
    className = "",
    gridOptions: userGridOptions = {},
  }: {
    columnDefs: Array<any>;
    rowData: Array<any>;
    className?: string;
    gridOptions?: Partial<GridOptions<any>>;
  } = $props();

  const gridContainerClass = $derived(
    "rounded-lg border border-base-content/20 bg-base-100 overflow-hidden " + (className || ""),
  );

  const theme = themeQuartz.withParams({
    accentColor: "var(--color-accent)",
    backgroundColor: "var(--color-base-100)",
    foregroundColor: "var(--color-base-content)",
    headerBackgroundColor: "var(--color-base-300)",
    oddRowBackgroundColor: "var(--color-base-200)",
    borderColor: "var(--color-base-300)",
    headerFontSize: 16,
    wrapperBorderRadius: 4,
  });

  let gridDiv: HTMLDivElement;
  let gridApi: GridApi | null = null;

  onMount(() => {
    const gridOptions: GridOptions<any> = {
      theme: theme,
      columnDefs,
      rowData,
      defaultColDef: {
        flex: 1,
        minWidth: 120,
      },
      ...userGridOptions,
    };

    if (gridDiv) {
      gridApi = createGrid(gridDiv, gridOptions);
    }
  });

  $effect(() => {
    if (gridApi) {
      gridApi.setGridOption("rowData", rowData);
      gridApi.setGridOption("columnDefs", columnDefs);
    }
  });
</script>

<div bind:this={gridDiv} class={gridContainerClass}></div>
