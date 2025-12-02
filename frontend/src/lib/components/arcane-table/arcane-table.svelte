<script lang="ts" generics="TData extends {id: string}">
	import {
		type Column,
		type ColumnDef,
		type ColumnFiltersState,
		type Row,
		type RowSelectionState,
		type SortingState,
		type VisibilityState,
		type Table as TableType,
		getCoreRowModel
	} from '@tanstack/table-core';
	import DataTableToolbar from './arcane-table-toolbar.svelte';
	import { createSvelteTable } from '$lib/components/ui/data-table/data-table.svelte.js';
	import FlexRender from '$lib/components/ui/data-table/flex-render.svelte';
	import * as Table from '$lib/components/ui/table/index.js';
	import * as Card from '$lib/components/ui/card/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { renderComponent, renderSnippet } from '$lib/components/ui/data-table/render-helpers.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import ChevronRightIcon from '@lucide/svelte/icons/chevron-right';
	import ChevronLeftIcon from '@lucide/svelte/icons/chevron-left';
	import ChevronsLeftIcon from '@lucide/svelte/icons/chevrons-left';
	import ChevronsRightIcon from '@lucide/svelte/icons/chevrons-right';
	import ArrowUpIcon from '@lucide/svelte/icons/arrow-up';
	import ArrowDownIcon from '@lucide/svelte/icons/arrow-down';
	import ChevronsUpDownIcon from '@lucide/svelte/icons/chevrons-up-down';
	import EyeOffIcon from '@lucide/svelte/icons/eye-off';
	import * as Select from '$lib/components/ui/select/index.js';
	import type { SvelteHTMLElements } from 'svelte/elements';
	import { cn } from '$lib/utils.js';

	type DivAttributes = SvelteHTMLElements['div'];
	type PlainHeaderProps = { title: string } & DivAttributes;
	type ColumnHeaderProps = { column: Column<TData, unknown>; title: string } & DivAttributes;
	type PaginationProps = { table: TableType<TData> };
	type MobileCardProps = { row: Row<TData>; item: TData };
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
	import type { Snippet } from 'svelte';
	import type { ColumnSpec } from './arcane-table.types.svelte';
	import TableCheckbox from './arcane-table-checkbox.svelte';
	import { m } from '$lib/paraglide/messages';
	import { PersistedState } from 'runed';
	import {
		type CompactTablePrefs,
		type FieldSpec,
		encodeHidden,
		applyHiddenPatch,
		encodeFilters,
		encodeMobileHidden
	} from './arcane-table.types.svelte';
	import { buildInitialMobileVisibility, extractPersistedPreferences, filterMapsEqual, toFilterMap } from './arcane-table.utils';
	import * as Empty from '$lib/components/ui/empty/index.js';
	import FolderXIcon from '@lucide/svelte/icons/folder-x';

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
		onRemoveSelected,
		persistKey,
		customViewOptions,
		customTableView,
		customSettings = $bindable<Record<string, unknown>>({})
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
		onRemoveSelected?: (ids: string[]) => void;
		persistKey?: string;
		customViewOptions?: Snippet;
		customTableView?: Snippet<[{ table: TableType<TData> }]>;
		customSettings?: Record<string, unknown>;
	} = $props();

	let rowSelection = $state<RowSelectionState>({});
	let columnVisibility = $state<VisibilityState>({});
	let columnFilters = $state<ColumnFiltersState>([]);
	let sorting = $state<SortingState>([]);
	let globalFilter = $state<string>('');

	const enablePersist = $derived(!!persistKey);
	const getDefaultLimit = () => requestOptions?.pagination?.limit ?? items?.pagination?.itemsPerPage ?? 20;
	let prefs = $state<PersistedState<CompactTablePrefs> | null>(null);

	$effect(() => {
		if (persistKey) {
			prefs = new PersistedState<CompactTablePrefs>(
				persistKey,
				{ v: [], f: [], g: '', l: getDefaultLimit() },
				{ syncTabs: false }
			);
		} else {
			prefs = null;
		}
	});

	const passAllGlobal: (row: unknown, columnId: string, filterValue: unknown) => boolean = () => true;

	const currentPage = $derived(items.pagination.currentPage ?? requestOptions?.pagination?.page ?? 1);
	const totalPages = $derived(items.pagination.totalPages ?? 1);
	const totalItems = $derived(items.pagination.totalItems ?? 0);
	const pageSize = $derived(requestOptions?.pagination?.limit ?? items?.pagination?.itemsPerPage ?? 20);
	const canPrev = $derived(currentPage > 1);
	const canNext = $derived(currentPage < totalPages);

	import { onMount } from 'svelte';
	onMount(() => {
		if (!enablePersist) return;
		const snapshot = extractPersistedPreferences(prefs?.current, getDefaultLimit());

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
					pagination: { page: 1, limit: requestOptions?.pagination?.limit ?? getDefaultLimit() }
				};
				shouldRefresh = true;
			}
		} else if (requestOptions?.filters && Object.keys(requestOptions.filters).length > 0) {
			requestOptions = {
				...requestOptions,
				filters: undefined,
				pagination: { page: 1, limit: requestOptions?.pagination?.limit ?? getDefaultLimit() }
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
					pagination: { page: 1, limit: requestOptions?.pagination?.limit ?? getDefaultLimit() }
				};
				shouldRefresh = true;
			}
		} else if (currentSearch) {
			requestOptions = {
				...requestOptions,
				search: undefined,
				pagination: { page: 1, limit: requestOptions?.pagination?.limit ?? getDefaultLimit() }
			};
			shouldRefresh = true;
		}

		const persistedLimit = snapshot.limit ?? getDefaultLimit();
		const currentLimit = requestOptions?.pagination?.limit ?? getDefaultLimit();
		if (persistedLimit !== currentLimit) {
			requestOptions = { ...requestOptions, pagination: { page: 1, limit: persistedLimit } };
			shouldRefresh = true;
		}
		if (shouldRefresh) onRefresh(requestOptions);

		const initialMobileVisibility = buildInitialMobileVisibility(mobileFields, mobileFieldVisibility, snapshot.mobileHidden);
		if (initialMobileVisibility) {
			mobileFieldVisibility = initialMobileVisibility;
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
				header: ({ column }) => {
					if (spec.header) return renderSnippet(spec.header, { column, title: spec.title, class: spec.class });
					if (spec.sortable) return renderSnippet(ColumnHeader, { column, title: spec.title, class: spec.class });
					return renderSnippet(PlainHeader, { title: spec.title, class: spec.class });
				},
				cell: ({ row, getValue }) => {
					const item = row.original as TData;
					const value = accessorKey ? row.getValue(accessorKey) : getValue?.();
					if (spec.cell) return renderSnippet(spec.cell, { row, item, value });
					return renderSnippet(TextCell, { value });
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

	const columnsDef = $derived(buildColumns(columns, selectionDisabled));

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
			if (first) {
				requestOptions = {
					...requestOptions,
					sort: { column: String(first.id), direction: first.desc ? 'desc' : 'asc' },
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
			prefs.current = { ...prefs.current, m: encodeMobileHidden(mobileFieldVisibility) };
		}
	}

	const mobileFieldsForOptions = $derived(
		mobileFields.map((field) => ({
			id: field.id,
			label: field.label,
			visible: mobileFieldVisibility[field.id] ?? true
		}))
	);

	$effect(() => {
		const s = requestOptions?.sort;
		if (!s) {
			if (sorting.length) sorting = [];
			return;
		}
		const desc = s.direction === 'desc';
		if (!sorting[0] || sorting[0].id !== s.column || sorting[0].desc !== desc) {
			sorting = [{ id: s.column, desc }];
		}
	});

	$effect(() => {
		if (!enablePersist || !prefs) return;
		prefs.current = { ...prefs.current, c: customSettings };
	});
</script>

{#snippet TextCell({ value }: { value: unknown })}
	<span class="max-w-[500px] truncate">{value ?? ''}</span>
{/snippet}

{#snippet PlainHeader({ title, class: className, ...restProps }: PlainHeaderProps)}
	<div class={className} {...restProps}>{title}</div>
{/snippet}

{#snippet Pagination({ table }: PaginationProps)}
	<div class="flex w-full flex-col gap-4 px-2 sm:flex-row sm:items-center sm:justify-between">
		<div class="text-muted-foreground order-2 text-sm sm:order-1">
			{m.common_showing_of_total({ shown: items.data.length, total: totalItems })}
		</div>
		<div class="order-1 flex flex-col gap-4 sm:order-2 sm:flex-row sm:items-center sm:space-x-6 lg:space-x-8">
			<div class="flex items-center justify-between space-x-2 sm:justify-start">
				<p class="text-sm font-medium">{m.common_rows_per_page()}</p>
				<Select.Root
					allowDeselect={false}
					type="single"
					value={`${pageSize}`}
					onValueChange={(value) => setPageSize(Number(value))}
				>
					<Select.Trigger class="h-8 w-[70px]">
						{String(pageSize)}
					</Select.Trigger>
					<Select.Content side="top">
						{#each [10, 20, 30, 40, 50] as size (size)}
							<Select.Item value={`${size}`}>
								{size}
							</Select.Item>
						{/each}
					</Select.Content>
				</Select.Root>
			</div>
			<div class="flex items-center justify-between sm:justify-center">
				<div class="flex items-center justify-center text-sm font-medium sm:w-[100px]">
					{m.common_page_of({ page: currentPage, total: totalPages })}
				</div>
				<div class="flex items-center space-x-1 sm:space-x-2">
					<Button variant="outline" class="hidden size-8 p-0 lg:flex" onclick={() => setPage(1)} disabled={!canPrev}>
						<span class="sr-only">{m.common_go_first_page()}</span>
						<ChevronsLeftIcon />
					</Button>
					<Button variant="outline" class="size-8 p-0" onclick={() => setPage(currentPage - 1)} disabled={!canPrev}>
						<span class="sr-only">{m.common_go_prev_page()}</span>
						<ChevronLeftIcon />
					</Button>
					<Button variant="outline" class="size-8 p-0" onclick={() => setPage(currentPage + 1)} disabled={!canNext}>
						<span class="sr-only">{m.common_go_next_page()}</span>
						<ChevronRightIcon />
					</Button>
					<Button variant="outline" class="hidden size-8 p-0 lg:flex" onclick={() => setPage(totalPages)} disabled={!canNext}>
						<span class="sr-only">{m.common_go_last_page()}</span>
						<ChevronsRightIcon />
					</Button>
				</div>
			</div>
		</div>
	</div>
{/snippet}

{#snippet MobileCard({ row, item }: MobileCardProps)}
	{@render mobileCard({ row, item, mobileFieldVisibility })}
{/snippet}

{#snippet ColumnHeader({ column, title, class: className, ...restProps }: ColumnHeaderProps)}
	{#if !column?.getCanSort()}
		<div class={className} {...restProps}>
			{title}
		</div>
	{:else}
		<div class={cn('flex items-center', className)} {...restProps}>
			<DropdownMenu.Root>
				<DropdownMenu.Trigger>
					{#snippet child({ props })}
						<Button {...props} variant="ghost" size="sm" class="data-[state=open]:bg-accent -ml-3 h-8">
							<span>
								{title}
							</span>
							{#if column.getIsSorted() === 'desc'}
								<ArrowDownIcon />
							{:else if column.getIsSorted() === 'asc'}
								<ArrowUpIcon />
							{:else}
								<ChevronsUpDownIcon />
							{/if}
						</Button>
					{/snippet}
				</DropdownMenu.Trigger>
				<DropdownMenu.Content align="start">
					<DropdownMenu.Item onclick={() => column.toggleSorting(false)}>
						<ArrowUpIcon class="text-muted-foreground/70 mr-2 size-3.5" />
						{m.common_sort_asc()}
					</DropdownMenu.Item>
					<DropdownMenu.Item onclick={() => column.toggleSorting(true)}>
						<ArrowDownIcon class="text-muted-foreground/70 mr-2 size-3.5" />
						{m.common_sort_desc()}
					</DropdownMenu.Item>
					<DropdownMenu.Separator />
					<DropdownMenu.Item onclick={() => column.toggleVisibility(false)}>
						<EyeOffIcon class="text-muted-foreground/70 mr-2 size-3.5" />
						{m.common_hide()}
					</DropdownMenu.Item>
				</DropdownMenu.Content>
			</DropdownMenu.Root>
		</div>
	{/if}
{/snippet}

{#if customTableView}
	{@render customTableView({ table })}
{:else if unstyled}
	<div class="flex h-full min-h-0 flex-col">
		{#if !withoutSearch}
			<div class="shrink-0 border-b">
				<DataTableToolbar
					{table}
					{selectedIds}
					{selectionDisabled}
					{onRemoveSelected}
					mobileFields={mobileFieldsForOptions}
					{onToggleMobileField}
					{customViewOptions}
				/>
			</div>
		{/if}

		<div class="hidden h-full min-h-0 flex-1 overflow-auto md:block">
			<div class="h-full w-full">
				<Table.Root class="relative">
					<Table.Header>
						{#each table.getHeaderGroups() as headerGroup (headerGroup.id)}
							<Table.Row>
								{#each headerGroup.headers as header (header.id)}
									<Table.Head colspan={header.colSpan}>
										{#if !header.isPlaceholder}
											<FlexRender content={header.column.columnDef.header} context={header.getContext()} />
										{/if}
									</Table.Head>
								{/each}
							</Table.Row>
						{/each}
					</Table.Header>
					<Table.Body>
						{#each table.getRowModel().rows as row (row.id)}
							<Table.Row data-state={(selectedIds ?? []).includes((row.original as TData).id) && 'selected'}>
								{#each row.getVisibleCells() as cell (cell.id)}
									<Table.Cell>
										<FlexRender content={cell.column.columnDef.cell} context={cell.getContext()} />
									</Table.Cell>
								{/each}
							</Table.Row>
						{:else}
							<Table.Row>
								<Table.Cell colspan={columnsDef.length} class="h-48">
									<Empty.Root class="border border-dashed">
										<Empty.Header>
											<Empty.Media variant="icon">
												<FolderXIcon />
											</Empty.Media>
											<Empty.Title>{m.common_no_results_found()}</Empty.Title>
										</Empty.Header>
									</Empty.Root>
								</Table.Cell>
							</Table.Row>
						{/each}
					</Table.Body>
				</Table.Root>
			</div>
		</div>

		<!-- Mobile Card View -->
		<div class="block flex-1 overflow-auto md:hidden">
			<div class="divide-border/40 divide-y">
				{#each table.getRowModel().rows as row (row.id)}
					{@render MobileCard({ row, item: row.original as TData })}
				{:else}
					<Empty.Root class="min-h-48 border-0">
						<Empty.Header>
							<Empty.Media variant="icon">
								<FolderXIcon />
							</Empty.Media>
							<Empty.Title>{m.common_no_results_found()}</Empty.Title>
						</Empty.Header>
					</Empty.Root>
				{/each}
			</div>
		</div>

		{#if !withoutPagination}
			<div class="shrink-0 border-t px-2 py-4">
				{@render Pagination({ table })}
			</div>
		{/if}
	</div>
{:else}
	<Card.Root class="overflow-hiddens flex h-full min-h-0 flex-col">
		{#snippet children()}
			{#if !withoutSearch}
				<Card.Header class="border-b">
					<DataTableToolbar
						{table}
						{selectedIds}
						{selectionDisabled}
						{onRemoveSelected}
						mobileFields={mobileFieldsForOptions}
						{onToggleMobileField}
						{customViewOptions}
					/>
				</Card.Header>
			{/if}

			<Card.Content class="hidden h-full min-h-0 flex-1 overflow-auto p-0 md:block">
				<Table.Root class="relative">
					<Table.Header>
						{#each table.getHeaderGroups() as headerGroup (headerGroup.id)}
							<Table.Row>
								{#each headerGroup.headers as header (header.id)}
									<Table.Head colspan={header.colSpan}>
										{#if !header.isPlaceholder}
											<FlexRender content={header.column.columnDef.header} context={header.getContext()} />
										{/if}
									</Table.Head>
								{/each}
							</Table.Row>
						{/each}
					</Table.Header>
					<Table.Body>
						{#each table.getRowModel().rows as row (row.id)}
							<Table.Row data-state={(selectedIds ?? []).includes((row.original as TData).id) && 'selected'}>
								{#each row.getVisibleCells() as cell (cell.id)}
									<Table.Cell>
										<FlexRender content={cell.column.columnDef.cell} context={cell.getContext()} />
									</Table.Cell>
								{/each}
							</Table.Row>
						{:else}
							<Table.Row>
								<Table.Cell colspan={columnsDef.length} class="h-48">
									<Empty.Root class="border border-dashed">
										<Empty.Header>
											<Empty.Media variant="icon">
												<FolderXIcon />
											</Empty.Media>
											<Empty.Title>{m.common_no_results_found()}</Empty.Title>
										</Empty.Header>
									</Empty.Root>
								</Table.Cell>
							</Table.Row>
						{/each}
					</Table.Body>
				</Table.Root>
			</Card.Content>

			<!-- Mobile Card View -->
			<Card.Content class="block flex-1 overflow-auto p-0 md:hidden">
				{#each table.getRowModel().rows as row (row.id)}
					{@render MobileCard({ row, item: row.original as TData })}
				{:else}
					<Empty.Root class="min-h-48 border-0">
						<Empty.Header>
							<Empty.Media variant="icon">
								<FolderXIcon />
							</Empty.Media>
							<Empty.Title>{m.common_no_results_found()}</Empty.Title>
						</Empty.Header>
					</Empty.Root>
				{/each}
			</Card.Content>

			{#if !withoutPagination}
				<Card.Footer class="shrink-0 border-t px-2 py-4">
					{@render Pagination({ table })}
				</Card.Footer>
			{/if}
		{/snippet}
	</Card.Root>
{/if}
