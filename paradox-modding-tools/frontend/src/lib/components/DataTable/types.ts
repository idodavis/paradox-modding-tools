import type { ColumnDef, RowData } from '@tanstack/table-core';

/**
 * Simplified column config for DataTable. Converted to ColumnDef internally.
 * Use this for a simple API, or pass full ColumnDef[] for full control.
 */
export interface DataTableColumn<TData extends RowData> {
  id: string;
  header: string;
  accessorKey?: keyof TData & string;
  accessorFn?: (row: TData, index: number) => unknown;
  enableSorting?: boolean;
  enableColumnFilter?: boolean;
  filterFn?: 'includesString' | 'equals' | 'arrIncludes' | 'inNumberRange' | string;
  /** For set-filter style: get unique values for this column (optional; table can derive from data) */
  getUniqueValues?: (row: TData, index: number) => unknown[];
  /** Min width in px (optional) */
  minSize?: number;
  /** Max width in px (optional) */
  maxSize?: number;
  size?: number;
}

export interface DataTableProps<TData extends RowData> {
  /** Table data */
  data: TData[];
  /** Column definitions: either simplified DataTableColumn[] or full ColumnDef<TData>[] */
  columns: DataTableColumn<TData>[] | ColumnDef<TData, unknown>[];
  /** Page size when pagination is enabled */
  pageSize?: number;
  /** Page size options for the selector */
  pageSizeOptions?: number[];
  /** Enable column sorting */
  enableSorting?: boolean;
  /** Enable global search filter */
  enableGlobalFilter?: boolean;
  /** Enable per-column filter inputs */
  enableColumnFilter?: boolean;
  /** Enable pagination */
  enablePagination?: boolean;
  /** Enable virtualization (when true, pagination is typically disabled for large data) */
  enableVirtualization?: boolean;
  /** Estimated row height in px for virtualization */
  rowHeightEstimate?: number;
  /** Global filter placeholder */
  globalFilterPlaceholder?: string;
  /** Optional CSS class for the table wrapper */
  class?: string;
  /** Optional row id getter for stable keys */
  getRowId?: (row: TData, index: number) => string;
}
