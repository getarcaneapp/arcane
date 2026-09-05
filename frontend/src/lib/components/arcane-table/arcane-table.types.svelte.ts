import type { ColumnFiltersState, ColumnVisibilityState, RowData } from '@tanstack/table-core';
import type { ArcaneColumn, ArcaneFilterFn, ArcaneRow, ArcaneSvelteTable } from './table-features';
import type { Snippet } from 'svelte';
import type { Component } from 'svelte';

export interface FilterOption {
	value: string | boolean;
	label: string;
	icon?: Component;
	dotClass?: string;
}

export type FieldSpec = {
	id: string;
	label: string;
	defaultVisible?: boolean;
};

export type MobileFieldVisibility = Record<string, boolean>;
export type SortDirection = 'asc' | 'desc';
export type SortPreference = [string, SortDirection];
export type SortState = { column: string; direction: SortDirection };

export type ColumnWidth = 'auto' | 'min' | 'max' | number;
export type ColumnAlign = 'left' | 'center' | 'right';

export type ColumnSpec<T extends RowData> = {
	accessorKey?: keyof T & string;
	accessorFn?: (row: T) => unknown;
	id?: string;
	title: string;
	hidden?: boolean;
	sortable?: boolean;
	/**
	 * Sort the current page locally via the column accessor instead of asking the
	 * server. For values the server cannot sort (e.g. live WebSocket stats).
	 */
	clientSort?: boolean;
	cell?: Snippet<[{ row: ArcaneRow<T>; item: T; value: unknown }]>;
	/** Render a shared value-only Svelte component without a per-table wrapper snippet. */
	cellComponent?: Component<{ value: unknown }>;
	header?: Snippet<[{ column: ArcaneColumn<T>; title: string; class?: string }]>;
	class?: string;
	filterFn?: ArcaneFilterFn<T>;
	filterOptions?: FilterOption[];
	width?: ColumnWidth;
	align?: ColumnAlign;
	truncate?: boolean;
};

// Compact persisted prefs to reduce JSON size
export type CompactTablePrefs = {
	// v: list of hidden column ids (only the exceptions)
	v?: string[];
	// f: filters as [id, value] tuples
	f?: [string, unknown][];
	// g: global filter string
	g?: string;
	// s: sort as [column, direction]
	s?: SortPreference;
	// l: page size (limit)
	l?: number;
	// m: list of hidden mobile field ids
	m?: string[];
	c?: Record<string, unknown>;
};

export function encodeHidden(visibility: ColumnVisibilityState): string[] {
	const hidden: string[] = [];
	for (const [id, visible] of Object.entries(visibility)) {
		if (visible === false) hidden.push(id);
	}
	return hidden;
}

export function applyHiddenPatch(target: ColumnVisibilityState, hidden?: string[]) {
	if (!hidden?.length) return;
	for (const id of hidden) {
		target[id] = false;
	}
}

export function encodeFilters(filters: ColumnFiltersState): [string, unknown][] {
	return (filters ?? []).map((f) => [f.id, f.value] as [string, unknown]);
}

export function decodeFilters(pairs?: [string, unknown][]): ColumnFiltersState {
	if (!pairs?.length) return [];
	return pairs.map(([id, value]) => ({ id, value }));
}

export function encodeSort(sort?: SortState): SortPreference | undefined {
	if (!sort?.column) return undefined;
	return [sort.column, sort.direction];
}

export function decodeSort(value: unknown): SortState | undefined {
	if (!Array.isArray(value) || value.length !== 2) return undefined;
	const [column, direction] = value;
	if (typeof column !== 'string') return undefined;
	if (direction !== 'asc' && direction !== 'desc') return undefined;
	return { column, direction };
}

export function encodeMobileVisibility(visibility: Record<string, boolean>): string[] {
	const encoded: string[] = [];
	for (const [id, visible] of Object.entries(visibility)) {
		encoded.push(visible ? id : `-${id}`);
	}
	return encoded;
}

function decodeMobileVisibility(encoded?: string[]): Record<string, boolean> {
	if (!encoded?.length) return {};
	const visibility: Record<string, boolean> = {};
	for (const entry of encoded) {
		if (entry.startsWith('-')) {
			visibility[entry.slice(1)] = false;
		} else {
			visibility[entry] = true;
		}
	}
	return visibility;
}

export function buildMobileVisibility(fields: FieldSpec[], persisted?: string[]): Record<string, boolean> {
	const visibility: Record<string, boolean> = {};
	for (const field of fields) {
		visibility[field.id] = field.defaultVisible ?? true;
	}
	const overrides = decodeMobileVisibility(persisted);
	for (const [id, visible] of Object.entries(overrides)) {
		visibility[id] = visible;
	}
	return visibility;
}

export type BulkAction = {
	id: string;
	label: string;
	action:
		| 'base'
		| 'start'
		| 'stop'
		| 'restart'
		| 'remove'
		| 'deploy'
		| 'redeploy'
		| 'up'
		| 'down'
		| 'prune'
		| 'archive'
		| 'update';
	onClick: (ids: string[]) => void;
	disabled?: boolean;
	disabledReason?: string;
	loading?: boolean;
	icon?: Component;
};

// Grouping types
export type GroupedData<T> = {
	groupName: string;
	items: T[];
};

export type GroupSelectionState = 'none' | 'some' | 'all';

export function shouldIgnoreTableRowClick(event: MouseEvent): boolean {
	const target = event.target as HTMLElement | null;
	return !!target?.closest('a, button, input, [role="checkbox"], [data-slot="checkbox"], [data-row-select-ignore]');
}

export function getTableRowsForItems<T extends RowData & { id?: string }>(
	table: ArcaneSvelteTable<T>,
	groupItems: Array<{ id: string }>
) {
	const groupIds = new Set(groupItems.map((item) => item.id));
	return table.getRowModel().rows.filter((row) => groupIds.has(row.original.id ?? ''));
}

export type TableEmptyState = {
	title: string;
	description: string;
	action?: {
		label: string;
		href?: string;
		onclick?: () => void;
	};
};
