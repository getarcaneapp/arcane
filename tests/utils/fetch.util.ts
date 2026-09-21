import { expect, test, type APIRequestContext, type Page } from '@playwright/test';
import { ContainerSummary } from 'types/containers.type';
import { ImageUsageCounts } from 'types/image.type';
import { NetworkSummary, NetworkUsageCounts } from 'types/networks.type';
import { Project, ProjectStatusCounts } from 'types/project.type';
import { VolumeUsageCounts } from 'types/volumes.type';
import { ApiKey } from 'types/api-key.type';

export type Paginated<T> = { data: T[]; pagination?: { totalItems?: number } };

type JsonResponse = {
	ok(): boolean;
	status(): number;
	text(): Promise<string>;
};

async function readApiResponse<T>(
	response: JsonResponse,
	action: string
): Promise<{ data: T; pagination?: { totalItems?: number } }> {
	const responseText = await response.text();
	let parsed: unknown;

	try {
		parsed = JSON.parse(responseText);
	} catch {
		throw new Error(`${action} returned non-JSON response ${response.status()}: ${responseText}`);
	}

	if (typeof parsed !== 'object' || parsed === null) {
		throw new Error(`${action} returned an invalid response ${response.status()}: ${responseText}`);
	}

	const payload = parsed as { success?: boolean; data?: T };
	if (
		!response.ok() ||
		payload.success === false ||
		!('data' in payload) ||
		payload.data === null ||
		payload.data === undefined
	) {
		throw new Error(`${action} failed with ${response.status()}: ${responseText}`);
	}

	return parsed as { data: T; pagination?: { totalItems?: number } };
}

export async function readApiData<T>(response: JsonResponse, action: string): Promise<T> {
	return (await readApiResponse<T>(response, action)).data;
}

export async function readList<T>(response: JsonResponse, action: string): Promise<Paginated<T>> {
	const payload = await readApiResponse<T[]>(response, action);
	if (!Array.isArray(payload.data)) throw new Error(`${action} did not return a list`);
	if (
		payload.pagination !== undefined &&
		(typeof payload.pagination !== 'object' ||
			payload.pagination === null ||
			!Number.isInteger(payload.pagination.totalItems) ||
			Number(payload.pagination.totalItems) < 0)
	) {
		throw new Error(`${action} returned invalid pagination`);
	}
	return { data: payload.data, pagination: payload.pagination };
}

async function readCounts<T>(
	response: JsonResponse,
	action: string,
	keys: Array<keyof T>
): Promise<T> {
	const data = await readApiData<T>(response, action);
	if (
		typeof data !== 'object' ||
		data === null ||
		keys.some(
			(key) =>
				typeof data[key] !== 'number' || !Number.isInteger(data[key]) || Number(data[key]) < 0
		)
	) {
		throw new Error(`${action} returned invalid counts`);
	}
	return data;
}

async function retry<T>(fn: () => Promise<T>, maxRetries: number, delayMs = 1000): Promise<T> {
	let attempt = 0;
	while (true) {
		try {
			return await fn();
		} catch (e) {
			attempt++;
			if (attempt >= maxRetries) throw e;
			await new Promise((r) => setTimeout(r, delayMs));
		}
	}
}

export async function fetchVolumesWithRetry(
	page: Page,
	maxRetries = 1
): Promise<Record<string, unknown>[]> {
	return retry(
		async () =>
			(
				await readList<Record<string, unknown>>(
					await page.request.get('/api/environments/0/volumes'),
					'List volumes'
				)
			).data,
		maxRetries
	);
}

export async function fetchProjectsWithRetry(page: Page, maxRetries = 3): Promise<Project[]> {
	return retry(
		async () =>
			(
				await readList<Project>(
					await page.request.get('/api/environments/0/projects'),
					'List projects'
				)
			).data,
		maxRetries
	);
}

export async function fetchNetworksWithRetry(
	page: Page,
	maxRetries = 3
): Promise<NetworkSummary[]> {
	return retry(
		async () =>
			(
				await readList<NetworkSummary>(
					await page.request.get('/api/environments/0/networks'),
					'List networks'
				)
			).data,
		maxRetries
	);
}

