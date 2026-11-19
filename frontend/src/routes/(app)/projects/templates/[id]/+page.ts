import { error } from '@sveltejs/kit';

import { templateAllQuery, templateContentQuery } from '#lib/query/templates.js';
import type { GlobalVariable } from '#lib/types/env.js';
import type { Template, TemplateContent } from '#lib/types/template.js';
import { loadGlobalVariablesOrEmpty } from '#lib/utils/template-load.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({
	params,
	parent
}): Promise<{
	templateData: TemplateContent;
	allTemplates: Template[];
	globalVariables: GlobalVariable[];
}> => {
	const { queryClient } = await parent();

	const templateLoad = await tryCatch(
		Promise.all([
			queryClient.query(templateContentQuery(params.id)),
			queryClient.query(templateAllQuery()),
			loadGlobalVariablesOrEmpty(queryClient)
		])
	);
	if (templateLoad.error !== null) {
		console.error('Failed to load template:', templateLoad.error);
		throw error(404, 'Template not found');
	}
	const [templateData, allTemplates, globalVariables] = templateLoad.data;

	return {
		templateData,
		allTemplates,
		globalVariables
	};
};
