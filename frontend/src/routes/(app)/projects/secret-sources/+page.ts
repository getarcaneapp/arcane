import { queryKeys } from '#lib/query/query-keys.js';
import { secretSourceService } from '#lib/services/secret-source-service.js';
import { throwPageLoadError } from '#lib/utils/api.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();

	try {
		const sources = await queryClient.query({
			queryKey: queryKeys.secretSources.list(),
			queryFn: () => secretSourceService.list()
		});
		return { sources };
	} catch (err) {
		throwPageLoadError(err, 'Failed to load secret sources');
	}
};
