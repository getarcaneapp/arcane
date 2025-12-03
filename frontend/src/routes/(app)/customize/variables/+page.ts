import type { PageLoad } from './$types';
import { templateService } from '$lib/services/template-service';

export const load: PageLoad = async () => {
	const variables = await templateService.getGlobalVariables();
	return {
		variables
	};
};
