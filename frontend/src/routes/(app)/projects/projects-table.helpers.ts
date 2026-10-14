import { projectService } from '#lib/services/project-service.js';
import type { EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
import type { ProjectTagOption } from '#lib/types/swarm.js';
import { readAcrossEnvironments, type EnvironmentFailure } from '#lib/utils/environments.js';

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
	const { value, failures } = await readAcrossEnvironments(environments, (scope, config) =>
		projectService.getProjectTagsForEnvironment(scope, config)
	);
	return { tags: value, failedEnvironments: failures };
}
