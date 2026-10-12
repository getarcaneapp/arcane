import { projectService } from '#lib/services/project-service.js';
import type { EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
import type { ProjectTagOption } from '#lib/types/swarm.js';
import { collectAcrossEnvironments, type EnvironmentFailure } from '#lib/utils/environments.js';

export type ActionStatus =
	| 'starting'
	| 'stopping'
	| 'restarting'
	| 'redeploying'
	| 'archiving'
	| 'unarchiving'
	| 'destroying'
	| 'syncing'
	| '';

export type ProjectTagOptions = { tags: ProjectTagOption[]; failedEnvironments: EnvironmentFailure[] };

/** Tag options from every shown environment, one per name, with the environments that failed. */
export async function fetchProjectTags(environments: EnvironmentRef[]): Promise<ProjectTagOptions> {
	const { values, failures } = await collectAcrossEnvironments(environments, (id, config) =>
		projectService.getProjectTagsForEnvironment(id, config)
	);
	const tags = values.flatMap((result) => result.value);
	return {
		tags: tags.filter((tag, index) => tags.findIndex((other) => other.name === tag.name) === index),
		failedEnvironments: failures
	};
}
