import { queryKeys } from '#lib/query/query-keys.js';
import { auditService } from '#lib/services/audit-service.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();

	const auditRequestOptions = resolveInitialTableRequest('arcane-audit-table', {
		pagination: {
			page: 1,
			limit: 20
		},
		sort: {
			column: 'createdAt',
			direction: 'desc'
		}
	} satisfies SearchPaginationSortRequest);

	const result = await tryCatch(
		queryClient.query({
			queryKey: queryKeys.audit.list(auditRequestOptions),
			queryFn: () => auditService.getLogs(auditRequestOptions)
		})
	);
	if (result.error !== null) {
		throwPageLoadError(result.error, 'Failed to load audit log');
	}

	return { logs: result.data, auditRequestOptions };
};
