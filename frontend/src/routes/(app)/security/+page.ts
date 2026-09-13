import { featureStore } from '#lib/stores/features.store.svelte.js';
import { tryCatch } from '#lib/utils/try-catch.js';
import { vulnerabilityService } from '#lib/services/vulnerability-service.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import { queryKeys } from '#lib/query/query-keys.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';
import { mapVulnerabilityPage, mapVulnerabilityRequest } from '#lib/utils/vulnerability.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();
	const envId = await environmentStore.getCurrentEnvironmentId();

	const vulnerabilityRequestOptions = resolveInitialTableRequest('arcane-security-vuln-table', {
		pagination: {
			page: 1,
			limit: 20
		},
		sort: {
			column: 'vulnSeverity',
			direction: 'desc'
		}
	} satisfies SearchPaginationSortRequest);

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

	await featureStore.load(envId);
	if (!featureStore.isEnabled('vulnerabilityManagement', envId)) {
		return {
			envId,
			featureDisabled: true,
			summary: null,
			vulnerabilities: { data: [], pagination: { totalPages: 0, totalItems: 0, currentPage: 1, itemsPerPage: 20 } },
			vulnerabilityRequestOptions,
			patchRequestOptions
		};
	}

	const requestForApi = mapVulnerabilityRequest(vulnerabilityRequestOptions);

	let summary;
	let vulnerabilities;
	const operationResult = await tryCatch(
		(async () =>
			Promise.all([
				queryClient.query({
					queryKey: queryKeys.vulnerabilities.summaryByEnvironment(envId),
					queryFn: () => vulnerabilityService.getEnvironmentSummaryForEnvironment(envId)
				}),
				queryClient.query({
					queryKey: queryKeys.vulnerabilities.allByEnvironment(envId, requestForApi),
					queryFn: () => vulnerabilityService.getAllVulnerabilitiesForEnvironment(envId, requestForApi)
				})
			]))()
	);
	if (operationResult.error !== null) {
		const err = operationResult.error;

		throwPageLoadError(err, 'Failed to load security data');
	} else {
		[summary, vulnerabilities] = operationResult.data;
	}

	return {
		envId,
		featureDisabled: false,
		summary,
		vulnerabilities: mapVulnerabilityPage(vulnerabilities, vulnerabilityRequestOptions),
		vulnerabilityRequestOptions,
		patchRequestOptions
	};
};
