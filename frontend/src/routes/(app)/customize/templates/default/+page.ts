import type { GlobalVariable } from '#lib/types/env.js';
import type { Template } from '#lib/types/template.js';
import { loadTemplateAuthoringData } from '#lib/utils/template-load.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({
	parent
}): Promise<{ composeTemplate: string; envTemplate: string; templates: Template[]; globalVariables: GlobalVariable[] }> => {
	const { defaultTemplates, templates, globalVariables } = await loadTemplateAuthoringData(parent);

	return {
		composeTemplate: defaultTemplates.composeTemplate,
		envTemplate: defaultTemplates.envTemplate,
		templates,
		globalVariables
	};
};
