import type { PageLoad } from './$types';
import { templateService } from '$lib/services/template-service';
import { queryKeys } from '$lib/query/query-keys';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();

	const variables = await queryClient.fetchQuery({
		queryKey: queryKeys.templates.globalVariables(),
		queryFn: () => templateService.getGlobalVariables()
	});

	return {
		variables
	};
};
