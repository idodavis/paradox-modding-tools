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

  // Register AG Grid Modules
  ModuleRegistry.registerModules([AllCommunityModule]);

  let {
    columnDefs,
    rowData,
    className = "",
  }: {
    columnDefs: Array<any>;
    rowData: Array<any>;
    className?: string;
  } = $props();

  // Create a custom dark theme using Theming API
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
        minWidth: 100,
      },
    };

    if (gridDiv) {
      gridApi = createGrid(gridDiv, gridOptions);
    }
  });

  // Update grid when rowData or columnDefs change (e.g. after Run Compare)
  $effect(() => {
    if (gridApi) {
      gridApi.setGridOption("rowData", rowData);
      gridApi.setGridOption("columnDefs", columnDefs);
    }
  });
</script>

<!-- Grid Container -->
<div bind:this={gridDiv} class={className}></div>
