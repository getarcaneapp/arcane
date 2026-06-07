import { templateService } from '$lib/services/template-service';
import { queryKeys } from '$lib/query/query-keys';
import type { Variable } from '$lib/types/variable.type';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }): Promise<{ globalVariables: Variable[] }> => {
	const { queryClient } = await parent();

	const globalVariables = await queryClient
		.fetchQuery({
			queryKey: queryKeys.templates.globalVariables(),
			queryFn: () => templateService.getGlobalVariables()
		})
		.catch(() => [] as Variable[]);

	return { globalVariables };
};
