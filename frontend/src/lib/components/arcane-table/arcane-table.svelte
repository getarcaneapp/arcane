<script lang="ts" generics="TData extends {id: string}">
	import {
		type ColumnDef,
		type ColumnFiltersState,
		type Row,
		type RowSelectionState,
		type SortingState,
		type VisibilityState,
		type Table as TableType,
		getCoreRowModel
	} from '@tanstack/table-core';
	import { createSvelteTable } from '$lib/components/ui/data-table/data-table.svelte.js';
	import DataTableToolbar from './arcane-table-toolbar.svelte';
	import { renderComponent, renderSnippet } from '$lib/components/ui/data-table/render-helpers.js';
	import { onMount, untrack } from 'svelte';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
	import type { Snippet } from 'svelte';
	import type { ColumnSpec } from './arcane-table.types.svelte';
	import TableCheckbox from './arcane-table-checkbox.svelte';
	import { m } from '$lib/paraglide/messages';
	import { PersistedState } from 'runed';
	import {
		type CompactTablePrefs,
		type FieldSpec,
		type GroupedData,
		type GroupSelectionState,
		encodeHidden,
		applyHiddenPatch,
		encodeFilters,
		encodeSort,
		encodeMobileVisibility,
		buildMobileVisibility,
		type BulkAction
	} from './arcane-table.types.svelte';
	import type { Component } from 'svelte';
	import { extractPersistedPreferences, filterMapsEqual, toFilterMap } from './arcane-table.utils';
	import ArcaneTablePagination from './arcane-table-pagination.svelte';
	import ArcaneTableHeader from './arcane-table-header.svelte';
	import ArcaneTableCell from './arcane-table-cell.svelte';
	import ArcaneTableDesktopView from './arcane-table-desktop-view.svelte';
	import ArcaneTableMobileView from './arcane-table-mobile-view.svelte';

	let {
		items,
		requestOptions = $bindable(),
		withoutSearch = $bindable(),
		withoutPagination = false,
		selectionDisabled = false,
		unstyled = false,
		onRefresh,
		columns,
		rowActions,
		mobileCard,
		mobileFields = [],
		mobileFieldVisibility = $bindable<Record<string, boolean>>({}),
		selectedIds = $bindable<string[]>([]),
		bulkActions = [],
		persistKey,
		customViewOptions,
		customToolbarActions,
		customTableView,
		customSettings = $bindable<Record<string, unknown>>({}),
		columnVisibility = $bindable<VisibilityState>({}),
		// Grouping props
		groupBy,
		groupIcon,
		groupCollapsedState = $bindable<Record<string, boolean>>({}),
		onGroupToggle,
		imageNameFilterOptions
	}: {
		items: Paginated<TData>;
		requestOptions: SearchPaginationSortRequest;
		withoutSearch?: boolean;
		withoutPagination?: boolean;
		selectionDisabled?: boolean;
		unstyled?: boolean;
		onRefresh: (requestOptions: SearchPaginationSortRequest) => Promise<Paginated<TData>>;
		columns: ColumnSpec<TData>[];
		rowActions?: Snippet<[{ row: Row<TData>; item: TData }]>;
		mobileCard: Snippet<[{ row: Row<TData>; item: TData; mobileFieldVisibility: Record<string, boolean> }]>;
		mobileFields?: FieldSpec[];
		mobileFieldVisibility?: Record<string, boolean>;
		selectedIds?: string[];
		bulkActions?: BulkAction[];
		persistKey?: string;
		customViewOptions?: Snippet;
		customToolbarActions?: Snippet;
		customTableView?: Snippet<
			[
				{
					table: TableType<TData>;
					renderPagination: Snippet;
					mobileFieldsForOptions: { id: string; label: string; visible: boolean }[];
					onToggleMobileField: (fieldId: string) => void;
				}
			]
		>;
		customSettings?: Record<string, unknown>;
		columnVisibility?: VisibilityState;
		// Grouping props
		groupBy?: (item: TData) => string;
		groupIcon?: (groupName: string) => Component;
		groupCollapsedState?: Record<string, boolean>;
		onGroupToggle?: (groupName: string) => void;
		imageNameFilterOptions?: string[];
	} = $props();

	// Default page size constant
	const DEFAULT_LIMIT = 20;

	let rowSelection = $state<RowSelectionState>({});
	let columnFilters = $state<ColumnFiltersState>([]);
	let sorting = $state<SortingState>([]);
	let globalFilter = $state<string>('');

	const enablePersist = $derived(!!persistKey);
	const getEffectiveLimit = () => requestOptions?.pagination?.limit ?? items?.pagination?.itemsPerPage ?? DEFAULT_LIMIT;
	let prefs = $state<PersistedState<CompactTablePrefs> | null>(null);

	const passAllGlobal: (row: unknown, columnId: string, filterValue: unknown) => boolean = () => true;

	const currentPage = $derived(items.pagination?.currentPage ?? requestOptions?.pagination?.page ?? 1);
	const totalPages = $derived(items.pagination?.totalPages ?? 1);
	const totalItems = $derived(items.pagination?.totalItems ?? 0);
	const pageSize = $derived(requestOptions?.pagination?.limit ?? items?.pagination?.itemsPerPage ?? DEFAULT_LIMIT);
	const canPrev = $derived(currentPage > 1);
	const canNext = $derived(currentPage < totalPages);

	onMount(() => {
		// Initialize prefs first
		if (persistKey && !prefs) {
			prefs = new PersistedState<CompactTablePrefs>(
				persistKey,
				{ v: [], f: [], g: '', l: getEffectiveLimit() },
				{ syncTabs: false }
			);
		}

		// Then restore preferences
		if (!enablePersist) return;
		const snapshot = extractPersistedPreferences(prefs?.current, getEffectiveLimit());

		const patchedVisibility = { ...columnVisibility };
		applyHiddenPatch(patchedVisibility, snapshot.hiddenColumns);
		columnVisibility = patchedVisibility;

		let shouldRefresh = false;
		const { restoredFilters, filtersMap } = snapshot;
		if (restoredFilters.length) {
			columnFilters = restoredFilters;
		}
		if (Object.keys(filtersMap).length > 0) {
			if (!filterMapsEqual(filtersMap, requestOptions?.filters)) {
				requestOptions = {
					...requestOptions,
					filters: filtersMap,
					pagination: { page: 1, limit: requestOptions?.pagination?.limit ?? getEffectiveLimit() }
				};
				shouldRefresh = true;
			}
		} else if (requestOptions?.filters && Object.keys(requestOptions.filters).length > 0) {
			requestOptions = {
				...requestOptions,
				filters: undefined,
				pagination: { page: 1, limit: requestOptions?.pagination?.limit ?? getEffectiveLimit() }
			};
			shouldRefresh = true;
		}

		const persistedSearch = snapshot.search;
		const currentSearch = (requestOptions?.search ?? '').trim();
		if (persistedSearch !== globalFilter) {
			globalFilter = persistedSearch;
		}
		if (persistedSearch) {
			if (persistedSearch !== currentSearch) {
				requestOptions = {
					...requestOptions,
					search: persistedSearch,
					pagination: { page: 1, limit: requestOptions?.pagination?.limit ?? getEffectiveLimit() }
				};
				shouldRefresh = true;
			}
		} else if (currentSearch) {
			requestOptions = {
				...requestOptions,
				search: undefined,
				pagination: { page: 1, limit: requestOptions?.pagination?.limit ?? getEffectiveLimit() }
			};
			shouldRefresh = true;
		}

		const persistedLimit = snapshot.limit ?? getEffectiveLimit();
		const currentLimit = requestOptions?.pagination?.limit ?? getEffectiveLimit();
		if (persistedLimit !== currentLimit) {
			requestOptions = { ...requestOptions, pagination: { page: 1, limit: persistedLimit } };
			shouldRefresh = true;
		}

		const persistedSort = snapshot.sort;
		const currentSort = requestOptions?.sort;
		if (persistedSort) {
			if (currentSort?.column !== persistedSort.column || currentSort?.direction !== persistedSort.direction) {
				requestOptions = {
					...requestOptions,
					sort: persistedSort,
					pagination: { page: 1, limit: requestOptions?.pagination?.limit ?? getEffectiveLimit() }
				};
				shouldRefresh = true;
			}
		}
		if (shouldRefresh) onRefresh(requestOptions);

		if (mobileFields.length && !Object.keys(mobileFieldVisibility).length) {
			mobileFieldVisibility = buildMobileVisibility(mobileFields, snapshot.mobileVisibility);
		}

		if (snapshot.customSettings && Object.keys(snapshot.customSettings).length > 0) {
			customSettings = { ...snapshot.customSettings };
		}
	});

	function updatePagination(patch: Partial<{ page: number; limit: number }>) {
		const prev = requestOptions?.pagination ?? {
			page: items?.pagination?.currentPage ?? 1,
			limit: items?.pagination?.itemsPerPage ?? 10
		};
		const next = { ...prev, ...patch };
		requestOptions = { ...requestOptions, pagination: next };
		onRefresh(requestOptions);
	}

	function setPage(page: number) {
		if (page < 1) page = 1;
		if (totalPages > 0 && page > totalPages) page = totalPages;
		updatePagination({ page });
	}

	function setPageSize(limit: number) {
		// Persist page size
		if (enablePersist && prefs) prefs.current = { ...prefs.current, l: limit };
		updatePagination({ limit, page: 1 });
	}

	function onToggleAll(checked: boolean, table: TableType<TData>) {
		const pageIds = table.getRowModel().rows.map((r) => (r.original as TData).id);
		if (checked) {
			const set = new Set([...(selectedIds ?? []), ...pageIds]);
			selectedIds = Array.from(set);
		} else {
			const pageSet = new Set(pageIds);
			selectedIds = (selectedIds ?? []).filter((id) => !pageSet.has(id));
		}
	}

	function onToggleRow(checked: boolean, id: string) {
		if (checked) {
			if (!selectedIds?.includes(id)) selectedIds = [...(selectedIds ?? []), id];
		} else {
			selectedIds = (selectedIds ?? []).filter((x) => x !== id);
		}
	}

	function buildColumns(specs: ColumnSpec<TData>[], isSelectionDisabled: boolean): ColumnDef<TData>[] {
		const cols: ColumnDef<TData>[] = [];

		if (!isSelectionDisabled) {
			cols.push({
				id: 'select',
				header: ({ table }) => {
					const pageIds = table.getRowModel().rows.map((r) => (r.original as TData).id);
					const selectedSet = new Set(selectedIds ?? []);
					const total = pageIds.length;
					const selectedOnPage = pageIds.filter((id) => selectedSet.has(id)).length;
					const checked = total > 0 && selectedOnPage === total;
					const indeterminate = selectedOnPage > 0 && selectedOnPage < total;

					return renderComponent(TableCheckbox, {
						checked,
						indeterminate,
						onCheckedChange: (value) => onToggleAll(!!value, table),
						'aria-label': m.common_select_all()
					});
				},
				cell: ({ row }) => {
					const id = (row.original as TData).id;
					return renderComponent(TableCheckbox, {
						checked: (selectedIds ?? []).includes(id),
						onCheckedChange: (value) => onToggleRow(!!value, id),
						'aria-label': m.common_select_row()
					});
				},
				enableSorting: false,
				enableHiding: false
			});
		}

		specs.forEach((spec, i) => {
			const accessorKey = spec.accessorKey;
			const accessorFn = spec.accessorFn;
			const id = spec.id ?? (accessorKey as string) ?? `col_${i}`;

			cols.push({
				id,
				...(accessorKey ? { accessorKey } : {}),
				...(accessorFn ? { accessorFn } : {}),
				meta: {
					title: spec.title,
					width: spec.width,
					align: spec.align,
					truncate: spec.truncate
				},
				header: ({ column }) => {
					if (spec.header) return renderSnippet(spec.header, { column, title: spec.title, class: spec.class });
					return renderComponent(ArcaneTableHeader, {
						column: spec.sortable ? column : undefined,
						title: spec.title,
						class: spec.class
					});
				},
				cell: ({ row, getValue }) => {
					const item = row.original as TData;
					const value = accessorKey ? row.getValue(accessorKey) : getValue?.();
					if (spec.cell) return renderSnippet(spec.cell, { row, item, value });
					return renderComponent(ArcaneTableCell, { value });
				},
				enableSorting: !!spec.sortable,
				enableHiding: true
			});
		});

		if (rowActions) {
			cols.push({
				id: 'actions',
				cell: ({ row }) => renderSnippet(rowActions, { row, item: row.original as TData })
			});
		}

		return cols;
	}

	// Compute initial hidden columns from column specs (without mutating state in derived)
	function getInitialHiddenColumns(specs: ColumnSpec<TData>[]): Record<string, boolean> {
		const hidden: Record<string, boolean> = {};
		specs.forEach((spec, i) => {
			if (spec.hidden) {
				const accessorKey = spec.accessorKey;
				const id = spec.id ?? (accessorKey as string) ?? `col_${i}`;
				hidden[String(accessorKey ?? id)] = false;
			}
		});
		return hidden;
	}

	// Apply initial hidden columns once on mount
	let initialHiddenApplied = false;
	$effect(() => {
		if (!initialHiddenApplied && columns.length > 0) {
			const hiddenCols = getInitialHiddenColumns(columns);
			if (Object.keys(hiddenCols).length > 0) {
				columnVisibility = { ...columnVisibility, ...hiddenCols };
			}
			initialHiddenApplied = true;
		}
	});

	// Memoize column definitions - only rebuild when structure changes
	// Generate a key based on column structure, not data
	function getColumnsKey(specs: ColumnSpec<TData>[], hasRowActions: boolean, isSelectionDisabled: boolean): string {
		const colIds = specs.map((s, i) => s.id ?? s.accessorKey ?? `col_${i}`).join(',');
		return `${colIds}:${hasRowActions}:${isSelectionDisabled}`;
	}

	let cachedColumnsDef = $state<ColumnDef<TData>[]>([]);
	let lastColumnsKey = '';

	// Use $effect to rebuild columns only when structure changes
	$effect(() => {
		const key = getColumnsKey(columns, !!rowActions, selectionDisabled);
		if (key !== lastColumnsKey) {
			cachedColumnsDef = buildColumns(columns, selectionDisabled);
			lastColumnsKey = key;
		}
	});

	const columnsDef = $derived(cachedColumnsDef.length > 0 ? cachedColumnsDef : buildColumns(columns, selectionDisabled));

	const table = createSvelteTable({
		get data() {
			return items.data ?? [];
		},
		state: {
			get sorting() {
				return sorting;
			},
			get columnVisibility() {
				return columnVisibility;
			},
			get rowSelection() {
				return rowSelection;
			},
			get columnFilters() {
				return columnFilters;
			},
			get globalFilter() {
				return globalFilter;
			}
		},
		get columns() {
			return columnsDef;
		},
		globalFilterFn: passAllGlobal,
		get enableRowSelection() {
			return !selectionDisabled;
		},
		onRowSelectionChange: (updater) => {
			rowSelection = typeof updater === 'function' ? updater(rowSelection) : updater;
		},
		onSortingChange: (updater) => {
			const next = typeof updater === 'function' ? updater(sorting) : updater;
			sorting = next;
			const first = next[0];
			const sortState = first
				? { column: String(first.id), direction: (first.desc ? 'desc' : 'asc') as 'asc' | 'desc' }
				: undefined;
			if (enablePersist && prefs) {
				prefs.current = {
					...prefs.current,
					s: encodeSort(sortState)
				};
			}
			if (sortState) {
				requestOptions = {
					...requestOptions,
					sort: sortState,
					pagination: {
						page: 1,
						limit: requestOptions?.pagination?.limit ?? items?.pagination?.itemsPerPage ?? 10
					}
				};
			} else {
				requestOptions = {
					...requestOptions,
					sort: undefined,
					pagination: {
						page: 1,
						limit: requestOptions?.pagination?.limit ?? items?.pagination?.itemsPerPage ?? 10
					}
				};
			}
			onRefresh(requestOptions);
		},
		onColumnFiltersChange: (updater) => {
			columnFilters = typeof updater === 'function' ? updater(columnFilters) : updater;
			if (enablePersist && prefs) {
				prefs.current = { ...prefs.current, f: encodeFilters(columnFilters) };
			}
			requestOptions = {
				...requestOptions,
				filters: toFilterMap(columnFilters),
				pagination: {
					page: 1,
					limit: requestOptions?.pagination?.limit ?? items?.pagination?.itemsPerPage ?? 10
				}
			};
			onRefresh(requestOptions);
		},
		onColumnVisibilityChange: (updater) => {
			columnVisibility = typeof updater === 'function' ? updater(columnVisibility) : updater;
			// Persist visibility
			if (enablePersist && prefs) {
				prefs.current = { ...prefs.current, v: encodeHidden(columnVisibility) };
			}
		},
		onGlobalFilterChange: (value) => {
			globalFilter = (value ?? '') as string;
			const limit = requestOptions?.pagination?.limit ?? items?.pagination?.itemsPerPage ?? 10;
			requestOptions = {
				...requestOptions,
				search: globalFilter,
				pagination: { page: 1, limit }
			};
			// Persist global filter
			if (enablePersist && prefs) {
				prefs.current = { ...prefs.current, g: globalFilter };
			}
			onRefresh(requestOptions);
		},
		getCoreRowModel: getCoreRowModel()
	});

	function onToggleMobileField(fieldId: string) {
		mobileFieldVisibility = {
			...mobileFieldVisibility,
			[fieldId]: !mobileFieldVisibility[fieldId]
		};
		// Persist mobile field visibility
		if (enablePersist && prefs) {
			prefs.current = { ...prefs.current, m: encodeMobileVisibility(mobileFieldVisibility) };
		}
	}

	const mobileFieldsForOptions = $derived(
		mobileFields.map((field) => ({
			id: field.id,
			label: field.label,
			visible: mobileFieldVisibility[field.id] ?? true
		}))
	);

	// Compute grouped rows when groupBy is provided
	const groupedRows = $derived.by((): GroupedData<TData>[] | null => {
		if (!groupBy) return null;

		const groups = new Map<string, TData[]>();
		for (const item of items.data ?? []) {
			const groupName = groupBy(item);
			if (!groups.has(groupName)) {
				groups.set(groupName, []);
			}
			groups.get(groupName)!.push(item);
		}

		return Array.from(groups.entries()).map(([groupName, groupItems]) => ({
			groupName,
			items: groupItems
		}));
	});

	// Get selection state for a group
	function getGroupSelectionState(groupItems: TData[]): GroupSelectionState {
		const groupIds = groupItems.map((item) => item.id);
		const selectedSet = new Set(selectedIds ?? []);
		const selectedCount = groupIds.filter((id) => selectedSet.has(id)).length;

		if (selectedCount === 0) return 'none';
		if (selectedCount === groupIds.length) return 'all';
		return 'some';
	}

	// Toggle selection for all items in a group
	function onToggleGroupSelection(groupItems: TData[]) {
		const groupIds = groupItems.map((item) => item.id);
		const state = getGroupSelectionState(groupItems);

		if (state === 'all') {
			// Deselect all in group
			const groupSet = new Set(groupIds);
			selectedIds = (selectedIds ?? []).filter((id) => !groupSet.has(id));
		} else {
			// Select all in group
			const set = new Set([...(selectedIds ?? []), ...groupIds]);
			selectedIds = Array.from(set);
		}
	}

	// Handle group collapse toggle
	function handleGroupToggle(groupName: string) {
		if (onGroupToggle) {
			onGroupToggle(groupName);
		} else {
			// Default behavior: toggle collapsed state
			groupCollapsedState = {
				...groupCollapsedState,
				[groupName]: !groupCollapsedState[groupName]
			};
		}
	}

	$effect(() => {
		const s = requestOptions?.sort;
		const currentSort = untrack(() => sorting[0]);

		if (!s) {
			if (currentSort) {
				untrack(() => {
					sorting = [];
				});
			}
			return;
		}

		const desc = s.direction === 'desc';
		if (!currentSort || currentSort.id !== s.column || currentSort.desc !== desc) {
			untrack(() => {
				sorting = [{ id: s.column, desc }];
			});
		}
	});

	// Track last persisted settings to prevent infinite loops
	let lastPersistedSettings: string | null = null;
	let persistTimeout: ReturnType<typeof setTimeout> | null = null;

	$effect(() => {
		if (!enablePersist || !prefs) return;

		// Read current settings without creating dependency on the stringified value
		const currentSettings = customSettings;
		const settingsJson = JSON.stringify(currentSettings);

		// Skip if unchanged
		if (settingsJson === lastPersistedSettings) return;

		// Debounce persistence to prevent rapid updates
		if (persistTimeout) clearTimeout(persistTimeout);

		persistTimeout = setTimeout(() => {
			untrack(() => {
				if (prefs && settingsJson !== lastPersistedSettings) {
					lastPersistedSettings = settingsJson;
					prefs.current = { ...prefs.current, c: currentSettings };
				}
			});
		}, 100);

		return () => {
			if (persistTimeout) clearTimeout(persistTimeout);
		};
	});
