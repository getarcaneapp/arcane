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
	mapVulnerabilityPage,
	mapVulnerabilityRequest,
	vulnerabilityEnvironments,
	vulnerabilityOverviewEnvironment
} from '#lib/utils/vulnerability.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();
	const envId = await environmentStore.getCurrentEnvironmentId();
	const shown = await environmentStore.getShownEnvironments();

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

	await Promise.all([envId, ...shown.map((env) => env.id)].map((id) => featureStore.load(id)));
	const overviewEnvironment = vulnerabilityOverviewEnvironment();
	if (!overviewEnvironment) {
		return {
			envId,
			featureDisabled: true,
			overview: null,
			vulnerabilities: { data: [], pagination: { totalPages: 0, totalItems: 0, currentPage: 1, itemsPerPage: 20 } },
			vulnerabilityRequestOptions,
			patchRequestOptions
		};
	}

	const requestForApi = mapVulnerabilityRequest(vulnerabilityRequestOptions);

	let overview;
	let vulnerabilities;
	const operationResult = await tryCatch(
		(async () =>
			Promise.all([
				queryClient.query({
					queryKey: queryKeys.vulnerabilities.overviewByEnvironment(overviewEnvironment.id),
					queryFn: () => vulnerabilityService.getRiskOverviewForEnvironment(overviewEnvironment.id)
				}),
				queryClient.query({
					queryKey: queryKeys.vulnerabilities.allByEnvironment(environmentStore.shownKey, requestForApi),
					queryFn: () =>
						fetchAcrossEnvironments(vulnerabilityEnvironments(), requestForApi, (id, request, config) =>
							vulnerabilityService.getAllVulnerabilitiesForEnvironment(id, request, config)
						)
				})
			]))()
	);
	if (operationResult.error !== null) {
		const err = operationResult.error;

		throwPageLoadError(err, 'Failed to load security data');
	} else {
		[overview, vulnerabilities] = operationResult.data;
	}

	return {
		envId,
		featureDisabled: false,
		overview,
		vulnerabilities: mapVulnerabilityPage(vulnerabilities, vulnerabilityRequestOptions),
		vulnerabilityRequestOptions,
		patchRequestOptions
	};
};
