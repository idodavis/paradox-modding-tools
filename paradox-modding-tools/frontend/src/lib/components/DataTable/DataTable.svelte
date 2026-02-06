<script lang="ts" generics="TData extends RowData">
	import {
		createTable,
		getCoreRowModel,
		getSortedRowModel,
		getFilteredRowModel,
		getPaginationRowModel,
		getFacetedUniqueValues,
		getFacetedRowModel,
		type ColumnDef,
		type RowData,
		type Table,
		type Cell,
		type Row,
	} from '@tanstack/table-core';
	import { createVirtualizer } from '@tanstack/svelte-virtual';
	import { get } from 'svelte/store';
	import type { DataTableProps } from './types';
	import { toColumnDefs } from './columnUtils';

	type Props = DataTableProps<TData> & {
		/** Optional custom cell renderer. Receives cell context; render primitives or custom HTML/Svelte. */
		cell?: import('svelte').Snippet<[{ cell: Cell<TData, unknown> }]>;
	};

	let {
		data = [],
		columns = [],
		pageSize = 10,
		pageSizeOptions = [10, 25, 50, 100],
		enableSorting = true,
		enableGlobalFilter = true,
		enableColumnFilter = true,
		enablePagination = true,
		enableVirtualization = false,
		rowHeightEstimate = 40,
		globalFilterPlaceholder = 'Search...',
		class: className = '',
		getRowId,
		cell: cellSnippet,
	}: Props = $props();

	const columnDefs = $derived(toColumnDefs(columns));

	const tableState = $state({
		sorting: [] as { id: string; desc: boolean }[],
		columnFilters: [] as { id: string; value: unknown }[],
		globalFilter: '' as string,
		pagination: { pageIndex: 0, pageSize: 10 },
	});
	let didInitPageSize = $state(false);
	$effect(() => {
		if (!didInitPageSize) {
			didInitPageSize = true;
			tableState.pagination.pageSize = pageSize;
		}
	});

	let table = $state<Table<TData> | null>(null);
	let scrollContainer = $state<HTMLDivElement | null>(null);

	$effect(() => {
		const state = tableState;
		const opts = {
			data,
			columns: columnDefs,
			state: {
				sorting: state.sorting,
				columnFilters: state.columnFilters,
				globalFilter: state.globalFilter,
				pagination: state.pagination,
			},
			onStateChange: (updater: (prev: typeof state) => typeof state) => {
				const next = typeof updater === 'function' ? updater(tableState) : updater;
				tableState.sorting = next.sorting ?? tableState.sorting;
				tableState.columnFilters = next.columnFilters ?? tableState.columnFilters;
				tableState.globalFilter = (next.globalFilter as string) ?? tableState.globalFilter;
				tableState.pagination = next.pagination ?? tableState.pagination;
			},
			renderFallbackValue: null,
			getCoreRowModel: getCoreRowModel(),
			getSortedRowModel: getSortedRowModel(),
			getFilteredRowModel: getFilteredRowModel(),
			getPaginationRowModel: enablePagination && !enableVirtualization ? getPaginationRowModel() : undefined,
			initialState: {
				pagination: { pageIndex: 0, pageSize: tableState.pagination.pageSize },
			},
			getRowId: getRowId as any,
			enableSorting,
			enableColumnFilter,
			enableGlobalFilter,
		};
		if (!table) {
			table = createTable(opts as any);
		} else {
			table.setOptions(opts as any);
		}
	});

	const rowModel = $derived(table?.getRowModel());
	const rows = $derived(rowModel?.rows ?? []);
	const headerGroups = $derived(table?.getHeaderGroups() ?? []);
	const pageCount = $derived(table?.getPageCount() ?? 0);
	const canPrev = $derived(table?.getCanPreviousPage() ?? false);
	const canNext = $derived(table?.getCanNextPage() ?? false);
	const pageIndex = $derived(table?.getState().pagination.pageIndex ?? 0);

	let virtualizerStore = $state<ReturnType<typeof createVirtualizer<HTMLDivElement, HTMLTableRowElement>> | null>(null);
	$effect(() => {
		if (!enableVirtualization || !scrollContainer) {
			virtualizerStore = null;
			return;
		}
		virtualizerStore = createVirtualizer({
			count: rows.length,
			getScrollElement: () => scrollContainer,
			estimateSize: () => rowHeightEstimate,
			overscan: 5,
		});
	});
	$effect(() => {
		const v = virtualizerStore ? get(virtualizerStore) : null;
		if (v) v.setOptions({ count: rows.length });
	});

	function setPageSize(size: number) {
		table?.setState((s) => ({ ...s, pagination: { ...s.pagination, pageSize: size, pageIndex: 0 } }));
	}

	function setGlobalFilter(value: string) {
		table?.setState((s) => ({ ...s, globalFilter: value }));
	}
</script>