</script>

{#snippet PaginationSnippet()}
	<ArcaneTablePagination
		{table}
		{items}
		{currentPage}
		{totalPages}
		{totalItems}
		{pageSize}
		{canPrev}
		{canNext}
		{setPage}
		{setPageSize}
	/>
{/snippet}

{#if customTableView}
	{@render customTableView({ table, renderPagination: PaginationSnippet, mobileFieldsForOptions, onToggleMobileField })}
{:else if unstyled}
	<div class="flex h-full min-h-0 flex-col">
		{#if !withoutSearch}
			<div class="w-full shrink-0 border-b">
				<DataTableToolbar
					{table}
					{selectedIds}
					{selectionDisabled}
					{bulkActions}
					mobileFields={mobileFieldsForOptions}
					{onToggleMobileField}
					{customViewOptions}
					{customToolbarActions}
					{imageNameFilterOptions}
				/>
			</div>
		{/if}

		<div class="hidden h-full min-h-0 flex-1 overflow-auto md:block">
			<ArcaneTableDesktopView
				{table}
				{selectedIds}
				columnsCount={columnsDef.length}
				{groupedRows}
				{groupIcon}
				{groupCollapsedState}
				{selectionDisabled}
				onGroupToggle={handleGroupToggle}
				{getGroupSelectionState}
				{onToggleGroupSelection}
				onToggleRowSelection={(id, selected) => onToggleRow(selected, id)}
				{unstyled}
			/>
		</div>

		<div class="block flex-1 overflow-auto md:hidden">
			<div class="divide-border/40 divide-y">
				<ArcaneTableMobileView
					{table}
					{mobileCard}
					{mobileFieldVisibility}
					{groupedRows}
					{groupIcon}
					{groupCollapsedState}
					onGroupToggle={handleGroupToggle}
					{unstyled}
				/>
			</div>
		</div>

		{#if !withoutPagination}
			<div class="shrink-0 border-t px-2 py-4">
				{@render PaginationSnippet()}
			</div>
		{/if}
	</div>
{:else}
	<div class="bg-background/60 flex h-full min-h-0 flex-col overflow-hidden rounded-xl border backdrop-blur-sm">
		{#if !withoutSearch}
			<div class="border-border/50 w-full shrink-0 border-b">
				<DataTableToolbar
					{table}
					{selectedIds}
					{selectionDisabled}
					{bulkActions}
					mobileFields={mobileFieldsForOptions}
					{onToggleMobileField}
					{customViewOptions}
					{customToolbarActions}
					{imageNameFilterOptions}
				/>
			</div>
		{/if}

		<div class="hidden h-full min-h-0 flex-1 overflow-auto md:block">
			<ArcaneTableDesktopView
				{table}
				{selectedIds}
				columnsCount={columnsDef.length}
				{groupedRows}
				{groupIcon}
				{groupCollapsedState}
				{selectionDisabled}
				onGroupToggle={handleGroupToggle}
				{getGroupSelectionState}
				{onToggleGroupSelection}
				onToggleRowSelection={(id, selected) => onToggleRow(selected, id)}
			/>
		</div>

		<div class="block flex-1 overflow-auto md:hidden">
			<ArcaneTableMobileView
				{table}
				{mobileCard}
				{mobileFieldVisibility}
				{groupedRows}
				{groupIcon}
				{groupCollapsedState}
				onGroupToggle={handleGroupToggle}
			/>
		</div>

		{#if !withoutPagination}
			<div class="border-border/50 shrink-0 border-t px-2 py-4">
				{@render PaginationSnippet()}
			</div>
		{/if}
	</div>
{/if}
