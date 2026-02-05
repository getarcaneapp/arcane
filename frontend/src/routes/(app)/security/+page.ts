import { vulnerabilityService } from '$lib/services/vulnerability-service';
import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { resolveInitialTableRequest } from '$lib/utils/table-persistence.util';
import type { Paginated } from '$lib/types/pagination.type';
import type { VulnerabilityWithImage } from '$lib/types/vulnerability.type';
import type { PageLoad } from './$types';

function mapVulnerabilityRequest(options: SearchPaginationSortRequest): SearchPaginationSortRequest {
	const filters = { ...(options.filters ?? {}) };
	if (filters.vulnSeverity) {
		filters.severity = filters.vulnSeverity;
		delete filters.vulnSeverity;
	}

	const sort = options.sort?.column === 'vulnSeverity' ? { ...options.sort, column: 'severity' } : options.sort;

	return {
		...options,
		sort,
		filters: Object.keys(filters).length ? filters : undefined
	};
}

function getVulnerabilityKey(vuln: VulnerabilityWithImage, index: number): string {
	return [
		vuln.imageId,
		vuln.vulnerabilityId,
		vuln.pkgName,
		vuln.installedVersion ?? '',
		vuln.fixedVersion ?? '',
		String(index)
	].join('-');
}

function mapVulnerabilityPage(
	page: Paginated<VulnerabilityWithImage>,
	options: SearchPaginationSortRequest
): Paginated<VulnerabilityWithImage & { id: string }> {
	const pageNumber = options.pagination?.page ?? page.pagination?.currentPage ?? 1;
	const limit = options.pagination?.limit ?? page.pagination?.itemsPerPage ?? 20;
	const offset = (pageNumber - 1) * limit;
	return {
		...page,
		data: (page.data ?? []).map((item, index) => ({
			...item,
			id: getVulnerabilityKey(item, offset + index)
		}))
	};
}

export const load: PageLoad = async () => {
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

	const requestForApi = mapVulnerabilityRequest(vulnerabilityRequestOptions);

	const [summary, vulnerabilities] = await Promise.all([
		vulnerabilityService.getEnvironmentSummary(),
		vulnerabilityService.getAllVulnerabilities(requestForApi)
	]);

	return {
		summary,
		vulnerabilities: mapVulnerabilityPage(vulnerabilities, vulnerabilityRequestOptions),
		vulnerabilityRequestOptions
	};
};
