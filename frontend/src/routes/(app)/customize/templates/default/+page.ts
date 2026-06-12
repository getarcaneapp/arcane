import type { Template } from '$lib/types/swarm';
import type { Variable } from '$lib/types/shared';
import { loadTemplateAuthoringData } from '$lib/utils/template-load';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({
	parent
}): Promise<{ composeTemplate: string; envTemplate: string; templates: Template[]; globalVariables: Variable[] }> => {
	const { defaultTemplates, templates, globalVariables } = await loadTemplateAuthoringData(parent);

	return {
		composeTemplate: defaultTemplates.composeTemplate,
		envTemplate: defaultTemplates.envTemplate,
		templates,
		globalVariables
	};
};
