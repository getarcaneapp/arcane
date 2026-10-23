import { queryKeys } from '#lib/query/query-keys.js';
import { vulnerabilityService } from '#lib/services/vulnerability-service.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import { featureStore } from '#lib/stores/features.store.svelte.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { fetchAcrossEnvironments } from '#lib/utils/environments.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';
import { tryCatch } from '#lib/utils/try-catch.js';
import {
	fetchRiskOverview,
	mapVulnerabilityPage,
	mapVulnerabilityRequest,
	vulnerabilityEnvironments,
	vulnerabilityOverviewEnvironments,
	withVulnerabilityToggles
} from '#lib/utils/vulnerability.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();
	await environmentStore.ready;

	const vulnerabilityRequestOptions = resolveInitialTableRequest(
		'arcane-security-vuln-table',
		{
			pagination: {
				page: 1,
				limit: 20
			},
			sort: {
				column: 'vulnSeverity',
				direction: 'desc'
			}
		} satisfies SearchPaginationSortRequest,
		{ restoreAllPageSize: false }
	);

	const patchRequestOptions = resolveInitialTableRequest('arcane-security-patch-table', {
		pagination: {
			page: 1,
			limit: 20
		},
		sort: {
			column: 'scanTime',
			direction: 'desc'
		}
	} satisfies SearchPaginationSortRequest);

	await Promise.all(environmentStore.shown.map((env) => featureStore.load(env.id)));
	if (vulnerabilityOverviewEnvironments().length === 0) {
		return {
			featureDisabled: true,
			overview: null,
			vulnerabilityRequestOptions,
			patchRequestOptions
		};
	}

	const requestForApi = mapVulnerabilityRequest(
		withVulnerabilityToggles(vulnerabilityRequestOptions, { showIgnored: false, fixAvailable: false })
	);

	let overview;
	const operationResult = await tryCatch(
		(async () =>
			Promise.all([
				queryClient.query({
					queryKey: queryKeys.vulnerabilities.overviewByEnvironment(environmentStore.shownKey),
					queryFn: fetchRiskOverview
				}),
				queryClient.query({
					queryKey: queryKeys.vulnerabilities.allByEnvironment(environmentStore.shownKey, requestForApi),
					queryFn: async () =>
						mapVulnerabilityPage(
							await fetchAcrossEnvironments(vulnerabilityEnvironments(), requestForApi, (id, request, config) =>
								vulnerabilityService.getAllVulnerabilitiesForEnvironment(id, request, config)
							),
							requestForApi
						)
				})
			]))()
	);
	if (operationResult.error !== null) {
		const err = operationResult.error;

		throwPageLoadError(err, 'Failed to load security data');
	} else {
		[overview] = operationResult.data;
	}

	return {
		featureDisabled: false,
		overview,
		vulnerabilityRequestOptions,
		patchRequestOptions
	};
};
