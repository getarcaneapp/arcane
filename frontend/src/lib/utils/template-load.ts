import { tryCatch } from '#lib/utils/try-catch.js';
import { queryKeys } from '#lib/query/query-keys.js';
import { templateService } from '#lib/services/template-service.js';
import { variableService } from '#lib/services/variable-service.js';
import type { GlobalVariable } from '#lib/types/variable.js';

type QueryClientLike = {
	query: <T>(options: { queryKey: unknown; queryFn: () => Promise<T> }) => Promise<T>;
};

type ParentWithQueryClient = () => Promise<{
	queryClient: unknown;
	[key: string]: unknown;
}>;

export function globalVariablesToMap(globalVariables: GlobalVariable[] | null | undefined): Record<string, string> {
	return Object.fromEntries((globalVariables ?? []).map((item) => [item.key, item.value]));
}

export async function loadTemplateAuthoringData(parent: ParentWithQueryClient) {
	const { queryClient } = await parent();
	const client = queryClient as QueryClientLike;

	const [defaultTemplates, templates, globalVariables] = await Promise.all([
		tryCatch(
			client.query({
				queryKey: queryKeys.templates.defaults(),
				queryFn: () => templateService.getDefaultTemplates()
			})
		).then((result) => {
			if (result.error !== null) {
				const err = result.error;

				console.warn('Failed to load default templates:', err);
				return { composeTemplate: '', envTemplate: '' };
			} else {
				return result.data;
			}
		}),
		tryCatch(
			client.query({
				queryKey: queryKeys.templates.allTemplates(),
				queryFn: () => templateService.getAllTemplates()
			})
		).then((result) => {
			if (result.error !== null) {
				const err = result.error;

				console.warn('Failed to load templates:', err);
				return [];
			} else {
				return result.data;
			}
		}),
		tryCatch(
			client.query({
				queryKey: queryKeys.variables.list(),
				queryFn: () => variableService.list()
			})
		).then((result) => {
			if (result.error !== null) {
				const err = result.error;

				console.warn('Failed to load global variables:', err);
				return [];
			} else {
				return result.data;
			}
		})
	]);

	return { defaultTemplates, templates, globalVariables };
}

export async function loadTemplateContent(client: QueryClientLike, templateId: string) {
	return tryCatch(
		client.query({
			queryKey: queryKeys.templates.content(templateId),
			queryFn: () => templateService.getTemplateContent(templateId)
		})
	).then((result) => {
		if (result.error !== null) {
			const err = result.error;

			console.warn('Failed to load selected template:', err);
			return null;
		} else {
			return result.data;
		}
	});
}
