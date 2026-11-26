import { queryOptions } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { groupService } from '#lib/services/group-service.js';
import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';

export function groupListQuery(requestOptions: SearchPaginationSortRequest) {
	return queryOptions({
		queryKey: queryKeys.groups.list(requestOptions),
		queryFn: () => groupService.getGroups(requestOptions)
	});
}

export function groupDetailQuery(groupId: string) {
	return queryOptions({
		queryKey: queryKeys.groups.detail(groupId),
		queryFn: () => groupService.get(groupId)
	});
}

export function groupMembersQuery(groupId: string) {
	return queryOptions({
		queryKey: queryKeys.groups.members(groupId),
		queryFn: () => groupService.getMembers(groupId)
	});
}

export function groupRoleAssignmentsQuery(groupId: string) {
	return queryOptions({
		queryKey: queryKeys.groups.roles(groupId),
		queryFn: () => groupService.getRoleAssignments(groupId)
	});
}

export function groupIdpMappingsQuery(groupId?: string) {
	return queryOptions({
		queryKey: queryKeys.groups.idpMappings(groupId),
		queryFn: () => groupService.getIdpMappings(groupId)
	});
}
