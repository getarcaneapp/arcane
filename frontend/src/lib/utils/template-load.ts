import { queryKeys } from '$lib/query/query-keys';
import { templateService } from '$lib/services/template-service';
import type { Variable } from '$lib/types/shared';

type QueryClientLike = {
	fetchQuery: <T>(options: { queryKey: unknown; queryFn: () => Promise<T> }) => Promise<T>;
};

type ParentWithQueryClient = () => Promise<{
	queryClient: unknown;
	[key: string]: unknown;
}>;

export function globalVariablesToMap(globalVariables: Variable[] | null | undefined): Record<string, string> {
	return Object.fromEntries((globalVariables ?? []).map((item) => [item.key, item.value]));
}

export async function loadTemplateAuthoringData(parent: ParentWithQueryClient) {
	const { queryClient } = await parent();
	const client = queryClient as QueryClientLike;

	const [defaultTemplates, templates, globalVariables] = await Promise.all([
		client
			.fetchQuery({
				queryKey: queryKeys.templates.defaults(),
				queryFn: () => templateService.getDefaultTemplates()
			})
			.catch((err) => {
				console.warn('Failed to load default templates:', err);
				return { composeTemplate: '', envTemplate: '' };
			}),
		client
			.fetchQuery({
				queryKey: queryKeys.templates.allTemplates(),
				queryFn: () => templateService.getAllTemplates()
			})
			.catch((err) => {
				console.warn('Failed to load templates:', err);
				return [];
			}),
		client
			.fetchQuery({
				queryKey: queryKeys.templates.globalVariables(),
				queryFn: () => templateService.getGlobalVariables()
			})
			.catch((err) => {
				console.warn('Failed to load global variables:', err);
				return [];
			})
	]);

	return { defaultTemplates, templates, globalVariables };
}

export async function loadTemplateContent(client: QueryClientLike, templateId: string) {
	return client
		.fetchQuery({
			queryKey: queryKeys.templates.content(templateId),
			queryFn: () => templateService.getTemplateContent(templateId)
		})
		.catch((err) => {
			console.warn('Failed to load selected template:', err);
			return null;
		});
}
