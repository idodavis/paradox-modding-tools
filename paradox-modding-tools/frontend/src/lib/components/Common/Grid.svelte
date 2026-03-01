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
    /** Optional AG-Grid options merged with defaults (e.g. pagination, filter, rowSelection, onRowClicked). */
    gridOptions?: Partial<GridOptions<any>>;
  } = $props();

  const gridContainerClass = $derived(
    "rounded-lg border border-base-content/20 bg-base-100 overflow-hidden " + (className || ""),
  );

  const darkTheme = themeQuartz.withParams({
    accentColor: "#B387FA",
    backgroundColor: "#1A1E28",
    borderColor: "#14171F",
    browserColorScheme: "dark",
    oddRowBackgroundColor: "hsl(220, 29%, 6%)",
    foregroundColor: "#9FB9D0",
    headerBackgroundColor: "#0D1016",
    headerFontSize: 16,
    wrapperBorderRadius: 4,
  });

  let gridDiv: HTMLDivElement;
  let gridApi: GridApi | null = null;

  onMount(() => {
    const gridOptions: GridOptions<any> = {
      theme: darkTheme,
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
