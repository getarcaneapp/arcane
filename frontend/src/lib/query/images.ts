import { queryOptions } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { imageService } from '#lib/services/image-service.js';
import type { EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
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
