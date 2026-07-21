import type { PageLoad } from './$types';
import { systemBackupService } from '$lib/services/system-backup-service';
import { s3DestinationService } from '$lib/services/s3-destination-service';
import { resolveInitialTableRequest } from '$lib/utils/tables';
import type { SearchPaginationSortRequest } from '$lib/types/shared';

export const load: PageLoad = async () => {
	const requestOptions = resolveInitialTableRequest('arcane-system-backups-table', {
		pagination: { page: 1, limit: 20 },
		sort: { column: 'createdAt', direction: 'desc' }
	} satisfies SearchPaginationSortRequest);
	const [backups, policyCollection, destinations] = await Promise.all([
		systemBackupService.list(requestOptions),
		systemBackupService.getPolicies(),
		s3DestinationService.listAll()
	]);
	return { backups, policyCollection, destinations, requestOptions };
};
