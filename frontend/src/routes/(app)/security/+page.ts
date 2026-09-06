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

	const requestForApi = mapVulnerabilityRequest(vulnerabilityRequestOptions);

	let summary;
	let vulnerabilities;
	try {
		[summary, vulnerabilities] = await Promise.all([
			queryClient.fetchQuery({
				queryKey: queryKeys.vulnerabilities.summaryByEnvironment(envId),
				queryFn: () => vulnerabilityService.getEnvironmentSummaryForEnvironment(envId)
			}),
			queryClient.fetchQuery({
				queryKey: queryKeys.vulnerabilities.allByEnvironment(envId, requestForApi),
				queryFn: () => vulnerabilityService.getAllVulnerabilitiesForEnvironment(envId, requestForApi)
			})
		]);
	} catch (err) {
		throwPageLoadError(err, 'Failed to load security data');
	}

	return {
		summary,
		vulnerabilities: mapVulnerabilityPage(vulnerabilities, vulnerabilityRequestOptions),
		vulnerabilityRequestOptions,
		patchRequestOptions
	};
};
