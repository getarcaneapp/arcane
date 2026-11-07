<script lang="ts" generics="TData extends Record<string, any> & { id: string }">
	import { createTable, createTableState, renderComponent, renderSnippet } from '@tanstack/svelte-table';
	import type { ColumnFiltersState, RowSelectionState, SortingState, ColumnVisibilityState } from '@tanstack/table-core';
	import { PersistedState } from 'runed';
	import { onDestroy, onMount } from 'svelte';
	import type { Snippet } from 'svelte';
	import type { Component } from 'svelte';
	import { SvelteSet } from 'svelte/reactivity';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { TABLE_PAGE_SIZE_ALL } from '#lib/constants/table-pagination.js';
	import { IsMobile } from '#lib/hooks/is-mobile.svelte.js';
	import { AlertIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import settingsStore from '#lib/stores/config-store.svelte.js';
	import type { EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
	import type { Paginated } from '#lib/types/base.js';
	import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
	import { cn } from '#lib/utils.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { rowKey, type EnvironmentFailure } from '#lib/utils/environments.js';
	import { trackOverflow, type ScrollOverflow } from '#lib/utils/overflow.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import ArcaneTableCell from './arcane-table-cell.svelte';
	import TableCheckbox from './arcane-table-checkbox.svelte';
	import ArcaneTableDesktopView from './arcane-table-desktop-view.svelte';
	import ArcaneTableGridView from './arcane-table-grid-view.svelte';
	import ArcaneTableHeader from './arcane-table-header.svelte';
	import ArcaneTableMobileView from './arcane-table-mobile-view.svelte';
	import ArcaneTablePagination from './arcane-table-pagination.svelte';
	import DataTableToolbar from './arcane-table-toolbar.svelte';
	import type { ColumnSpec } from './arcane-table.types.svelte';
	import {
		type CompactTablePrefs,
		type FieldSpec,
		type GroupedData,
		type GroupSelectionState,
		type SelectionModifiers,
		type SortState,
		type TableViewMode,
		encodeHidden,
		applyHiddenPatch,
		encodeFilters,
		encodeSort,
		encodeMobileVisibility,
		buildMobileVisibility,
		type BulkAction
	} from './arcane-table.types.svelte';
	import {
		extractPersistedPreferences,
		fromFilterMap,
		getTableRowsForItems,
		restoreTableRequestOptions,
		toFilterMap
	} from './arcane-table.utils';
	import TableEmpty from './table-empty.svelte';
	import { arcaneTableFeatures, type ArcaneColumnDef, type ArcaneRow, type ArcaneTable } from './table-features';

	let {
		items,
		requestOptions = $bindable(),
		withoutSearch = $bindable(),
		withoutFilters = false,
		withoutPagination = false,
		restoreAllPageSize = true,
		selectionDisabled = false,
		loading = false,
		unstyled = false,
		onRetry,
		onPageSizeChangeRequest,
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
		viewMode = $bindable<TableViewMode>('list'),
		hiddenSortFallback,
		groupedRows = null,
		groupBy,
		groupIcon,
		groupCollapsedState = $bindable<Record<string, boolean>>({}),
		onGroupToggle,
		imageNameFilterOptions,
		expandedRowContent,
		emptyState
	}: {
		items: Paginated<TData> & { failedEnvironments?: EnvironmentFailure[]; excludedEnvironments?: EnvironmentRef[] };
		requestOptions: SearchPaginationSortRequest;
		withoutSearch?: boolean;
		withoutFilters?: boolean;
		withoutPagination?: boolean;
		restoreAllPageSize?: boolean;
		selectionDisabled?: boolean;
		loading?: boolean;
		unstyled?: boolean;
		onRetry?: () => Promise<unknown>;
		onPageSizeChangeRequest?: (limit: number, apply: () => void) => void;
		columns: ColumnSpec<TData>[];
		rowActions?: Snippet<[{ row: ArcaneRow<TData>; item: TData; view: TableViewMode }]>;
		mobileCard: Snippet<
			[{ row: ArcaneRow<TData>; item: TData; mobileFieldVisibility: Record<string, boolean>; expander?: Snippet }]
		>;
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
		viewMode?: TableViewMode;
		hiddenSortFallback?: SortState;
		groupedRows?: GroupedData<TData>[] | null;
		groupBy?: (item: TData) => string;
		groupIcon?: (groupName: string) => Component;
		groupCollapsedState?: Record<string, boolean>;
		onGroupToggle?: (groupName: string) => void;
		imageNameFilterOptions?: string[];
		expandedRowContent?: Snippet<[{ row: ArcaneRow<TData>; item: TData }]>;
		emptyState?: Snippet;
	} = $props();

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

	const expandedRows = new SvelteSet<string>();

	const wrapTextPref = new PersistedState<boolean>('arcane-table-wrap-text', false);
	const wrapText = $derived(wrapTextPref.current);
	function toggleWrapText() {
		wrapTextPref.current = !wrapTextPref.current;
	}

	let desktopScrollEl = $state<HTMLElement>();
	let gridScrollEl = $state<HTMLElement>();
	let overflow = $state.raw<ScrollOverflow>({ below: false, left: false, right: false, scrollable: false });
	let mobileScrollEl = $state<HTMLElement>();
	const isMobile = new IsMobile();
	const isNarrowViewport = new IsMobile({ honorLayoutMode: false });
	const scrollPositions = { desktop: { top: 0, left: 0 }, grid: { top: 0, left: 0 }, mobile: { top: 0, left: 0 } };

	function setViewMode(mode: TableViewMode) {
		viewMode = mode;
		if (enablePersist && prefs) prefs.current = { ...prefs.current, d: mode };
	}
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

	function fitToViewport(node: HTMLElement) {
		const ancestors: HTMLElement[] = [];
		for (let el = node.parentElement; el; el = el.parentElement) ancestors.push(el);
		const scrollContainer =
			ancestors.find((el) => ['auto', 'scroll', 'overlay'].includes(getComputedStyle(el).overflowY)) ?? document.documentElement;
		let frame = 0;
		const update = () => {
			cancelAnimationFrame(frame);
			frame = requestAnimationFrame(() => {
				let top = node.getBoundingClientRect().top;
				let bottomGap = parseFloat(getComputedStyle(node).marginBottom);
				for (const el of ancestors) {
					const style = getComputedStyle(el);
					top += el.scrollTop;
					bottomGap += parseFloat(style.paddingBottom) + parseFloat(style.borderBottomWidth) + parseFloat(style.marginBottom);
				}
				const available = window.innerHeight - top - bottomGap;
				node.style.maxHeight = `${Math.max(available, window.innerHeight / 2)}px`;
			});
		};
		const observer = new ResizeObserver(update);
		observer.observe(scrollContainer);
		window.addEventListener('resize', update);
		update();
		return () => {
			cancelAnimationFrame(frame);
			observer.disconnect();
			window.removeEventListener('resize', update);
			node.style.maxHeight = '';
		};
	}

	function toggleRowExpanded(rowId: string) {
		if (expandedRows.has(rowId)) {
			expandedRows.delete(rowId);
		} else {
			expandedRows.add(rowId);
		}
	}

	const clientSortAccessors = $derived.by(() => {
		return new Map(
			columns.flatMap((spec, i): [string, (row: TData) => unknown][] => {
				if (!spec.clientSort) return [];
				const accessorKey = spec.accessorKey;
				return [
					[
						spec.id ?? (spec.accessorKey as string) ?? `col_${i}`,
						spec.accessorFn ?? ((row: TData) => (accessorKey ? row[accessorKey] : undefined))
					]
				];
			})
		);
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
	const canNext = $derived(totalPages < 0 ? (items.pagination?.hasNextPage ?? false) : currentPage < totalPages);
	const emptyFirstPage = $derived(items.pagination?.totalItems === 0 && currentPage === 1);

	onMount(() => {
		if (persistKey && !prefs) {
			prefs = new PersistedState<CompactTablePrefs>(
				persistKey,
				{ v: [], f: [], g: '', l: getEffectiveLimit() },
				{ syncTabs: false }
			);
		}

		if (!enablePersist) {
			preferencesReady = true;
			return;
		}
		const snapshot = extractPersistedPreferences(prefs?.current, getEffectiveLimit());
		if (snapshot.viewMode) viewMode = snapshot.viewMode;
		if (!restoreAllPageSize && snapshot.limit === TABLE_PAGE_SIZE_ALL) {
			snapshot.limit = DEFAULT_LIMIT;
		}

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
		scrollPositions.desktop.top = 0;
		scrollPositions.grid.top = 0;
		scrollPositions.mobile.top = 0;
		if (desktopScrollEl) desktopScrollEl.scrollTop = 0;
		if (gridScrollEl) gridScrollEl.scrollTop = 0;
		if (mobileScrollEl) mobileScrollEl.scrollTop = 0;
		if (mobilePageScroll && mobileScrollEl && mobileScrollEl.getBoundingClientRect().top < 0) {
			mobileScrollEl.scrollIntoView({ block: 'start' });
		}
		const next = { ...prev, ...patch };
		requestOptions = { ...requestOptions, pagination: next };
	}

	function setPage(page: number) {
		if (page < 1) page = 1;
		if (totalPages > 0 && page > totalPages) page = totalPages;
		updatePagination({ page });
	}

	function setPageSize(limit: number) {
		if (limit === pageSize) return;
		const apply = () => {
			if (enablePersist && prefs) prefs.current = { ...prefs.current, l: limit };
			updatePagination({ limit, page: 1 });
		};
		if (onPageSizeChangeRequest) {
			onPageSizeChangeRequest(limit, apply);
		} else {
			apply();
		}
	}

	function onToggleAll(checked: boolean, table: ArcaneTable<TData>) {
		const pageIds = table.getRowModel().rows.map((r) => r.id);
		if (checked) {
			const set = new Set([...(selectedIds ?? []), ...pageIds]);
			selectedIds = Array.from(set);
		} else {
			const pageSet = new Set(pageIds);
			selectedIds = (selectedIds ?? []).filter((id) => !pageSet.has(id));
		}
	}

	let rangeAnchor: { id: string; orderKey: string; selection: string[] | undefined } | null = null;

	function onToggleRow(checked: boolean, id: string, modifiers?: SelectionModifiers) {
		const ids = orderedRowIds;
		const orderKey = ids.join('\0');
		const anchor =
			modifiers?.shiftKey && rangeAnchor?.orderKey === orderKey && rangeAnchor.selection === selectedIds ? rangeAnchor : null;
		const anchorIndex = anchor ? ids.indexOf(anchor.id) : -1;
		const targetIndex = anchor ? ids.indexOf(id) : -1;
		const useRange = anchorIndex >= 0 && targetIndex >= 0;
		const rangeIds = useRange ? ids.slice(Math.min(anchorIndex, targetIndex), Math.max(anchorIndex, targetIndex) + 1) : [id];
		if (checked) {
			selectedIds = Array.from(new Set([...(selectedIds ?? []), ...rangeIds]));
		} else {
			const remove = new Set(rangeIds);
			selectedIds = (selectedIds ?? []).filter((x) => !remove.has(x));
		}
		rangeAnchor = { id: useRange && anchor ? anchor.id : id, orderKey, selection: selectedIds };
	}

	function buildColumns(specs: ColumnSpec<TData>[], isSelectionDisabled: boolean): ArcaneColumnDef<TData>[] {
		const cols: ArcaneColumnDef<TData>[] = [];

		if (!isSelectionDisabled) {
			cols.push({
				id: 'select',
				header: ({ table }) => {
					const pageIds = table.getRowModel().rows.map((r) => r.id);
					const selectedSet = new Set(selectedIds ?? []);
					const total = pageIds.length;
					const selectedOnPage = pageIds.filter((id) => selectedSet.has(id)).length;
					const checked = total > 0 && selectedOnPage === total;
					const indeterminate = selectedOnPage > 0 && selectedOnPage < total;

					return renderComponent(TableCheckbox, {
						checked,
						indeterminate,
						disabled: total === 0,
						onCheckedChange: (value) => onToggleAll(!!value, table),
						'aria-label': m.common_select_all()
					});
				},
				cell: ({ row }) => {
					const id = row.id;
					return renderComponent(TableCheckbox, {
						checked: (selectedIds ?? []).includes(id),
						onCheckedChange: (value, modifiers) => onToggleRow(!!value, id, modifiers),
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
					truncate: spec.truncate,
					card: spec.card,
					cardIcon: spec.cardIcon,
					cardLabel: spec.cardLabel
				},
				header: ({ column }) => {
					if (spec.header) return renderSnippet(spec.header, { column, title: spec.title, class: spec.class });
					return renderComponent(ArcaneTableHeader, {
						column: spec.sortable || spec.clientSort ? column : undefined,
						title: spec.title,
						align: spec.align,
						class: spec.class
					});
				},
				cell: ({ row, getValue }) => {
					const item = row.original as TData;
					const value = accessorKey ? row.getValue(accessorKey) : getValue?.();
					if (spec.cell) return renderSnippet(spec.cell, { row, item, value, view: viewMode });
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
				cell: ({ row }) =>
					rowActions ? renderSnippet(rowActions, { row, item: row.original as TData, view: viewMode }) : undefined
			});
		}

		return cols;
	}

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

	const columnsDef = $derived(buildColumns(columns, selectionDisabled));

	const table = createTable({
		features: arcaneTableFeatures,
		manualSorting: true,
		manualFiltering: true,
		getRowId: (row) => rowKey(row),
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
		},
		onColumnVisibilityChange: (updater) => {
			let nextVisibility: ColumnVisibilityState;
			if (typeof updater === 'function') nextVisibility = updater(effectiveColumnVisibility);
			else nextVisibility = updater;
			columnVisibility = nextVisibility;
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
			if (enablePersist && prefs) {
				prefs.current = { ...prefs.current, g: globalFilter() };
			}
		}
	});

	const effectiveColumnsCount = $derived(table.getVisibleLeafColumns().length + (expandedRowContent ? 1 : 0));

	function onToggleMobileField(fieldId: string) {
		mobileFieldVisibility = {
			...mobileFieldVisibility,
			[fieldId]: !mobileFieldVisibility[fieldId]
		};
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

	let builtRowIndex: { rows: ArcaneRow<TData>[]; index: Map<string, { row: ArcaneRow<TData>; index: number }> } | undefined;
	const rowIndex = $derived.by(() => {
		const rows = table.getRowModel().rows;
		if (builtRowIndex?.rows !== rows)
			builtRowIndex = { rows, index: new Map(rows.map((row, index) => [row.id, { row, index }])) };
		return builtRowIndex.index;
	});

	const effectiveGroupedRows = $derived.by((): GroupedData<TData>[] | null => {
		if (groupedRows) return groupedRows;
		if (!groupBy) return null;

		return Array.from(Map.groupBy(items.data ?? [], groupBy).entries()).map(([groupName, groupItems]) => ({
			groupName,
			items: groupItems
		}));
	});

	const orderedRowIds = $derived.by((): string[] => {
		const groups = effectiveGroupedRows;
		if (!groups || groups.length === 0) return table.getRowModel().rows.map((row) => row.id);
		return groups.flatMap((group) =>
			(groupCollapsedState[group.groupName] ?? true) ? [] : getTableRowsForItems(rowIndex, group.items).map((row) => row.id)
		);
	});

	function getGroupSelectionState(groupItems: TData[]): GroupSelectionState {
		const groupIds = groupItems.map((item) => rowKey(item));
		const selectedCount = groupIds.filter((id) => selectedIdSet.has(id)).length;

		if (selectedCount === 0) return 'none';
		if (selectedCount === groupIds.length) return 'all';
		return 'some';
	}

	function onToggleGroupSelection(groupItems: TData[]) {
		const groupIds = groupItems.map((item) => rowKey(item));
		const state = getGroupSelectionState(groupItems);

		if (state === 'all') {
			const groupSet = new Set(groupIds);
			selectedIds = (selectedIds ?? []).filter((id) => !groupSet.has(id));
		} else {
			const set = new Set([...(selectedIds ?? []), ...groupIds]);
			selectedIds = Array.from(set);
		}
	}

	function handleGroupToggle(groupName: string) {
		if (onGroupToggle) {
			onGroupToggle(groupName);
		} else {
			groupCollapsedState = {
				...groupCollapsedState,
				[groupName]: !(groupCollapsedState[groupName] ?? true)
			};
		}
	}

	let lastPersistedSettings: string | null = null;
	let persistTimeout: ReturnType<typeof setTimeout> | undefined;

	export function persistSort(sort: SortState | undefined) {
		if (!enablePersist || !prefs) return;
		prefs.current = { ...prefs.current, s: encodeSort(sort) };
	}

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

	const mobileVirtualized = $derived(table.getRowModel().rows.length > 100);
	const mobilePageScroll = $derived(isMobile.current && isNarrowViewport.current && !mobileVirtualized);

	const isFiltered = $derived(
		!!table.atoms.globalFilter.get() || (!withoutFilters && table.atoms.columnFilters.get().length > 0)
	);

	function resetFilters() {
		if (!withoutFilters) table.setColumnFilters([]);
		table.setGlobalFilter('');
	}

	const shellClass = $derived(
		unstyled ? 'flex h-full min-h-0 flex-col' : 'flex h-full min-h-0 flex-col overflow-hidden rounded-xl border bg-card/30'
	);
	const toolbarWrapClass = $derived(unstyled ? 'w-full shrink-0 border-b' : 'w-full shrink-0 bg-table-head');
</script>

{#snippet PaginationSnippet()}
	<ArcaneTablePagination
		shown={items.data.length}
		{currentPage}
		{totalPages}
		{totalItems}
		{pageSize}
		{canPrev}
		{canNext}
		{setPage}
	/>
{/snippet}

{#snippet EmptySnippet()}
	{#if !isFiltered && emptyState}
		{@render emptyState()}
	{:else}
		<TableEmpty onClear={isFiltered ? resetFilters : undefined} />
	{/if}
{/snippet}

{#snippet MobileViewSnippet()}
	<ArcaneTableMobileView
		scrollElement={mobileScrollEl}
		initialScrollTop={scrollPositions.mobile.top}
		{rowIndex}
		{table}
		{mobileCard}
		{mobileFieldVisibility}
		groupedRows={effectiveGroupedRows}
		{groupIcon}
		{expandedRowContent}
		{expandedRows}
		onToggleRowExpanded={toggleRowExpanded}
		{loading}
		virtualize={mobileVirtualized}
		{selectedIdSet}
		{selectionDisabled}
		onToggleRowSelection={(id, selected, modifiers) => onToggleRow(selected, id, modifiers)}
		empty={EmptySnippet}
	/>
{/snippet}

{#if customTableView}
	{@render customTableView({ table, renderPagination: PaginationSnippet, mobileFieldsForOptions, onToggleMobileField })}
{:else}
	<div {@attach mobilePageScroll ? undefined : fitToViewport} class={shellClass}>
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
					{viewMode}
					onViewModeChange={isMobile.current ? undefined : setViewMode}
					{isFiltered}
					onResetFilters={resetFilters}
					pagination={withoutPagination || emptyFirstPage ? undefined : PaginationSnippet}
					pageSize={withoutPagination ? undefined : pageSize}
					onPageSizeChange={withoutPagination ? undefined : setPageSize}
				/>
			</div>
		{/if}

		{#if items.failedEnvironments?.length}
			<div class="shrink-0 px-3 pt-3">
				<Alert.Root variant="warning-subtle" size="sm" icon={AlertIcon}>
					<div class="flex flex-wrap items-center justify-between gap-2">
						<Alert.Description>
							{m.all_environments_unavailable({
								environments: items.failedEnvironments
									.map(({ environment, message }) => `${environment.name} (${message})`)
									.join(', ')
							})}
						</Alert.Description>
						{#if onRetry}
							{@const retry = onRetry}
							<ArcaneButton
								action="refresh"
								customLabel={m.common_retry()}
								onClickPromise={async () =>
									handleApiResultWithCallbacks({ result: await tryCatch(retry()), message: m.common_failed() })}
							/>
						{/if}
					</div>
				</Alert.Root>
			</div>
		{/if}

		{#if items.excludedEnvironments?.length}
			<div class="shrink-0 px-3 pt-3">
				<Alert.Root variant="warning-subtle" size="sm" icon={AlertIcon}>
					<Alert.Description>
						{m.all_environments_excluded({
							count: items.excludedEnvironments.length,
							limit: settingsStore.current?.allEnvironmentsLimit ?? 0
						})}
					</Alert.Description>
				</Alert.Root>
			</div>
		{/if}

		{#if !isMobile.current && viewMode === 'grid'}
			<div
				{@attach (node) => restoreScroll(node, 'grid')}
				{@attach trackOverflow((value) => (overflow = value))}
				bind:this={gridScrollEl}
				data-table-scroll="grid"
				class={cn(
					'isolate h-full min-h-0 flex-1 overflow-auto',
					overflow.below && 'scroll-fade-b',
					overflow.left && 'scroll-fade-l',
					overflow.right && 'scroll-fade-r'
				)}
			>
				<ArcaneTableGridView
					scrollElement={gridScrollEl}
					initialScrollTop={scrollPositions.grid.top}
					{rowIndex}
					{table}
					{selectedIdSet}
					{selectionDisabled}
					onToggleRowSelection={(id, selected, modifiers) => onToggleRow(selected, id, modifiers)}
					groupedRows={effectiveGroupedRows}
					{groupIcon}
					{groupCollapsedState}
					onGroupToggle={handleGroupToggle}
					{getGroupSelectionState}
					{onToggleGroupSelection}
					{expandedRowContent}
					{expandedRows}
					onToggleRowExpanded={toggleRowExpanded}
					{loading}
					empty={EmptySnippet}
				/>
			</div>
		{:else if !isMobile.current}
			<div
				{@attach (node) => restoreScroll(node, 'desktop')}
				{@attach trackOverflow((value) => (overflow = value))}
				bind:this={desktopScrollEl}
				data-table-scroll="desktop"
				data-overflow-right={overflow.right || undefined}
				class={cn(
					'isolate h-full min-h-0 flex-1 overflow-auto',
					overflow.below && 'scroll-fade-b',
					overflow.left && 'scroll-fade-l',
					overflow.right && 'scroll-fade-r'
				)}
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
					onToggleRowSelection={(id, selected, modifiers) => onToggleRow(selected, id, modifiers)}
					{unstyled}
					{expandedRowContent}
					{expandedRows}
					onToggleRowExpanded={toggleRowExpanded}
					scrollElement={desktopScrollEl}
					{loading}
					{wrapText}
					empty={EmptySnippet}
				/>
			</div>
		{:else}
			<div
				{@attach (node) => restoreScroll(node, 'mobile')}
				{@attach trackOverflow((value) => (overflow = value))}
				bind:this={mobileScrollEl}
				data-table-scroll="mobile"
				class={cn(
					'isolate block',
					!mobilePageScroll && 'min-h-0 flex-1 overflow-auto',
					overflow.below && 'scroll-fade-b',
					overflow.left && 'scroll-fade-l',
					overflow.right && 'scroll-fade-r'
				)}
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

		{#if withoutSearch && !withoutPagination && !emptyFirstPage}
			<div class="flex shrink-0 justify-end px-4 py-3">
				{@render PaginationSnippet()}
			</div>
		{/if}
	</div>
{/if}
