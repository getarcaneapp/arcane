import { variableService } from '#lib/services/variable-service.js';
import { queryKeys } from '#lib/query/query-keys.js';
import type { GlobalVariable } from '#lib/types/variable.js';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }): Promise<{ globalVariables: GlobalVariable[] }> => {
	const { queryClient } = await parent();

	const globalVariables = await queryClient
		.fetchQuery({
			queryKey: queryKeys.variables.list(),
			queryFn: () => variableService.list()
		})
		.catch(() => [] as GlobalVariable[]);

	return { globalVariables };
};
