import type { PageLoad } from './$types';
import { systemBackupService } from '#lib/services/system-backup-service';
import { s3DestinationService } from '#lib/services/s3-destination-service';
import { resolveInitialTableRequest } from '#lib/utils/tables';
import type { SearchPaginationSortRequest } from '#lib/types/shared';
import type { SystemVolumeBackupOption } from '#lib/types/system-backup';

export const load: PageLoad = async () => {
	const requestOptions = resolveInitialTableRequest('arcane-system-backups-table', {
		pagination: { page: 1, limit: 20 },
		sort: { column: 'createdAt', direction: 'desc' }
	} satisfies SearchPaginationSortRequest);
	const [backups, policyCollection, destinations, systemVolumeConfig] = await Promise.all([
		systemBackupService.listHistory(requestOptions),
		systemBackupService.getPolicies(),
		s3DestinationService.listAll(),
		systemBackupService.getSystemVolumeConfig()
	]);
	const systemVolumeOptions: SystemVolumeBackupOption[] = [];
	return { backups, policyCollection, destinations, systemVolumeConfig, systemVolumeOptions, requestOptions };
};
