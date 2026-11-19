import { queryOptions } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { templateService } from '#lib/services/template-service.js';
import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';

export function templateListQuery(requestOptions: SearchPaginationSortRequest) {
	return queryOptions({
		queryKey: queryKeys.templates.list(requestOptions),
		queryFn: () => templateService.getTemplates(requestOptions)
	});
}

export function templateRegistriesQuery() {
	return queryOptions({
		queryKey: queryKeys.templates.registries(),
		queryFn: () => templateService.getRegistries()
	});
}

export function templateContentQuery(templateId: string) {
	return queryOptions({
		queryKey: queryKeys.templates.content(templateId),
		queryFn: () => templateService.getTemplateContent(templateId)
	});
}

export function templateAllQuery() {
	return queryOptions({
		queryKey: queryKeys.templates.allTemplates(),
		queryFn: () => templateService.getAllTemplates()
	});
}

export function templateDefaultsQuery() {
	return queryOptions({
		queryKey: queryKeys.templates.defaults(),
		queryFn: () => templateService.getDefaultTemplates()
	});
}
