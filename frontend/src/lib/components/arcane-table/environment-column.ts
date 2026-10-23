import { m } from '#lib/paraglide/messages.js';
import userStore from '#lib/stores/user-store.svelte.js';
import { ENVIRONMENT_FILTER } from '#lib/utils/environments.js';

import type { ColumnSpec } from './arcane-table.types.svelte';
import EnvironmentCell from './cells/environment-cell.svelte';

export function environmentColumns<T extends { environmentId?: string }>(): ColumnSpec<T>[] {
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
