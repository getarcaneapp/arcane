import {
	columnFilteringFeature,
	columnVisibilityFeature,
	globalFilteringFeature,
	rowSelectionFeature,
	rowSortingFeature,
	tableFeatures,
	type SvelteTable
} from '@tanstack/svelte-table';
import type { Cell, Column, ColumnDef, FilterFn, Row, RowData, Table } from '@tanstack/table-core';

/**
 * The single, app-wide TanStack Table v9 feature set shared by every Arcane table.
 *
 * Arcane runs all tables in manual / server-side mode — sorting, filtering and
 * pagination all happen on the backend (driven by `requestOptions` + `onRefresh`) — so
 * we register only the *state* features the UI actually drives and deliberately omit
 * every client-side row-model factory (`createSortedRowModel`, `createFilteredRowModel`,
 * `createPaginatedRowModel`, …). v9 tree-shakes the unregistered feature code out of the
 * bundle, which is the headline performance win of this migration. The core row model is
 * automatic, so no `rowModels` entry is needed.
 *
 * Declared once, statically, outside any component (per the `tableFeatures` guidance).
 */
export const arcaneTableFeatures = tableFeatures({
	rowSelectionFeature,
	columnVisibilityFeature,
	rowSortingFeature,
	columnFilteringFeature,
	globalFilteringFeature
});

/**
 * The concrete `TFeatures` for every Arcane table. v9 threads a leading `TFeatures`
 * generic through `ColumnDef`/`Row`/`Column`/`Cell`/`Table`; binding it here lets the
 * single-data-param aliases below absorb it, so the ~30 consumers and the public
 * `ColumnSpec` API never have to spell the feature generic.
 */
export type ArcaneFeatures = typeof arcaneTableFeatures;

// v9 requires `TData extends RowData` (= Record<string, any> | Array<any>). Every Arcane table
// row is an object DTO (which satisfies RowData), so the aliases carry that bound explicitly;
// callers thread it through their own `TData extends Record<string, any>` generic. These bind
// the real `arcaneTableFeatures` so the buildColumns cell/header callbacks infer precise
// contexts. The leaf view components (header/cell/row/table props) instead use fully-`any`
// tanstack types — v9's conditional type shapes only reconcile a deferred `TData` against a
// resolved instantiation when `TFeatures` is also `any`, and those components were already
// loosely typed pre-migration.
export type ArcaneColumnDef<T extends RowData> = ColumnDef<ArcaneFeatures, T>;
export type ArcaneRow<T extends RowData> = Row<ArcaneFeatures, T>;
export type ArcaneColumn<T extends RowData> = Column<ArcaneFeatures, T>;
export type ArcaneCell<T extends RowData> = Cell<ArcaneFeatures, T>;
/** Core table type — used for callback contexts and read-only method access. */
export type ArcaneTable<T extends RowData> = Table<ArcaneFeatures, T>;
/** The Svelte adapter table instance — adds reactive `.state` (read state slices). */
export type ArcaneSvelteTable<T extends RowData> = SvelteTable<ArcaneFeatures, T>;
export type ArcaneFilterFn<T extends RowData> = FilterFn<ArcaneFeatures, T>;
