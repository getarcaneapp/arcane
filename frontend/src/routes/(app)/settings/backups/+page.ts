import type { PageLoad } from './$types';
import { systemBackupService } from '#lib/services/system-backup-service.js';
import { s3DestinationService } from '#lib/services/s3-destination-service.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';

export const load: PageLoad = async () => {
	const requestOptions = resolveInitialTableRequest('arcane-system-backups-table', {
		pagination: { page: 1, limit: 20 },
		sort: { column: 'createdAt', direction: 'desc' }
	} satisfies SearchPaginationSortRequest);
	const [backups, policyCollection, destinations, systemVolumePolicyCollection] = await Promise.all([
		systemBackupService.listHistory(requestOptions),
		systemBackupService.getPolicies(),
		s3DestinationService.listAll(),
		systemBackupService.getSystemVolumeConfig()
	]);
	return { backups, policyCollection, destinations, systemVolumePolicyCollection, requestOptions };
};
