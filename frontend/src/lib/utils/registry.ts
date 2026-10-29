import { m } from '#lib/paraglide/messages.js';
import type { ContainerRegistry } from '#lib/types/docker.js';

type RegistryIdentity = Pick<ContainerRegistry, 'url' | 'registryType'>;

export function getRegistryDisplayName(registry: RegistryIdentity): string {
	if (registry.registryType === 'ecr') return m.amazon_ecr();
	const url = registry.url;
	if (!url || url === 'docker.io') return m.registry_docker_hub();
	if (url.includes('ghcr.io')) return m.registry_github_container_registry();
	if (url.includes('gcr.io')) return m.registry_google_container_registry();
	if (url.includes('quay.io')) return m.registry_quay_io();
	return url;
}

export function arcaneImageRegistryOptions() {
	return [
		{ value: 'ghcr.io', label: m.registry_github_container_registry() },
		{ value: 'docker.io', label: m.registry_docker_hub(), badge: m.no_pull_limits() }
	];
}

export function arcaneUpdateCheckRegistryOptions() {
	return [
		{ value: 'auto', label: m.auto() },
		...arcaneImageRegistryOptions(),
		{ value: 'public.ecr.aws', label: m.registry_amazon_ecr_public() }
	];
}

function arcaneRegistryHost(registry: string): string {
	return registry === 'docker.io' ? 'docker.io' : 'ghcr.io';
}

export function arcaneToolsImage(registry: string): string {
	return `${arcaneRegistryHost(registry)}/getarcaneapp/tools:latest`;
}

export function arcaneTrivyDbImages(registry: string): string[] {
	const host = arcaneRegistryHost(registry);
	return [`${host}/getarcaneapp/trivy-db:2`, `${host}/getarcaneapp/trivy-java-db:1`, `${host}/getarcaneapp/trivy-checks:1`];
}

export function arcaneUpdateCheckImage(registry: string, isLocalEnvironment: boolean): string | null {
	if (registry === 'auto') return null;
	const host = registry === 'public.ecr.aws' ? registry : arcaneRegistryHost(registry);
	return `${host}/getarcaneapp/${isLocalEnvironment ? 'manager' : 'agent'}`;
}

export function buildImageReference(host: string, namespace: string, repositoryName: string, tag: string): string {
	const repository = repositoryName.trim();
	const trimmedTag = tag.trim();
	if (!repository || !trimmedTag) return '';
	return `${[host, namespace, repository].filter(Boolean).join('/')}:${trimmedTag}`;
}
