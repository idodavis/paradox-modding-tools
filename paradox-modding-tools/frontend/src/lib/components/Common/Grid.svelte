<script lang="ts">
  import { onMount } from "svelte";
  import {
    createGrid,
    ModuleRegistry,
    AllCommunityModule,
    type GridOptions,
    type GridApi,
    themeQuartz,
    colorSchemeDarkBlue,
  } from "ag-grid-community";

  // Register AG Grid Modules
  ModuleRegistry.registerModules([AllCommunityModule]);

  export let columnDefs: Array<any> = [];
  export let rowData: Array<any> = [];

  // Create a custom dark theme using Theming API
  const darkTheme = themeQuartz.withPart(colorSchemeDarkBlue).withParams({
    backgroundColor: "#212121",
    foregroundColor: "#ffffff",
    headerBackgroundColor: "#37474f",
    headerTextColor: "#cfd8dc",
    oddRowBackgroundColor: "#263238",
  });

  let gridDiv: HTMLDivElement;
  let gridApi: GridApi | null = null;

  onMount(() => {
    const gridOptions: GridOptions<any> = {
      theme: darkTheme,
      columnDefs,
      rowData,
      defaultColDef: {
        sortable: true,
        filter: true,
      },
    };

    if (gridDiv) {
      gridApi = createGrid(gridDiv, gridOptions);
    }
  });

  // Update grid when rowData or columnDefs change (e.g. after Run Compare)
  $: if (gridApi) {
    gridApi.setGridOption("rowData", rowData);
    gridApi.setGridOption("columnDefs", columnDefs);
  }
</script>

<!-- Grid Container -->
<div bind:this={gridDiv} style="height: 400px; width: 100%;"></div>
