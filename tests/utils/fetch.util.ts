import type { Page } from '@playwright/test';
import { ContainerSummary } from 'types/containers.type';
import { ImageUsageCounts } from 'types/image.type';
import { NetworkSummary, NetworkUsageCounts } from 'types/networks.type';
import { Project, ProjectStatusCounts } from 'types/project.type';
import { VolumeUsageCounts } from 'types/volumes.type';
import { ApiKey } from 'types/api-key.type';

export type Paginated<T> = { data: T[]; pagination?: { totalItems?: number } };

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

export async function fetchVolumesWithRetry(page: Page, maxRetries = 1): Promise<any[]> {
  return retry(async () => {
    const res = await page.request.get('/api/environments/0/volumes');
    const json = await res.json();
    return Array.isArray(json?.data) ? json.data : [];
  }, maxRetries);
}

export async function fetchVolumeCountsWithRetry(page: Page, maxRetries = 3): Promise<VolumeUsageCounts> {
  return retry(
    async () => {
      const res = await page.request.get('/api/environments/0/volumes/counts');
      const json = await res.json().catch(() => null);
      const data = Array.isArray(json) ? json : json?.data?.data ?? json?.data ?? [];
      return (data ?? []) as VolumeUsageCounts;
    },
    maxRetries,
    800
  );
}

const PROJECTS_ENDPOINT = '/api/environments/0/projects';
export async function fetchProjectsWithRetry(page: Page, maxRetries = 3): Promise<Project[]> {
  return retry(async () => {
    const res = await page.request.get(PROJECTS_ENDPOINT);
    const body = await res.json().catch(() => null as any);
    if (Array.isArray(body)) return body;
    if (Array.isArray(body?.data)) return body.data;
    if (Array.isArray(body?.projects)) return body.projects;
    return [];
  }, maxRetries);
}

export async function fetchProjectCountsWithRetry(page: Page, maxRetries = 3): Promise<ProjectStatusCounts> {
  return retry(
    async () => {
      const res = await page.request.get('/api/environments/0/projects/counts');
      const json = await res.json().catch(() => null);
      const data = Array.isArray(json) ? json : json?.data?.data ?? json?.data ?? [];
      return data as ProjectStatusCounts;
    },
    maxRetries,
    800
  );
}

export async function fetchNetworksWithRetry(page: Page, maxRetries = 3): Promise<NetworkSummary[]> {
  return retry(
    async () => {
      const res = await page.request.get('/api/environments/0/networks');
      const json = await res.json().catch(() => null);
      const data = Array.isArray(json) ? json : json?.data?.data ?? json?.data ?? [];
      return (data ?? []) as NetworkSummary[];
    },
    maxRetries,
    800
  );
}

export async function fetchNetworksCountsWithRetry(page: Page, maxRetries = 3): Promise<NetworkUsageCounts> {
  return retry(
    async () => {
      const res = await page.request.get('/api/environments/0/networks/counts');
      const json = await res.json().catch(() => null);
      const data = Array.isArray(json) ? json : json?.data?.data ?? json?.data ?? [];
      return (data ?? []) as NetworkUsageCounts;
    },
    maxRetries,
    800
  );
}

export async function fetchImagesWithRetry(page: Page, maxRetries = 3): Promise<any[]> {
  return retry(async () => {
    const res = await page.request.get('/api/environments/0/images');
    if (!res.ok()) throw new Error(`HTTP ${res.status()}`);
    const body = await res.json().catch(() => null as any);
    return Array.isArray(body?.data) ? body.data : [];
  }, maxRetries);
}

export async function fetchImageCountsWithRetry(page: Page, maxRetries = 3): Promise<ImageUsageCounts> {
  return retry(
    async () => {
      const res = await page.request.get('/api/environments/0/images/counts');
      const json = await res.json().catch(() => null);
      const data = Array.isArray(json) ? json : json?.data?.data ?? json?.data ?? [];
      return data as ImageUsageCounts;
    },
    maxRetries,
    800
  );
}

export async function fetchContainersWithRetry(page: Page, maxRetries = 3): Promise<Paginated<ContainerSummary>> {
  return retry(async () => {
    const res = await page.request.get('/api/environments/0/containers');
    if (!res.ok()) throw new Error(`HTTP ${res.status()}`);
    const body = await res.json().catch(() => null as any);
    const data = Array.isArray(body?.data) ? (body.data as ContainerSummary[]) : [];
    const pagination = body?.pagination || { totalItems: data.length };
    return { data, pagination };
  }, maxRetries);
}

export async function fetchApiKeysWithRetry(page: Page, maxRetries = 3): Promise<Paginated<ApiKey>> {
  return retry(async () => {
    const res = await page.request.get('/api/api-keys');
    if (!res.ok()) throw new Error(`HTTP ${res.status()}`);
    const body = await res.json().catch(() => null as any);
    const data = Array.isArray(body?.data) ? (body.data as ApiKey[]) : [];
    const pagination = body?.pagination || { totalItems: data.length };
    return { data, pagination };
  }, maxRetries);
}
