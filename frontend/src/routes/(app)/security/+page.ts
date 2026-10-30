import { vulnerabilityListQuery, vulnerabilityOverviewQuery } from '#lib/query/vulnerabilities.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import { featureStore } from '#lib/stores/features.store.svelte.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';
import { tryCatch } from '#lib/utils/try-catch.js';
import {
	mapVulnerabilityRequest,
	vulnerabilityEnvironments,
	vulnerabilityOverviewEnvironments,
	withVulnerabilityToggles
} from '#lib/utils/vulnerability.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const [{ queryClient }] = await Promise.all([parent(), environmentStore.ready]);
	const scope = environmentStore.shownKey;

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

	const operationResult = await tryCatch(
		Promise.all([
			queryClient.query(vulnerabilityOverviewQuery(scope)),
			queryClient.query(vulnerabilityListQuery(scope, vulnerabilityEnvironments(), requestForApi))
		])
	);
	if (operationResult.error !== null) {
		throwPageLoadError(operationResult.error, 'Failed to load security data');
	}
	const [overview] = operationResult.data;

	return {
		featureDisabled: false,
		overview,
		vulnerabilityRequestOptions,
		patchRequestOptions
	};
};
