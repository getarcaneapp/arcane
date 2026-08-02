import { m } from '#lib/paraglide/messages';
import type { ContainerRegistry } from '#lib/types/docker';

type RegistryIdentity = Pick<ContainerRegistry, 'url' | 'registryType'>;

/** Human readable name for a registry, falling back to its URL. */
export function getRegistryDisplayName(registry: RegistryIdentity): string {
	if (registry.registryType === 'ecr') return m.amazon_ecr();
	const url = registry.url;
	if (!url || url === 'docker.io') return m.registry_docker_hub();
	if (url.includes('ghcr.io')) return m.registry_github_container_registry();
	if (url.includes('gcr.io')) return m.registry_google_container_registry();
	if (url.includes('quay.io')) return m.registry_quay_io();
	return url;
}

/** Strips the scheme and trailing slashes so a registry URL can be used as an image host. */
export function normalizeRegistryHost(url: string): string {
	return url.replace(/^https?:\/\//, '').replace(/\/+$/, '');
}

/**
 * Builds a `host/repository:tag` reference, or an empty string when the
 * repository name or tag is missing.
 */
export function buildImageReference(registryUrl: string, repositoryName: string, tag: string): string {
	const repository = repositoryName.trim();
	const trimmedTag = tag.trim();
	if (!repository || !trimmedTag) return '';
	return `${normalizeRegistryHost(registryUrl)}/${repository}:${trimmedTag}`;
}
