import type { RowData } from '@tanstack/table-core';

import type { GroupedData } from '#lib/components/arcane-table/arcane-table.types.svelte.js';
import type { ArcaneRow } from '#lib/components/arcane-table/table-features.js';

export type TableDisplayEntry<T extends RowData> =
	| { kind: 'group'; key: string; group: GroupedData<T> }
	| { kind: 'row' | 'detail'; key: string; row: ArcaneRow<T>; grouped: boolean };
