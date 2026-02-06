import type { ColumnDef, RowData } from '@tanstack/table-core';
import type { DataTableColumn } from './types';

/**
 * Convert simplified DataTableColumn[] to ColumnDef[] for table-core.
 */
export function toColumnDefs<TData extends RowData>(
  columns: DataTableColumn<TData>[] | ColumnDef<TData, unknown>[]
): ColumnDef<TData, unknown>[] {
  if (columns.length === 0) return [];
  const first = columns[0];
  // If it has getContext or other ColumnDef-specific props, treat as ColumnDef[]
  if ('cell' in first && typeof (first as ColumnDef<TData, unknown>).cell === 'function') {
    return columns as ColumnDef<TData, unknown>[];
  }
  return (columns as DataTableColumn<TData>[]).map((col) => {
    const def: ColumnDef<TData, unknown> = {
      id: col.id,
      header: col.header,
      enableSorting: col.enableSorting ?? true,
      enableColumnFilter: col.enableColumnFilter ?? true,
      filterFn: col.filterFn as any,
      minSize: col.minSize,
      maxSize: col.maxSize,
      size: col.size,
    };
    if (col.accessorKey) {
      def.accessorKey = col.accessorKey as any;
    } else if (col.accessorFn) {
      def.accessorFn = col.accessorFn as any;
    }
    if (col.getUniqueValues) {
      def.getUniqueValues = col.getUniqueValues as any;
    }
    return def;
  });
}
