import { QueryClient, type QueryKey } from '@tanstack/svelte-query';
import { queryKeys } from './query-keys';

const DEFAULT_QUERY_STALE_TIME_MS = 30_000;
const STABLE_QUERY_STALE_TIME_MS = 5 * 60_000;
const DEFAULT_QUERY_GC_TIME_MS = 5 * 60_000;

let appQueryClient: QueryClient | null = null;

function createAppQueryClientInternal(): QueryClient {
	const queryClient = new QueryClient({
		defaultOptions: {
			queries: {
				staleTime: DEFAULT_QUERY_STALE_TIME_MS,
				gcTime: DEFAULT_QUERY_GC_TIME_MS,
				refetchOnMount: true,
				refetchOnWindowFocus: true,
				refetchOnReconnect: true
			}
		}
	});

	queryClient.setQueryDefaults(queryKeys.settings.all, {
		staleTime: STABLE_QUERY_STALE_TIME_MS
	});
	queryClient.setQueryDefaults(queryKeys.auth.autoLoginConfig(), {
		staleTime: STABLE_QUERY_STALE_TIME_MS
	});
	queryClient.setQueryDefaults(queryKeys.system.versionInfoPrefix(), {
		staleTime: STABLE_QUERY_STALE_TIME_MS
	});

	return queryClient;
}

export function getAppQueryClient(): QueryClient {
	appQueryClient ??= createAppQueryClientInternal();
	return appQueryClient;
}

async function invalidateQueryKeys(queryClient: QueryClient, queryKeysToInvalidate: QueryKey[]): Promise<void> {
	await Promise.all(queryKeysToInvalidate.map((queryKey) => queryClient.invalidateQueries({ queryKey })));
}

export async function invalidateAuthStateQueries(): Promise<void> {
	const queryClient = getAppQueryClient();
	await invalidateQueryKeys(queryClient, [
		queryKeys.auth.all,
		queryKeys.users.all,
		queryKeys.settings.all,
		queryKeys.environments.all,
		queryKeys.system.versionInfoPrefix()
	]);
}

export async function invalidateTemplateQueries(templateId?: string): Promise<void> {
	const queryClient = getAppQueryClient();
	await invalidateQueryKeys(queryClient, [
		queryKeys.templates.all,
		queryKeys.templates.contentPrefix(),
		queryKeys.templates.registries(),
		queryKeys.templates.globalVariables(),
		...(templateId ? [queryKeys.templates.content(templateId)] : [])
	]);
}

export async function invalidateContainerQueries(environmentId: string, containerId?: string): Promise<void> {
	const queryClient = getAppQueryClient();
	await invalidateQueryKeys(queryClient, [
		queryKeys.containers.all,
		['containers', environmentId],
		queryKeys.containers.statusCounts(environmentId),
		queryKeys.dashboard.all,
		...(containerId ? [queryKeys.containers.detail(environmentId, containerId)] : [])
	]);
}

export async function invalidateProjectQueries(environmentId: string, projectId?: string): Promise<void> {
	const queryClient = getAppQueryClient();
	await invalidateQueryKeys(queryClient, [
		queryKeys.projects.all,
		['projects', environmentId],
		queryKeys.projects.statusCounts(environmentId),
		['containers', environmentId],
		['images', environmentId],
		queryKeys.dashboard.all,
		...(projectId ? [queryKeys.projects.detail(environmentId, projectId)] : [])
	]);
}

export async function invalidateSwarmServiceQueries(serviceId?: string): Promise<void> {
	const queryClient = getAppQueryClient();
	await invalidateQueryKeys(queryClient, [
		queryKeys.swarm.all,
		queryKeys.swarm.services.all,
		queryKeys.swarm.services.detailPrefix(),
		queryKeys.dashboard.all,
		...(serviceId ? [queryKeys.swarm.services.detail(serviceId)] : [])
	]);
}

export async function invalidateSwarmStackQueries(name?: string): Promise<void> {
	const queryClient = getAppQueryClient();
	await invalidateQueryKeys(queryClient, [
		queryKeys.swarm.all,
		queryKeys.swarm.stacks.all,
		queryKeys.swarm.stacks.detailPrefix(),
		queryKeys.swarm.services.all,
		queryKeys.dashboard.all,
		...(name ? [queryKeys.swarm.stacks.detail(name)] : [])
	]);
}

export async function invalidateEnvironmentScopedQueries(environmentId: string): Promise<void> {
	const queryClient = getAppQueryClient();

	await Promise.all([
		queryClient.invalidateQueries({ queryKey: queryKeys.environments.all }),
		queryClient.invalidateQueries({ queryKey: queryKeys.settings.all }),
		queryClient.invalidateQueries({ queryKey: queryKeys.dashboard.all }),
		queryClient.invalidateQueries({ queryKey: ['containers', environmentId] }),
		queryClient.invalidateQueries({ queryKey: ['container', environmentId] }),
		queryClient.invalidateQueries({ queryKey: queryKeys.containers.statusCounts(environmentId) }),
		queryClient.invalidateQueries({ queryKey: ['images', environmentId] }),
		queryClient.invalidateQueries({ queryKey: ['image', environmentId] }),
		queryClient.invalidateQueries({ queryKey: queryKeys.images.usageCounts(environmentId) }),
		queryClient.invalidateQueries({ queryKey: ['projects', environmentId] }),
		queryClient.invalidateQueries({ queryKey: ['project', environmentId] }),
		queryClient.invalidateQueries({ queryKey: queryKeys.projects.statusCounts(environmentId) }),
		queryClient.invalidateQueries({ queryKey: ['networks', environmentId] }),
		queryClient.invalidateQueries({ queryKey: ['network', environmentId] }),
		queryClient.invalidateQueries({ queryKey: ['ports', environmentId] }),
		queryClient.invalidateQueries({ queryKey: ['volumes', environmentId] }),
		queryClient.invalidateQueries({ queryKey: ['volume', environmentId] }),
		queryClient.invalidateQueries({ queryKey: ['gitops-syncs', environmentId] }),
		queryClient.invalidateQueries({ queryKey: queryKeys.vulnerabilities.summaryByEnvironment(environmentId) }),
		queryClient.invalidateQueries({ queryKey: queryKeys.system.versionInfoPrefix() })
	]);
}
