import {
	s3DestinationAllQuery,
	systemBackupHistoryQuery,
	systemBackupPoliciesQuery,
	systemVolumeBackupConfigQuery
} from '#lib/query/backups.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();
	const requestOptions = resolveInitialTableRequest('arcane-system-backups-table', {
		pagination: { page: 1, limit: 20 },
		sort: { column: 'createdAt', direction: 'desc' }
	} satisfies SearchPaginationSortRequest);
	const [backups, policyCollection, destinations, systemVolumePolicyCollection] = await Promise.all([
		queryClient.query(systemBackupHistoryQuery(requestOptions)),
		queryClient.query(systemBackupPoliciesQuery()),
		queryClient.query(s3DestinationAllQuery()),
		queryClient.query(systemVolumeBackupConfigQuery())
	]);
	return { backups, policyCollection, destinations, systemVolumePolicyCollection, requestOptions };
};
