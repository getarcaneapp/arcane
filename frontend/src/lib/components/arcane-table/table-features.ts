import {
	columnFilteringFeature,
	columnVisibilityFeature,
	globalFilteringFeature,
	rowSelectionFeature,
	rowSortingFeature,
	tableFeatures,
	type SvelteTable
} from '@tanstack/svelte-table';
import { metaHelper } from '@tanstack/table-core';
import type { Cell, Column, ColumnDef, FilterFn, Row, RowData, Table } from '@tanstack/table-core';

// Type-only cycle with arcane-table.types.svelte.ts (which imports only types from here) — erased at emit.
import type { IconType } from '#lib/icons/index.js';

import type { CardSlot, ColumnAlign, ColumnWidth, FilterOption } from './arcane-table.types.svelte';

export interface ArcaneColumnMeta {
	title?: string;
	filterOptions?: FilterOption[];
	width?: ColumnWidth;
	align?: ColumnAlign;
	truncate?: boolean;
	card?: CardSlot;
	cardIcon?: IconType;
	cardLabel?: boolean;
}

export const arcaneTableFeatures = tableFeatures({
	rowSelectionFeature,
	columnVisibilityFeature,
	rowSortingFeature,
	columnFilteringFeature,
	globalFilteringFeature,
	columnMeta: metaHelper<ArcaneColumnMeta>()
});

export type ArcaneFeatures = typeof arcaneTableFeatures;

export type ArcaneColumnDef<T extends RowData> = ColumnDef<ArcaneFeatures, T>;
export type ArcaneRow<T extends RowData> = Row<ArcaneFeatures, T>;
export type ArcaneColumn<T extends RowData> = Column<ArcaneFeatures, T>;
export type ArcaneCell<T extends RowData> = Cell<ArcaneFeatures, T>;
export type ArcaneTable<T extends RowData> = Table<ArcaneFeatures, T>;
export type ArcaneSvelteTable<T extends RowData> = SvelteTable<ArcaneFeatures, T>;
export type ArcaneFilterFn<T extends RowData> = FilterFn<ArcaneFeatures, T>;
