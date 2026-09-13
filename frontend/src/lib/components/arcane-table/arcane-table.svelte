<script lang="ts" generics="TData extends Record<string, any> & { id: string }">
	import { createTable, createTableState, renderComponent, renderSnippet } from '@tanstack/svelte-table';
	import type { ColumnFiltersState, RowSelectionState, SortingState, ColumnVisibilityState } from '@tanstack/table-core';
	import { arcaneTableFeatures, type ArcaneColumnDef, type ArcaneRow, type ArcaneTable } from './table-features';
	import DataTableToolbar from './arcane-table-toolbar.svelte';
	import { onDestroy, onMount, untrack } from 'svelte';
	import { IsMobile } from '#lib/hooks/is-mobile.svelte.js';
	import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
	import type { Snippet } from 'svelte';
	import type { ColumnSpec } from './arcane-table.types.svelte';
	import TableCheckbox from './arcane-table-checkbox.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { PersistedState } from 'runed';
	import {
		type CompactTablePrefs,
		type FieldSpec,
		type GroupedData,
		type GroupSelectionState,
		type SortState,
		encodeHidden,
		applyHiddenPatch,
		encodeFilters,
		encodeSort,
		encodeMobileVisibility,
		buildMobileVisibility,
		type BulkAction
	} from './arcane-table.types.svelte';
	import type { Component } from 'svelte';
	import { extractPersistedPreferences, fromFilterMap, restoreTableRequestOptions, toFilterMap } from './arcane-table.utils';
	import ArcaneTablePagination from './arcane-table-pagination.svelte';
	import ArcaneTableHeader from './arcane-table-header.svelte';
	import ArcaneTableCell from './arcane-table-cell.svelte';
	import ArcaneTableDesktopView from './arcane-table-desktop-view.svelte';
	import ArcaneTableMobileView from './arcane-table-mobile-view.svelte';

	let {
		items,
		requestOptions = $bindable(),
		withoutSearch = $bindable(),
		withoutFilters = false,
		withoutPagination = false,
		selectionDisabled = false,
		loading = false,
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
		columnVisibility = $bindable<ColumnVisibilityState>({}),
		preferencesReady = $bindable(false),
		hiddenSortFallback,
		groupedRows = null,
		// Grouping props
		groupBy,
		groupIcon,
		groupCollapsedState = $bindable<Record<string, boolean>>({}),
		onGroupToggle,
		imageNameFilterOptions,
		// Expandable row props
		expandedRowContent
	}: {
		items: Paginated<TData>;
		requestOptions: SearchPaginationSortRequest;
		withoutSearch?: boolean;
		withoutFilters?: boolean;
		withoutPagination?: boolean;
		selectionDisabled?: boolean;
		/** When true and there's no data yet, the desktop view shows skeleton rows (first-load). */
		loading?: boolean;
		unstyled?: boolean;
		onRefresh: (requestOptions: SearchPaginationSortRequest) => Promise<Paginated<TData>>;
		columns: ColumnSpec<TData>[];
		rowActions?: Snippet<[{ row: ArcaneRow<TData>; item: TData }]>;
		mobileCard: Snippet<[{ row: ArcaneRow<TData>; item: TData; mobileFieldVisibility: Record<string, boolean> }]>;
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
					table: ArcaneTable<TData>;
					renderPagination: Snippet;
					mobileFieldsForOptions: { id: string; label: string; visible: boolean }[];
					onToggleMobileField: (fieldId: string) => void;
				}
			]
		>;
		customSettings?: Record<string, unknown>;
		columnVisibility?: ColumnVisibilityState;
		preferencesReady?: boolean;
		hiddenSortFallback?: SortState;
		groupedRows?: GroupedData<TData>[] | null;
		// Grouping props
		groupBy?: (item: TData) => string;
		groupIcon?: (groupName: string) => Component;
		groupCollapsedState?: Record<string, boolean>;
		onGroupToggle?: (groupName: string) => void;
		imageNameFilterOptions?: string[];
		// Expandable row props
		expandedRowContent?: Snippet<[{ row: ArcaneRow<TData>; item: TData }]>;
	} = $props();

	// Default page size constant
	const DEFAULT_LIMIT = 20;

	const [rowSelection, setRowSelection] = createTableState<RowSelectionState>({});
	const columnFilters = $derived(fromFilterMap(requestOptions?.filters));
	const serverSort = $derived(requestOptions?.sort);
	let clientSortOverride = $state.raw<{
		serverSort: SearchPaginationSortRequest['sort'];
		column?: string;
		direction?: string;
		sorting: SortingState;
	} | null>(null);
	const sorting = $derived.by((): SortingState => {
		if (
			clientSortOverride &&
			clientSortOverride.serverSort === serverSort &&
			clientSortOverride.column === serverSort?.column &&
			clientSortOverride.direction === serverSort?.direction
		) {
			return clientSortOverride.sorting;
		}
		if (!serverSort) return [];
		return [{ id: serverSort.column, desc: serverSort.direction === 'desc' }];
	});
	const [globalFilter, setGlobalFilter] = createTableState<string>(requestOptions?.search ?? '');

	const enablePersist = $derived(!!persistKey);
	const getEffectiveLimit = () => requestOptions?.pagination?.limit ?? items?.pagination?.itemsPerPage ?? DEFAULT_LIMIT;
	let prefs = $state<PersistedState<CompactTablePrefs> | null>(null);

	// Expandable row state
	let expandedRows = $state<Set<string>>(new Set());

	// Wrap-text preference, shared by every table (view-options toggle).
	const wrapTextPref = new PersistedState<boolean>('arcane-table-wrap-text', false);
	const wrapText = $derived(wrapTextPref.current);
	function toggleWrapText() {
		wrapTextPref.current = !wrapTextPref.current;
	}

	// The desktop scroll container, bound below and handed to the desktop view so it can virtualize
	// long flat lists. The unstyled/styled branches are mutually exclusive, so one ref suffices.
	let desktopScrollEl = $state<HTMLElement>();
	const isMobile = new IsMobile();
	const scrollPositions = { desktop: { top: 0, left: 0 }, mobile: { top: 0, left: 0 } };
	const selectedIdSet = $derived(new Set(selectedIds ?? []));

	function restoreScroll(node: HTMLElement, view: keyof typeof scrollPositions) {
		const position = scrollPositions[view];
		const frame = requestAnimationFrame(() => {
			node.scrollTop = position.top;
			node.scrollLeft = position.left;
			node.addEventListener('scroll', savePosition, { passive: true });
		});
		function savePosition() {
			position.top = node.scrollTop;
			position.left = node.scrollLeft;
		}
		return () => {
			cancelAnimationFrame(frame);
			node.removeEventListener('scroll', savePosition);
		};
	}

	function toggleRowExpanded(rowId: string) {
		const next = new Set(expandedRows);
		if (next.has(rowId)) {
			next.delete(rowId);
		} else {
			next.add(rowId);
		}
		expandedRows = next;
	}

	// Client-sorted columns (spec.clientSort): the server can't order these values,
	// so header clicks reorder the current page locally via the column accessor.
	const clientSortAccessors = $derived.by(() => {
		const accessors = new Map<string, (row: TData) => unknown>();
		columns.forEach((spec, i) => {
			if (!spec.clientSort) return;
			const id = spec.id ?? (spec.accessorKey as string) ?? `col_${i}`;
			const accessorKey = spec.accessorKey;
			const accessorFn = spec.accessorFn;
			accessors.set(id, accessorFn ?? ((row: TData) => (accessorKey ? row[accessorKey] : undefined)));
		});
		return accessors;
	});

	const sortedData = $derived.by(() => {
		const data = items.data ?? [];
		const first = sorting[0];
		const accessor = first ? clientSortAccessors.get(String(first.id)) : undefined;
		if (!first || !accessor) return data;
		const direction = first.desc ? -1 : 1;
		return [...data].sort((a, b) => {
			const va = accessor(a);
			const vb = accessor(b);
			if (typeof va === 'number' && typeof vb === 'number') return (va - vb) * direction;
			return String(va ?? '').localeCompare(String(vb ?? '')) * direction;
		});
	});

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
		if (!enablePersist) {
			preferencesReady = true;
			return;
		}
		const snapshot = extractPersistedPreferences(prefs?.current, getEffectiveLimit());

		const patchedVisibility = { ...columnVisibility };
		applyHiddenPatch(patchedVisibility, snapshot.hiddenColumns);
		columnVisibility = patchedVisibility;

		const effectiveSearch = (requestOptions?.search ?? '').trim() || snapshot.search;
		if (effectiveSearch !== globalFilter()) setGlobalFilter(effectiveSearch);

		const persistedSort = snapshot.sort;
		if (
			persistedSort &&
			(patchedVisibility[persistedSort.column] ?? defaultColumnVisibility[persistedSort.column]) === false &&
			hiddenSortFallback
		) {
			snapshot.sort = hiddenSortFallback;
			if (snapshot.sort !== persistedSort && prefs) prefs.current = { ...prefs.current, s: encodeSort(snapshot.sort) };
		}
		const restoredOptions = restoreTableRequestOptions(requestOptions, snapshot, getEffectiveLimit());
		if (restoredOptions !== requestOptions) {
			requestOptions = restoredOptions;
			onRefresh(requestOptions);
		}

		if (mobileFields.length && !Object.keys(mobileFieldVisibility).length) {
			mobileFieldVisibility = buildMobileVisibility(mobileFields, snapshot.mobileVisibility);
		}

		if (snapshot.customSettings && Object.keys(snapshot.customSettings).length > 0) {
			customSettings = { ...snapshot.customSettings };
		}

		preferencesReady = true;
		persistCustomSettings(customSettings);
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

	function onToggleAll(checked: boolean, table: ArcaneTable<TData>) {
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

	function buildColumns(specs: ColumnSpec<TData>[], isSelectionDisabled: boolean): ArcaneColumnDef<TData>[] {
		const cols: ArcaneColumnDef<TData>[] = [];

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
					filterOptions: spec.filterOptions,
					width: spec.width,
					align: spec.align,
					truncate: spec.truncate
				},
				header: ({ column }) => {
					if (spec.header) return renderSnippet(spec.header, { column, title: spec.title, class: spec.class });
					return renderComponent(ArcaneTableHeader, {
						column: spec.sortable || spec.clientSort ? column : undefined,
						title: spec.title,
						class: spec.class
					});
				},
				cell: ({ row, getValue }) => {
					const item = row.original as TData;
					const value = accessorKey ? row.getValue(accessorKey) : getValue?.();
					if (spec.cell) return renderSnippet(spec.cell, { row, item, value });
					if (spec.cellComponent) return renderComponent(spec.cellComponent, { value });
					return renderComponent(ArcaneTableCell, { value });
				},
				enableSorting: !!spec.sortable || !!spec.clientSort,
				enableHiding: true
			});
		});

		if (rowActions) {
			cols.push({
				id: 'actions',
				// The closure reads the live rowActions prop. When the caller flips it
				// to undefined (e.g. a project stops and its per-row actions go away),
				// the old column can still render once before the column set rebuilds —
				// rendering an undefined snippet would crash the page (invalid_snippet).
				cell: ({ row }) => (rowActions ? renderSnippet(rowActions, { row, item: row.original as TData }) : undefined)
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
				hidden[id] = false;
			}
		});
		return hidden;
	}

	const defaultColumnVisibility = $derived(getInitialHiddenColumns(columns));
	const effectiveColumnVisibility = $derived({ ...defaultColumnVisibility, ...columnVisibility });

	// Memoize column definitions until their structure or facet options change.
	// Facet catalogs can arrive after the table's first render, so their metadata
	// must participate in the key even though row data does not.
	function getColumnsKey(specs: ColumnSpec<TData>[], hasRowActions: boolean, isSelectionDisabled: boolean): string {
		const columnsMetadata = specs.map((spec, index) => ({
			id: spec.id ?? spec.accessorKey ?? `col_${index}`,
			filterOptions: spec.filterOptions?.map((option) => ({
				value: option.value,
				label: option.label,
				dotClass: option.dotClass
			}))
		}));
		return `${JSON.stringify(columnsMetadata)}:${hasRowActions}:${isSelectionDisabled}`;
	}

	const columnsKey = $derived(getColumnsKey(columns, !!rowActions, selectionDisabled));
	const columnsDef = $derived.by(() => {
		columnsKey; // structural dependency: identity-churned `columns` arrays with an unchanged key don't rebuild defs
		return untrack(() => buildColumns(columns, selectionDisabled));
	});

	const table = createTable({
		features: arcaneTableFeatures,
		// Manual / server-side mode: no client row models are registered, so sorting and
		// filtering never run on the client — these flags make that explicit. Pagination is
		// fully external (no rowPaginationFeature registered), so there is no manualPagination.
		manualSorting: true,
		manualFiltering: true,
		// Stable row identity across server refreshes. Without this v9 keys rows by positional
		// index, so every onRefresh fetch re-keys all rows and forces a full re-render; keying
		// by the backing id lets unchanged rows (and their DOM/selection) be reused.
		getRowId: (row) => row.id,
		get data() {
			return sortedData;
		},
		state: {
			get sorting() {
				return sorting;
			},
			get columnVisibility() {
				return effectiveColumnVisibility;
			},
			get rowSelection() {
				return rowSelection();
			},
			get columnFilters() {
				return columnFilters;
			},
			get globalFilter() {
				return globalFilter();
			}
		},
		get columns() {
			return columnsDef;
		},
		get enableRowSelection() {
			return !selectionDisabled;
		},
		onRowSelectionChange: setRowSelection,
		onSortingChange: (updater) => {
			const wasClientSort = sorting[0] && clientSortAccessors.has(String(sorting[0].id));
			let next: SortingState;
			if (typeof updater === 'function') next = updater(sorting);
			else next = updater;
			const first = next[0];
			// Keep client-only sorting, including its cleared state, until the server sort changes.
			if ((first && clientSortAccessors.has(String(first.id))) || (!first && wasClientSort)) {
				clientSortOverride = {
					serverSort,
					column: serverSort?.column,
					direction: serverSort?.direction,
					sorting: next
				};
				return;
			}
			clientSortOverride = null;
			const sortState = first
				? { column: String(first.id), direction: (first.desc ? 'desc' : 'asc') as 'asc' | 'desc' }
				: undefined;
			if (enablePersist && prefs) {
				prefs.current = {
					...prefs.current,
					s: encodeSort(sortState)
				};
			}
			requestOptions = {
				...requestOptions,
				sort: sortState,
				pagination: {
					page: 1,
					limit: requestOptions?.pagination?.limit ?? items?.pagination?.itemsPerPage ?? 10
				}
			};
			onRefresh(requestOptions);
		},
		onColumnFiltersChange: (updater) => {
			let next: ColumnFiltersState;
			if (typeof updater === 'function') next = updater(columnFilters);
			else next = updater;
			if (enablePersist && prefs) {
				prefs.current = { ...prefs.current, f: encodeFilters(next) };
			}
			requestOptions = {
				...requestOptions,
				filters: toFilterMap(next),
				pagination: {
					page: 1,
					limit: requestOptions?.pagination?.limit ?? items?.pagination?.itemsPerPage ?? 10
				}
			};
			onRefresh(requestOptions);
		},
		onColumnVisibilityChange: (updater) => {
			let nextVisibility: ColumnVisibilityState;
			if (typeof updater === 'function') nextVisibility = updater(effectiveColumnVisibility);
			else nextVisibility = updater;
			columnVisibility = nextVisibility;
			// Persist visibility
			if (enablePersist && prefs) {
				prefs.current = { ...prefs.current, v: encodeHidden(columnVisibility) };
			}

			const activeSort = requestOptions?.sort;
			if (activeSort && nextVisibility[activeSort.column] === false && hiddenSortFallback) {
				clientSortOverride = null;
				requestOptions = {
					...requestOptions,
					sort: hiddenSortFallback,
					pagination: {
						page: 1,
						limit: requestOptions?.pagination?.limit ?? items?.pagination?.itemsPerPage ?? 10
					}
				};
				if (enablePersist && prefs) {
					prefs.current = { ...prefs.current, s: encodeSort(hiddenSortFallback) };
				}
				onRefresh(requestOptions);
			}
		},
		onGlobalFilterChange: (updater) => {
			setGlobalFilter(updater);
			if (typeof globalFilter() !== 'string') setGlobalFilter('');
			const limit = requestOptions?.pagination?.limit ?? items?.pagination?.itemsPerPage ?? 10;
			requestOptions = {
				...requestOptions,
				search: globalFilter(),
				pagination: { page: 1, limit }
			};
			// Persist global filter
			if (enablePersist && prefs) {
				prefs.current = { ...prefs.current, g: globalFilter() };
			}
			onRefresh(requestOptions);
		}
	});

	// Rendered column count (add 1 for the expand chevron column when expandable). Based on
	// visible leaf columns, not columnsDef — hidden columns (e.g. id) would otherwise inflate
	// colspans and create phantom columns that misalign grouped/empty/expanded rows.
	const effectiveColumnsCount = $derived(table.getVisibleLeafColumns().length + (expandedRowContent ? 1 : 0));

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

	const rowIndex = $derived.by(() => new Map(table.getRowModel().rows.map((row, index) => [row.original.id, { row, index }])));

	// Compute grouped rows when groupBy is provided
	const effectiveGroupedRows = $derived.by((): GroupedData<TData>[] | null => {
		if (groupedRows) return groupedRows;
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
		const selectedCount = groupIds.filter((id) => selectedIdSet.has(id)).length;

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
			// Default behavior: toggle collapsed state (unrecorded groups render collapsed)
			groupCollapsedState = {
				...groupCollapsedState,
				[groupName]: !(groupCollapsedState[groupName] ?? true)
			};
		}
	}

	let lastPersistedSettings: string | null = null;
	let persistTimeout: ReturnType<typeof setTimeout> | undefined;

	export function persistCustomSettings(settings: Record<string, unknown>) {
		if (!preferencesReady || !prefs) return;
		const settingsJson = JSON.stringify(settings);
		clearTimeout(persistTimeout);
		if (settingsJson === lastPersistedSettings) return;
		const owner = prefs;
		persistTimeout = setTimeout(() => {
			owner.current = { ...owner.current, c: settings };
			lastPersistedSettings = settingsJson;
		}, 100);
	}

	onDestroy(() => clearTimeout(persistTimeout));

	// Styled/unstyled differ only in wrapper chrome; the inner table/mobile/pagination tree is shared.
	const shellClass = $derived(
		unstyled ? 'flex h-full min-h-0 flex-col' : 'bg-background/60 flex h-full min-h-0 flex-col overflow-hidden rounded-xl border'
	);
	const toolbarWrapClass = $derived(unstyled ? 'w-full shrink-0 border-b' : 'border-border/50 w-full shrink-0 border-b');
