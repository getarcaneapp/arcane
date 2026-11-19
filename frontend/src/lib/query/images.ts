import { queryOptions } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { buildWorkspaceService } from '#lib/services/build-workspace-service.js';
import { imageService } from '#lib/services/image-service.js';
import type { EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
import { parseImageRef } from '#lib/utils/docker.js';
import { fetchAcrossEnvironments, sumAcrossEnvironments } from '#lib/utils/environments.js';

export function imageListQuery(scope: string, environments: EnvironmentRef[], requestOptions: SearchPaginationSortRequest) {
	return queryOptions({
		queryKey: queryKeys.images.list(scope, requestOptions),
		queryFn: () =>
			fetchAcrossEnvironments(environments, requestOptions, (id, request, config) =>
				imageService.getImagesForEnvironment(id, request, config)
			)
	});
}

export function imageUsageCountsQuery(scope: string, environments: EnvironmentRef[]) {
	return queryOptions({
		queryKey: queryKeys.images.usageCounts(scope),
		queryFn: () =>
			sumAcrossEnvironments(environments, (id, config) => imageService.getImageUsageCountsForEnvironment(id, config), {
				imagesInuse: 0,
				imagesUnused: 0,
				totalImages: 0,
				totalImageSize: 0
			})
	});
}

export function imageDetailQuery(environmentId: string, imageId: string) {
	return queryOptions({
		queryKey: queryKeys.images.detail(environmentId, imageId),
		queryFn: async () => {
			const image = await imageService.getImageForEnvironment(environmentId, imageId);
			const firstTag = image.repoTags?.[0];
			if (!firstTag || firstTag === '<none>:<none>') return { ...image, repo: '<none>', tag: '<none>' };
			return { ...image, ...parseImageRef(firstTag) };
		}
	});
}

export function imageBuildListQuery(scope: string, environments: EnvironmentRef[], requestOptions: SearchPaginationSortRequest) {
	return queryOptions({
		queryKey: queryKeys.images.buildsList(scope, requestOptions),
		queryFn: () =>
			fetchAcrossEnvironments(environments, requestOptions, (id, request, config) =>
				imageService.getImageBuilds(request, id, config)
			)
	});
}

export function imageUploadLimitQuery(environmentId: string) {
	return queryOptions({
		queryKey: queryKeys.images.uploadLimit(environmentId),
		queryFn: async () =>
			(await imageService.getImagesForEnvironment(environmentId, { pagination: { page: 1, limit: 1 } })).maxImageUploadSize
	});
}

export function imageHistoryQuery(environmentId: string, imageId: string) {
	return queryOptions({
		queryKey: queryKeys.images.history(environmentId ?? '', imageId),
		queryFn: async () => {
			await environmentStore.ready;
			return imageService.getImageHistory(imageId, environmentId);
		}
	});
}

export function imageBuildRecordQuery(environmentId: string, buildId: string | null) {
	return queryOptions({
		queryKey: queryKeys.images.buildRecord(environmentId, buildId ?? 'none'),
		queryFn: () => imageService.getImageBuild(buildId ?? '', environmentId)
	});
}

export function imageReleaseNotesQuery(environmentId: string, imageRef: string, version?: string, digest?: string) {
	return queryOptions({
		queryKey: queryKeys.images.releaseNotes(environmentId, imageRef, version ?? '', digest ?? ''),
		queryFn: () => imageService.getImageReleaseNotes(environmentId, imageRef, { version, digest })
	});
}

export function buildWorkspaceQuery(environmentId: string) {
	return queryOptions({
		queryKey: queryKeys.buildWorkspace.workspace(environmentId),
		queryFn: () => buildWorkspaceService.getWorkspace(environmentId)
	});
}
