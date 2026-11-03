import {
	s3DestinationAllQuery,
	systemBackupHistoryQuery,
	systemBackupPoliciesQuery,
	systemVolumeBackupConfigQuery
} from '#lib/query/backups.js';
import { GLOBAL_SCOPE } from '#lib/types/auth.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { userHasPermission } from '#lib/utils/auth.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient, user } = await parent();
	const canReadBackups = userHasPermission(user, 'system-backups:read', GLOBAL_SCOPE);
	const requestOptions = resolveInitialTableRequest('arcane-system-backups-table', {
		pagination: { page: 1, limit: 20 },
		sort: { column: 'createdAt', direction: 'desc' }
	} satisfies SearchPaginationSortRequest);
	const [backups, policyCollection, destinations, systemVolumePolicyCollection] = await Promise.all([
		queryClient.query(systemBackupHistoryQuery(requestOptions)),
		canReadBackups ? queryClient.query(systemBackupPoliciesQuery()) : { policies: [], recoveryKeyStored: false },
		userHasPermission(user, 's3-destinations:list', GLOBAL_SCOPE) ? queryClient.query(s3DestinationAllQuery()) : [],
		canReadBackups ? queryClient.query(systemVolumeBackupConfigQuery()) : { policies: [] }
	]);
	return { backups, policyCollection, destinations, systemVolumePolicyCollection, requestOptions };
};
