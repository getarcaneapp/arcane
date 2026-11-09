import { m } from '#lib/paraglide/messages.js';
import type { ContainerRegistry } from '#lib/types/containerregistry.js';
import type { ImageBuildRequest } from '#lib/types/image.js';
import { parseList } from '#lib/utils/form-parsers.js';
import { buildImageReference } from '#lib/utils/registry.js';

export type BuildFormValues = {
	dockerfile: string;
	tags: string;
	registryId: string;
	repositoryName: string;
	pushTag: string;
	target: string;
	buildArgs: string;
	labels: string;
	cacheFrom: string;
	cacheTo: string;
	network: string;
	isolation: string;
	shmSize: string;
	ulimits: string;
	entitlements: string;
	privileged: boolean;
	extraHosts: string;
	platforms: string;
	noCache: boolean;
	pull: boolean;
	provider: 'local' | 'depot';
	push: boolean;
	load: boolean;
};

type BuildRequestResult = { request: ImageBuildRequest; error?: undefined } | { request?: undefined; error: string };

function parseKeyValueLines(raw: string): Record<string, string> {
	const result: Record<string, string> = {};
	for (const line of raw.split('\n')) {
		const trimmed = line.trim();
		if (!trimmed) continue;
		const idx = trimmed.indexOf('=');
		if (idx === -1) continue;
		const key = trimmed.slice(0, idx).trim();
		if (!key) continue;
		result[key] = trimmed.slice(idx + 1).trim();
	}
	return result;
}

function parseOptionalBytes(raw: string): number | undefined {
	const parsed = Number.parseInt(raw.trim(), 10);
	return Number.isFinite(parsed) && parsed > 0 ? parsed : undefined;
}

function resolveBuildTags(
	data: BuildFormValues,
	push: boolean,
	registries: ContainerRegistry[]
): { tags: string[]; error?: undefined } | { tags?: undefined; error: string } {
	if (!push) {
		const tags = parseList(data.tags);
		return tags.length > 0 ? { tags } : { error: m.image_tags_required() };
	}

	const registry = registries.find((item) => item.id === data.registryId);
	if (!registry) return { error: m.select_a_registry() };

	const repositoryName = data.repositoryName.trim();
	if (!repositoryName) return { error: m.select_a_repository_name() };
	if (!registry.repositoryNames?.includes(repositoryName)) return { error: m.repository_name_not_configured() };

	const reference = buildImageReference(registry.host, registry.namespace, repositoryName, data.pushTag);
	if (!reference) return { error: m.tag_required() };

	return { tags: [reference] };
}

function unsupportedProviderFields(provider: 'local' | 'depot', request: ImageBuildRequest): string | null {
	const unsupportedFields =
		provider === 'local'
			? {
					cacheTo: (request.cacheTo?.length ?? 0) > 0,
					entitlements: (request.entitlements?.length ?? 0) > 0,
					privileged: request.privileged === true,
					platforms: (request.platforms?.length ?? 0) > 1
				}
			: {
					network: Boolean(request.network),
					isolation: Boolean(request.isolation),
					shmSize: Boolean(request.shmSize),
					ulimits: Object.keys(request.ulimits ?? {}).length > 0,
					extraHosts: (request.extraHosts?.length ?? 0) > 0
				};
	const unsupported = Object.entries(unsupportedFields)
		.filter(([, present]) => present)
		.map(([field]) => field);
	if (unsupported.length === 0) return null;
	const fields = unsupported.sort().join(', ');
	return provider === 'local' ? m.build_provider_unsupported_local({ fields }) : m.build_provider_unsupported_depot({ fields });
}

export function createBuildRequest(
	data: BuildFormValues,
	options: { contextDir: string; provider: 'local' | 'depot'; registries: ContainerRegistry[] }
): BuildRequestResult {
	const push = options.provider === 'depot' ? true : data.push;
	const resolvedTags = resolveBuildTags(data, push, options.registries);
	if (resolvedTags.error) return { error: resolvedTags.error };

	const request: ImageBuildRequest = {
		contextDir: options.contextDir,
		dockerfile: data.dockerfile.trim() || undefined,
		tags: resolvedTags.tags,
		target: data.target.trim() || undefined,
		buildArgs: parseKeyValueLines(data.buildArgs),
		labels: parseKeyValueLines(data.labels),
		cacheFrom: parseList(data.cacheFrom),
		cacheTo: parseList(data.cacheTo),
		noCache: data.noCache,
		pull: data.pull,
		network: data.network.trim() || undefined,
		isolation: data.isolation.trim() || undefined,
		shmSize: parseOptionalBytes(data.shmSize),
		ulimits: parseKeyValueLines(data.ulimits),
		entitlements: parseList(data.entitlements),
		privileged: data.privileged,
		extraHosts: parseList(data.extraHosts),
		platforms: parseList(data.platforms),
		provider: options.provider,
		push,
		load: options.provider === 'depot' ? false : data.load
	};

	const providerError = unsupportedProviderFields(options.provider, request);
	return providerError ? { error: providerError } : { request };
}