</script>

{#snippet PaginationSnippet()}
	<ArcaneTablePagination {items} {currentPage} {totalPages} {totalItems} {pageSize} {canPrev} {canNext} {setPage} {setPageSize} />
{/snippet}

{#snippet MobileViewSnippet()}
	<ArcaneTableMobileView
		{rowIndex}
		{table}
		{mobileCard}
		{mobileFieldVisibility}
		groupedRows={effectiveGroupedRows}
		{groupIcon}
		{unstyled}
		{expandedRowContent}
		{expandedRows}
		onToggleRowExpanded={toggleRowExpanded}
		{loading}
	/>
{/snippet}

{#if customTableView}
	{@render customTableView({ table, renderPagination: PaginationSnippet, mobileFieldsForOptions, onToggleMobileField })}
{:else}
	<div class={shellClass}>
		{#if !withoutSearch}
			<div class={toolbarWrapClass}>
				<DataTableToolbar
					{table}
					{selectedIds}
					{selectionDisabled}
					{bulkActions}
					{withoutFilters}
					mobileFields={mobileFieldsForOptions}
					{onToggleMobileField}
					{customViewOptions}
					{customToolbarActions}
					{imageNameFilterOptions}
					{wrapText}
					onToggleWrapText={toggleWrapText}
				/>
			</div>
		{/if}

		{#if !isMobile.current}
			<div
				{@attach (node) => restoreScroll(node, 'desktop')}
				bind:this={desktopScrollEl}
				class="[isolation:isolate] h-full min-h-0 flex-1 overflow-auto bg-background"
			>
				<ArcaneTableDesktopView
					{rowIndex}
					{table}
					{selectedIdSet}
					initialScrollTop={scrollPositions.desktop.top}
					columnsCount={effectiveColumnsCount}
					groupedRows={effectiveGroupedRows}
					{groupIcon}
					{groupCollapsedState}
					{selectionDisabled}
					onGroupToggle={handleGroupToggle}
					{getGroupSelectionState}
					{onToggleGroupSelection}
					onToggleRowSelection={(id, selected) => onToggleRow(selected, id)}
					{unstyled}
					{expandedRowContent}
					{expandedRows}
					onToggleRowExpanded={toggleRowExpanded}
					scrollElement={desktopScrollEl}
					{loading}
					{wrapText}
				/>
			</div>
		{:else}
			<div
				{@attach (node) => restoreScroll(node, 'mobile')}
				class="[isolation:isolate] block flex-1 overflow-auto bg-background/80"
			>
				{#if unstyled}
					<div class="divide-y divide-border/40">
						{@render MobileViewSnippet()}
					</div>
				{:else}
					{@render MobileViewSnippet()}
				{/if}
			</div>
		{/if}

		{#if !withoutPagination}
			<div class="shrink-0 border-t border-border/50 px-4 py-3">
				{@render PaginationSnippet()}
			</div>
		{/if}
	</div>
{/if}