<div class="data-table flex flex-col min-h-0 {className}">
	<!-- Toolbar: global filter + page size -->
	<div class="flex flex-wrap items-center gap-2 p-2 border-b border-base-content/10">
		{#if enableGlobalFilter}
			<input
				type="text"
				class="input input-bordered input-sm w-full max-w-xs"
				placeholder={globalFilterPlaceholder}
				value={tableState.globalFilter}
				oninput={(e) => setGlobalFilter(e.currentTarget.value)}
			/>
		{/if}
		{#if enablePagination && !enableVirtualization}
			<div class="flex items-center gap-2 ml-auto">
				<span class="text-sm text-base-content/70">Rows per page</span>
				<select
					class="select select-bordered select-sm w-20"
					value={tableState.pagination.pageSize}
					onchange={(e) => setPageSize(Number((e.currentTarget as HTMLSelectElement).value))}
				>
					{#each pageSizeOptions as size}
						<option value={size}>{size}</option>
					{/each}
				</select>
			</div>
		{/if}
	</div>

	<!-- Table scroll wrapper for virtualization -->
	<div
		bind:this={scrollContainer}
		class="overflow-auto flex-1 min-h-0 border border-base-content/10 rounded-lg"
		role="region"
		aria-label="Data table"
	>
		<table class="table table-zebra table-pin-rows">
			<thead>
				{#each headerGroups as headerGroup}
					<tr>
						{#each headerGroup.headers as header}
							<th
								class="bg-base-200 text-left whitespace-nowrap"
								style={header.getSize() ? `width: ${header.getSize()}px` : ''}
							>
								<div class="flex flex-col gap-1">
									<div
										class="flex items-center gap-1 cursor-pointer select-none"
										onclick={header.column.getCanSort() ? () => header.column.toggleSorting(header.column.getIsSorted() === 'asc') : undefined}
										role={header.column.getCanSort() ? 'button' : undefined}
									>
										{typeof header.column.columnDef.header === 'function'
											? header.column.columnDef.header(header.getContext())
											: header.column.columnDef.header}
										{#if header.column.getCanSort()}
											<span class="text-base-content/60">
												{#if header.column.getIsSorted() === 'asc'}↑{:else if header.column.getIsSorted() === 'desc'}↓{:else}↕{/if}
											</span>
										{/if}
									</div>
									{#if enableColumnFilter && header.column.getCanFilter()}
										<input
											type="text"
											class="input input-bordered input-xs w-full max-w-40"
											placeholder="Filter..."
											value={(header.column.getFilterValue() as string) ?? ''}
											oninput={(e) => header.column.setFilterValue(e.currentTarget.value)}
										/>
									{/if}
								</div>
							</th>
						{/each}
					</tr>
				{/each}
			</thead>
			{#if enableVirtualization && virtualizerStore}
				<tbody
					class="block relative"
					style="height: {get(virtualizerStore)?.getTotalSize() ?? 0}px"
				>
					{#each get(virtualizerStore)?.getVirtualItems() ?? [] as virtualRow (virtualRow.key)}
						{@const row = rows[virtualRow.index]}
						{#if row}
							<tr
								class="block! absolute left-0 right-0 border-b border-base-content/5 bg-base-100"
								style="height: {virtualRow.size}px; top: {virtualRow.start}px; display: grid; grid-template-columns: repeat({headerGroups[0]?.headers.length ?? 1}, minmax(0, 1fr));"
							>
								{#each row.getVisibleCells() as cell (cell.id)}
									<td class="px-4 py-2 flex items-center">
										{#if cellSnippet}
											{@render cellSnippet({ cell })}
										{:else}
											{typeof cell.getValue() === 'object' && cell.getValue() !== null
												? ''
												: (cell.getValue() ?? '')}
										{/if}
									</td>
								{/each}
							</tr>
						{/if}
					{/each}
				</tbody>
			{:else}
				<tbody>
					{#each rows as row (row.id)}
						<tr class="hover">
							{#each row.getVisibleCells() as cell (cell.id)}
								<td class="px-4 py-2">
									{#if cellSnippet}
										{@render cellSnippet({ cell })}
									{:else}
										{typeof cell.getValue() === 'object' && cell.getValue() !== null
											? (() => {
													const v = cell.getValue();
													if (v === null || v === undefined) return '';
													if (typeof v === 'string' || typeof v === 'number' || typeof v === 'boolean') return v;
													return String(v);
												})()
											: (cell.getValue() ?? '')}
									{/if}
								</td>
							{/each}
						</tr>
					{/each}
				</tbody>
			{/if}
		</table>
	</div>

	<!-- Pagination -->
	{#if enablePagination && !enableVirtualization && pageCount > 0}
		<div class="flex items-center justify-between gap-4 p-2 border-t border-base-content/10">
			<span class="text-sm text-base-content/70">
				Page {pageIndex + 1} of {pageCount}
				({rowModel?.rows?.length ?? 0} rows on this page)
			</span>
			<div class="join">
				<button
					class="join-item btn btn-sm"
					disabled={!canPrev}
					onclick={() => table?.previousPage()}
				>
					Previous
				</button>
				<button
					class="join-item btn btn-sm"
					disabled={!canNext}
					onclick={() => table?.nextPage()}
				>
					Next
				</button>
			</div>
		</div>
	{/if}
</div>