export async function fetchImagesWithRetry(
	page: Page,
	maxRetries = 3
): Promise<Record<string, unknown>[]> {
	return retry(
		async () =>
			(
				await readList<Record<string, unknown>>(
					await page.request.get('/api/environments/0/images'),
					'List images'
				)
			).data,
		maxRetries
	);
}

export async function fetchVolumeCountsWithRetry(
	page: Page,
	maxRetries = 3
): Promise<VolumeUsageCounts> {
	return retry(
		async () =>
			readCounts<VolumeUsageCounts>(
				await page.request.get('/api/environments/0/volumes/counts'),
				'Get volumes counts',
				['inuse', 'unused', 'total']
			),
		maxRetries,
		800
	);
}

export async function fetchProjectCountsWithRetry(
	page: Page,
	maxRetries = 3
): Promise<ProjectStatusCounts> {
	return retry(
		async () =>
			readCounts<ProjectStatusCounts>(
				await page.request.get('/api/environments/0/projects/counts'),
				'Get projects counts',
				['runningProjects', 'stoppedProjects', 'totalProjects', 'archivedProjects']
			),
		maxRetries,
		800
	);
}

export async function fetchNetworksCountsWithRetry(
	page: Page,
	maxRetries = 3
): Promise<NetworkUsageCounts> {
	return retry(
		async () =>
			readCounts<NetworkUsageCounts>(
				await page.request.get('/api/environments/0/networks/counts'),
				'Get networks counts',
				['inuse', 'unused', 'total']
			),
		maxRetries,
		800
	);
}

export async function fetchImageCountsWithRetry(
	page: Page,
	maxRetries = 3
): Promise<ImageUsageCounts> {
	return retry(
		async () =>
			readCounts<ImageUsageCounts>(
				await page.request.get('/api/environments/0/images/counts'),
				'Get images counts',
				['imagesInuse', 'imagesUnused', 'totalImages', 'totalImageSize']
			),
		maxRetries,
		800
	);
}

export async function fetchContainersWithRetry(
	page: Page,
	maxRetries = 3
): Promise<Paginated<ContainerSummary>> {
	return retry(
		async () =>
			readList<ContainerSummary>(
				await page.request.get('/api/environments/0/containers'),
				'List containers'
			),
		maxRetries
	);
}

export async function fetchApiKeysWithRetry(
	page: Page,
	maxRetries = 3
): Promise<Paginated<ApiKey>> {
	return retry(
		async () => readList<ApiKey>(await page.request.get('/api/api-keys'), 'List apikeys'),
		maxRetries
	);
}

export async function removeApiResource(
	page: Page,
	path: string,
	options?: Parameters<APIRequestContext['delete']>[1]
) {
	try {
		const resourcePath = path.split('?')[0];
		const asynchronous =
			/\/projects\/[^/]+\/destroy$/.test(resourcePath) || /\/containers\/[^/]+$/.test(resourcePath);
		if (
			asynchronous ||
			/\/environments\/[^/]+\/(images|networks|volumes)\/[^/]+$/.test(resourcePath)
		) {
			const existing = await page.request.get(resourcePath.replace(/\/destroy$/, ''));
			if (existing.status() === 404) return;
			if (!existing.ok())
				throw new Error(
					`Cleanup lookup ${resourcePath}: ${existing.status()} ${await existing.text()}`
				);
		}
		const response = await page.request.delete(path, options);
		if (response.status() === 404) return;
		if (response.ok()) {
			if (asynchronous) {
				await expect
					.poll(
						async () => {
							const remaining = await page.request.get(resourcePath.replace(/\/destroy$/, ''));
							if (!remaining.ok() && remaining.status() !== 404) {
								throw new Error(
									`Cleanup lookup ${resourcePath}: ${remaining.status()} ${await remaining.text()}`
								);
							}
							return remaining.status();
						},
						{ timeout: 60_000, message: `Wait for deletion of ${resourcePath}` }
					)
					.toBe(404);
			}
			return;
		}
		const detail = `DELETE ${path}: ${response.status()} ${await response.text()}`;
		await test.info().attach('cleanup-error', { body: detail, contentType: 'text/plain' });
		expect.soft(false, detail).toBe(true);
	} catch (error) {
		expect.soft(false, `DELETE ${path}: ${String(error)}`).toBe(true);
	}
}
