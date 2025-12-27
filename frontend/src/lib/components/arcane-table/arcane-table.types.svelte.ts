import type { Row, Column, FilterFn, ColumnFiltersState, VisibilityState } from '@tanstack/table-core';
import type { Snippet } from 'svelte';

export type FieldSpec = {
	id: string;
	label: string;
	defaultVisible?: boolean;
};

export type MobileFieldVisibility = Record<string, boolean>;

export type ColumnSpec<T> = {
	accessorKey?: keyof T & string;
	accessorFn?: (row: T) => any;
	id?: string;
	title: string;
	hidden?: boolean;
	sortable?: boolean;
	cell?: Snippet<[{ row: Row<T>; item: T; value: unknown }]>;
	header?: Snippet<[{ column: Column<T>; title: string; class?: string }]>;
	class?: string;
	filterFn?: FilterFn<T>;
};

// Compact persisted prefs to reduce JSON size
export type CompactTablePrefs = {
	// v: list of hidden column ids (only the exceptions)
	v?: string[];
	// f: filters as [id, value] tuples
	f?: [string, unknown][];
	// g: global filter string
	g?: string;
	// l: page size (limit)
	l?: number;
	// m: list of hidden mobile field ids
	m?: string[];
	c?: Record<string, unknown>;
};

export function encodeHidden(visibility: VisibilityState): string[] {
	const hidden: string[] = [];
	for (const [id, visible] of Object.entries(visibility)) {
		if (visible === false) hidden.push(id);
	}
	return hidden;
}

export function applyHiddenPatch(target: VisibilityState, hidden?: string[]) {
	if (!hidden?.length) return;
	for (const id of hidden) {
		target[id] = false;
	}
}

export function encodeFilters(filters: ColumnFiltersState): [string, unknown][] {
	return (filters ?? []).map((f) => [f.id, (f as any).value] as [string, unknown]);
}

export function decodeFilters(pairs?: [string, unknown][]): ColumnFiltersState {
	if (!pairs?.length) return [];
	return pairs.map(([id, value]) => ({ id, value }));
}

export function encodeMobileVisibility(visibility: Record<string, boolean>): string[] {
	const encoded: string[] = [];
	for (const [id, visible] of Object.entries(visibility)) {
		encoded.push(visible ? id : `-${id}`);
	}
	return encoded;
}

export function decodeMobileVisibility(encoded?: string[]): Record<string, boolean> {
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
