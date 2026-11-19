import { queryOptions } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { imageService } from '#lib/services/image-service.js';
import { vulnerabilityService } from '#lib/services/vulnerability-service.js';
import { environmentStore, type EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
import type { Paginated } from '#lib/types/base.js';
import type { PatchTarget } from '#lib/types/imagepatch.js';
import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
import type { Vulnerability, VulnerabilityScanResult } from '#lib/types/vulnerability.js';
import { isVulnerabilityScanInProgress, stabilizeFailedVulnerabilitySummary } from '#lib/utils/docker.js';
import { fetchAcrossEnvironments, type EnvironmentFailure, type EnvironmentRow } from '#lib/utils/environments.js';
import { tryCatch } from '#lib/utils/try-catch.js';
import { fetchRiskOverview, mapVulnerabilityPage } from '#lib/utils/vulnerability.js';

export type PatchTargetRow = EnvironmentRow<PatchTarget> & { id: string };
export type PatchTargetPage = Paginated<PatchTargetRow> & { failedEnvironments?: EnvironmentFailure[] };

export function vulnerabilityOverviewQuery(scope: string) {
	return queryOptions({
		queryKey: queryKeys.vulnerabilities.overviewByEnvironment(scope),
		queryFn: () => fetchRiskOverview()
	});
}

export function vulnerabilityListQuery(scope: string, environments: EnvironmentRef[], request: SearchPaginationSortRequest) {
	return queryOptions({
		queryKey: queryKeys.vulnerabilities.allByEnvironment(scope, request),
		queryFn: async () =>
			mapVulnerabilityPage(
				await fetchAcrossEnvironments(environments, request, (id, environmentRequest, config) =>
					vulnerabilityService.getAllVulnerabilitiesForEnvironment(id, environmentRequest, config)
				),
				request
			)
	});
}

export function patchTargetListQuery(scope: string, environments: EnvironmentRef[], requestOptions: SearchPaginationSortRequest) {
	return queryOptions({
		queryKey: queryKeys.images.patchTargets(scope, requestOptions),
		queryFn: async (): Promise<PatchTargetPage> => {
			const page = await fetchAcrossEnvironments(environments, requestOptions, (id, request, config) =>
				imageService.listPatchTargets(request, id, config)
			);
			return { ...page, data: page.data.map((target) => ({ ...target, id: target.imageId })) };
		}
	});
}

export type ImageVulnerabilityRow = Vulnerability & { id: string };

function imageVulnerabilityRowKey(vuln: Vulnerability, index: number): string {
	return [vuln.vulnerabilityId, vuln.pkgName, vuln.installedVersion ?? '', vuln.fixedVersion ?? '', String(index)].join('-');
}

function toImageVulnerabilityRequest(options: SearchPaginationSortRequest): SearchPaginationSortRequest {
	const { vulnSeverity, ...rest } = options.filters ?? {};
	const filters = vulnSeverity ? { ...rest, severity: vulnSeverity } : rest;
	return {
		...options,
		filters: Object.keys(filters).length ? filters : undefined,
		sort: options.sort?.column === 'vulnSeverity' ? { ...options.sort, column: 'severity' } : options.sort
	};
}

export function imageVulnerabilityListQuery(
	environmentId: string,
	imageId: string,
	scanTime: string,
	requestOptions: SearchPaginationSortRequest
) {
	const request = toImageVulnerabilityRequest(requestOptions);
	return queryOptions({
		queryKey: queryKeys.vulnerabilities.imageRows(environmentId, imageId, scanTime, request),
		queryFn: async ({ signal }): Promise<Paginated<ImageVulnerabilityRow>> => {
			await environmentStore.ready;
			signal.throwIfAborted();
			const response = await vulnerabilityService.getImageVulnerabilities(imageId, request, environmentId);
			const offset = ((request.pagination?.page ?? 1) - 1) * (request.pagination?.limit ?? response.pagination.itemsPerPage);
			return {
				...response,
				data: response.data.map((item, index) => ({ ...item, id: imageVulnerabilityRowKey(item, offset + index) }))
			};
		}
	});
}

async function fetchImageScanResult(
	environmentId: string,
	imageId: string,
	requestedAt: () => string | undefined,
	signal: AbortSignal
): Promise<VulnerabilityScanResult> {
	await environmentStore.ready;
	signal.throwIfAborted();
	const result = await vulnerabilityService.getScanResult(imageId, environmentId);
	const scanRequestedAt = requestedAt();
	if (result.status !== 'failed' || scanRequestedAt === undefined) return result;
	const { data: summary } = await tryCatch(
		stabilizeFailedVulnerabilitySummary(imageId, result, (id) => vulnerabilityService.getScanSummary(id, environmentId), {
			scanRequestedAt
		})
	);
	if (!summary || summary.status === 'failed') return result;
	if (!isVulnerabilityScanInProgress(summary.status)) return vulnerabilityService.getScanResult(imageId, environmentId);
	return {
		...result,
		scanTime: summary.scanTime,
		status: summary.status,
		scanPhase: summary.scanPhase,
		summary: summary.summary,
		error: summary.error
	};
}

export function imageScanResultQuery(
	environmentId: string,
	imageId: string,
	scan: { requestedAt: () => string | undefined; onResult: (result: VulnerabilityScanResult) => void }
) {
	return queryOptions({
		queryKey: queryKeys.vulnerabilities.scanResult(environmentId, imageId),
		queryFn: async ({ signal }) => {
			const result = await fetchImageScanResult(environmentId, imageId, scan.requestedAt, signal);
			scan.onResult(result);
			return result;
		}
	});
}
