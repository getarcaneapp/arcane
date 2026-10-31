import { queryOptions } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { roleService } from '#lib/services/role-service.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';

export function roleListQuery(requestOptions: SearchPaginationSortRequest) {
	return queryOptions({
		queryKey: queryKeys.roles.list(requestOptions),
		queryFn: () => roleService.getRoles(requestOptions)
	});
}

export function roleAllQuery() {
	return queryOptions({
		queryKey: queryKeys.roles.all(),
		queryFn: () => roleService.getAll()
	});
}

export function roleDetailQuery(roleId: string) {
	return queryOptions({
		queryKey: queryKeys.roles.detail(roleId),
		queryFn: () => roleService.get(roleId)
	});
}

export function permissionsManifestQuery() {
	return queryOptions({
		queryKey: queryKeys.roles.permissionsManifest(),
		queryFn: () => roleService.getPermissionsManifest()
	});
}
