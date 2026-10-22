import { m } from '#lib/paraglide/messages.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import userStore from '#lib/stores/user-store.svelte.js';
import { ENVIRONMENT_FILTER } from '#lib/utils/environments.js';

import type { ColumnSpec } from './arcane-table.types.svelte';
import EnvironmentCell from './cells/environment-cell.svelte';

/** Environment column for merged lists; empty while one environment is shown. */
export function environmentColumns<T extends { environmentId?: string }>(): ColumnSpec<T>[] {
	if (environmentStore.shown.length < 2) return [];
	// A color dot needs no more room than a short header.
	const dots = userStore.current?.preferences?.environmentLabelStyle === 'dot';
	return [
		{
			id: ENVIRONMENT_FILTER,
			accessorFn: (row) => row.environmentId,
			title: dots ? m.environment_column_short() : m.resource_environment_cap(),
			sortable: true,
			width: 'min',
			cellComponent: EnvironmentCell
		}
	];
}
