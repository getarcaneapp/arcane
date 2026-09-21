import { tryCatch } from '#lib/utils/try-catch.js';
import { queryKeys } from '#lib/query/query-keys.js';
import { templateService } from '#lib/services/template-service.js';
import { variableService } from '#lib/services/variable-service.js';
import { extractApiErrorMessage } from '#lib/utils/api.js';
import { userHasPermission } from '#lib/utils/auth.js';
import type { User } from '#lib/types/auth.js';
import type { Template, TemplateContentData } from '#lib/types/swarm.js';
import type { GlobalVariable } from '#lib/types/variable.js';

type QueryClientLike = {
	query: <T>(options: { queryKey: unknown; queryFn: () => Promise<T> }) => Promise<T>;
};

type ParentWithQueryClient = () => Promise<{
	queryClient: unknown;
	user?: User | null;
	[key: string]: unknown;
}>;

export type TemplateAuthoringResource = 'defaultTemplates' | 'templates' | 'globalVariables';

/** An authorized optional request that failed; permission gaps never produce one. */
export type TemplateAuthoringLoadError = { resource: TemplateAuthoringResource; message: string };

export type TemplateAuthoringPermissions = {
	canReadTemplates: boolean;
	canListTemplates: boolean;
	canReadVariables: boolean;
};

export type SelectedTemplateLoad = {
	content: TemplateContentData | null;
	/** Server error for an explicitly requested template; null when it loaded or was not requested. */
	error: string | null;
	/** True when the template was requested without `templates:read`, so no request was made. */
	forbidden: boolean;
};

export function globalVariablesToMap(globalVariables: GlobalVariable[] | null | undefined): Record<string, string> {
	return Object.fromEntries((globalVariables ?? []).map((item) => [item.key, item.value]));
}

const EMPTY_DEFAULT_TEMPLATES = { composeTemplate: '', envTemplate: '' };

/**
 * Loads compose-authoring resources that live behind global permissions. Each
 * resource is fetched only when the user holds its permission; otherwise an
 * empty value is returned without a request.
 */
export async function loadTemplateAuthoringData(parent: ParentWithQueryClient) {
	const { queryClient, user } = await parent();
	const client = queryClient as QueryClientLike;
	const permissions: TemplateAuthoringPermissions = {
		canReadTemplates: userHasPermission(user, 'templates:read'),
		canListTemplates: userHasPermission(user, 'templates:list'),
		canReadVariables: userHasPermission(user, 'variables:read')
	};
	const loadErrors: TemplateAuthoringLoadError[] = [];

	async function loadOptional<T>(resource: TemplateAuthoringResource, allowed: boolean, fallback: T, request: () => Promise<T>) {
		if (!allowed) return fallback;
		const result = await tryCatch(request());
		if (result.error !== null) {
			loadErrors.push({ resource, message: extractApiErrorMessage(result.error) });
			return fallback;
		}
		return result.data;
	}

	const [defaultTemplates, templates, globalVariables] = await Promise.all([
		loadOptional<{ composeTemplate: string; envTemplate: string }>(
			'defaultTemplates',
			permissions.canReadTemplates,
			EMPTY_DEFAULT_TEMPLATES,
			() =>
				client.query({
					queryKey: queryKeys.templates.defaults(),
					queryFn: () => templateService.getDefaultTemplates()
				})
		),
		loadOptional<Template[]>('templates', permissions.canListTemplates, [], () =>
			client.query({
				queryKey: queryKeys.templates.allTemplates(),
				queryFn: () => templateService.getAllTemplates()
			})
		),
		loadOptional<GlobalVariable[]>('globalVariables', permissions.canReadVariables, [], () =>
			client.query({
				queryKey: queryKeys.variables.list(),
				queryFn: () => variableService.list()
			})
		)
	]);

	return { defaultTemplates, templates, globalVariables, permissions, loadErrors };
}

/** Loads an explicitly requested template; without `templates:read` no request is sent. */
export async function loadTemplateContent(
	client: QueryClientLike,
	templateId: string,
	canRead: boolean
): Promise<SelectedTemplateLoad> {
	if (!canRead) return { content: null, error: null, forbidden: true };
	const result = await tryCatch(
		client.query({
			queryKey: queryKeys.templates.content(templateId),
			queryFn: () => templateService.getTemplateContent(templateId)
		})
	);
	if (result.error !== null) {
		return { content: null, error: extractApiErrorMessage(result.error), forbidden: false };
	}
	return { content: result.data, error: null, forbidden: false };
}
