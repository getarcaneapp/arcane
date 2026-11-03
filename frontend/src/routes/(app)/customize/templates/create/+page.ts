import type { GlobalVariable } from '#lib/types/env.js';
import { loadGlobalVariablesOrEmpty } from '#lib/utils/template-load.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }): Promise<{ globalVariables: GlobalVariable[] }> => {
	const { queryClient } = await parent();

	const globalVariables = await loadGlobalVariablesOrEmpty(queryClient);

	return { globalVariables };
};
