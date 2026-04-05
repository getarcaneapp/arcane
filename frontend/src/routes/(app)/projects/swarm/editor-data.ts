import { queryKeys } from '$lib/query/query-keys';
import { swarmService } from '$lib/services/swarm-service';
import { templateService } from '$lib/services/template-service';
import { environmentStore } from '$lib/stores/environment.store.svelte';
import type { Template, EnvVariable } from '$lib/types/template.type';
import type { SwarmStackProjectRuntimeState } from '$lib/types/swarm.type';
import type { QueryClient } from '@tanstack/svelte-query';

export type SwarmProjectEditorPageData = {
	composeTemplates: Template[];
	defaultTemplate: string;
	envTemplate: string;
	selectedTemplate: Template | null;
	globalVariables: EnvVariable[];
	stackName: string | null;
	runtimeState: SwarmStackProjectRuntimeState;
	serviceCount: number;
	isExistingProject: boolean;
	allowNameEdit: boolean;
	createdAt?: string | null;
	updatedAt?: string | null;
	backHref: string;
};

async function loadTemplateDependencies(queryClient: QueryClient, templateId: string | null) {
	const [allTemplates, defaultTemplates, selectedTemplate, globalVariables] = await Promise.all([
		queryClient
			.fetchQuery({
				queryKey: queryKeys.templates.allTemplates(),
				queryFn: () => templateService.getAllTemplates()
			})
			.catch((err) => {
				console.warn('Failed to load templates:', err);
				return [];
			}),
		queryClient
			.fetchQuery({
				queryKey: queryKeys.templates.defaults(),
				queryFn: () => templateService.getDefaultTemplates()
			})
			.catch((err) => {
				console.warn('Failed to load default templates:', err);
				return { composeTemplate: '', swarmStackTemplate: '', swarmStackEnvTemplate: '', envTemplate: '' };
			}),
		templateId
			? queryClient
					.fetchQuery({
						queryKey: queryKeys.templates.content(templateId),
						queryFn: () => templateService.getTemplateContent(templateId)
					})
					.catch((err) => {
						console.warn('Failed to load selected template:', err);
						return null;
					})
			: Promise.resolve(null),
		queryClient
			.fetchQuery({
				queryKey: queryKeys.templates.globalVariables(),
				queryFn: () => templateService.getGlobalVariables()
			})
			.catch((err) => {
				console.warn('Failed to load global variables:', err);
				return [];
			})
	]);

	return {
		composeTemplates: allTemplates,
		defaultTemplates,
		selectedTemplate,
		globalVariables
	};
}

export async function loadNewSwarmProjectEditorData(
	queryClient: QueryClient,
	templateId: string | null
): Promise<SwarmProjectEditorPageData> {
	const { composeTemplates, defaultTemplates, selectedTemplate, globalVariables } = await loadTemplateDependencies(
		queryClient,
		templateId
	);

	return {
		composeTemplates,
		defaultTemplate: selectedTemplate?.content || defaultTemplates.swarmStackTemplate,
		envTemplate: selectedTemplate?.envContent || defaultTemplates.swarmStackEnvTemplate,
		selectedTemplate: selectedTemplate?.template || null,
		globalVariables,
		stackName: null,
		runtimeState: 'down',
		serviceCount: 0,
		isExistingProject: false,
		allowNameEdit: true,
		createdAt: null,
		updatedAt: null,
		backHref: '/projects'
	};
}

export async function loadExistingSwarmProjectEditorData(
	queryClient: QueryClient,
	stackName: string,
	templateId: string | null
): Promise<SwarmProjectEditorPageData> {
	const envId = await environmentStore.getCurrentEnvironmentId();
	const { composeTemplates, defaultTemplates, selectedTemplate, globalVariables } = await loadTemplateDependencies(
		queryClient,
		templateId
	);

	try {
		const stackProject = await queryClient.fetchQuery({
			queryKey: queryKeys.swarm.stackProjectDetail(envId, stackName),
			queryFn: () => swarmService.getStackProject(stackName)
		});

		return {
			composeTemplates,
			defaultTemplate: stackProject.composeContent,
			envTemplate: stackProject.envContent ?? '',
			selectedTemplate: selectedTemplate?.template || null,
			globalVariables,
			stackName: stackProject.name,
			runtimeState: stackProject.runtimeState,
			serviceCount: stackProject.serviceCount,
			isExistingProject: true,
			allowNameEdit: stackProject.runtimeState !== 'live',
			createdAt: stackProject.createdAt,
			updatedAt: stackProject.updatedAt,
			backHref: '/projects'
		};
	} catch (err: any) {
		if (err?.status !== 404) {
			console.warn('Failed to load saved swarm stack project:', err);
		}
	}

	let runtimeState: SwarmStackProjectRuntimeState = 'down';
	let serviceCount = 0;

	try {
		const stack = await swarmService.getStack(stackName);
		runtimeState = 'live';
		serviceCount = stack.services ?? 0;
	} catch (err: any) {
		if (err?.status !== 404) {
			runtimeState = 'unavailable';
		}
	}

	return {
		composeTemplates,
		defaultTemplate: selectedTemplate?.content || defaultTemplates.swarmStackTemplate,
		envTemplate: selectedTemplate?.envContent || defaultTemplates.swarmStackEnvTemplate,
		selectedTemplate: selectedTemplate?.template || null,
		globalVariables,
		stackName,
		runtimeState,
		serviceCount,
		isExistingProject: false,
		allowNameEdit: runtimeState !== 'live',
		createdAt: null,
		updatedAt: null,
		backHref: '/projects'
	};
}
