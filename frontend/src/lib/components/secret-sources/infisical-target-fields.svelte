<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';

	import SwitchWithLabel from '#lib/components/form/labeled-switch.svelte';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { AlertIcon, FolderOpenIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { secretSourceService } from '#lib/services/secret-source-service.js';
	import type { InfisicalTarget } from '#lib/types/secret-source.js';

	let {
		sourceId,
		target = $bindable()
	}: {
		sourceId: string;
		target: InfisicalTarget;
	} = $props();

	// The project list fills the pickers. Identities that cannot list projects
	// fall back to typing the IDs.
	const projectsQuery = createQuery(() => ({
		queryKey: queryKeys.secretSources.browse(sourceId, 'projects'),
		queryFn: () => secretSourceService.browse(sourceId, { kind: 'projects' }),
		enabled: !!sourceId,
		retry: false
	}));
	const projects = $derived(projectsQuery.data ?? []);
	const manualEntry = $derived(projectsQuery.isError || (projectsQuery.isSuccess && projects.length === 0));
	const selectedProject = $derived(projects.find((project) => project.id === target.projectId));
	const environments = $derived(selectedProject?.options ?? []);

	const foldersQuery = createQuery(() => ({
		queryKey: queryKeys.secretSources.browse(sourceId, 'folders', target.projectId, target.environment, target.secretPath),
		queryFn: () =>
			secretSourceService.browse(sourceId, {
				kind: 'folders',
				projectId: target.projectId,
				environment: target.environment,
				path: target.secretPath
			}),
		enabled: !!sourceId && !!target.projectId && !!target.environment,
		retry: false
	}));

	function normalizePath(path: string): string {
		const trimmed = path.trim().replace(/^\/+|\/+$/g, '');
		return trimmed ? `/${trimmed}` : '/';
	}

	function enterFolder(folder: string) {
		const base = normalizePath(target.secretPath);
		target.secretPath = base === '/' ? `/${folder}` : `${base}/${folder}`;
	}

	function leaveFolder() {
		const parts = normalizePath(target.secretPath).split('/').filter(Boolean);
		parts.pop();
		target.secretPath = parts.length ? `/${parts.join('/')}` : '/';
	}
</script>

{#if projectsQuery.isPending}
	<div class="flex items-center gap-2 text-sm text-muted-foreground"><Spinner class="size-4" />{m.common_loading()}</div>
{:else if manualEntry}
	<Alert.Root variant="info" icon={AlertIcon} description={m.project_secrets_enter_ids_description()} />
	<div class="grid gap-4 sm:grid-cols-2">
		<div class="space-y-2">
			<Label for="infisical-project">{m.project_secrets_infisical_project_id()}</Label>
			<Input id="infisical-project" mono bind:value={target.projectId} />
		</div>
		<div class="space-y-2">
			<Label for="infisical-environment">{m.project_secrets_environment_slug()}</Label>
			<Input id="infisical-environment" mono placeholder="prod" bind:value={target.environment} />
		</div>
	</div>
{:else}
	<div class="grid gap-4 sm:grid-cols-2">
		<div class="space-y-2">
			<Label for="infisical-project">{m.project_secrets_infisical_project()}</Label>
			<Select.Root
				type="single"
				value={target.projectId}
				onValueChange={(value) => {
					const project = projects.find((candidate) => candidate.id === value);
					const onlyEnvironment = project?.options?.length === 1 ? (project.options[0]?.id ?? '') : '';
					target = { ...target, projectId: value, environment: onlyEnvironment, secretPath: '/' };
				}}
			>
				<Select.Trigger id="infisical-project" class="w-full">
					<span>{selectedProject?.name ?? m.project_secrets_select_project()}</span>
				</Select.Trigger>
				<Select.Content>
					{#each projects as project (project.id)}
						<Select.Item value={project.id}>{project.name}</Select.Item>
					{/each}
				</Select.Content>
			</Select.Root>
		</div>
		<div class="space-y-2">
			<Label for="infisical-environment">{m.project_secrets_environment()}</Label>
			<Select.Root
				type="single"
				value={target.environment}
				disabled={!selectedProject}
				onValueChange={(value) => {
					target = { ...target, environment: value, secretPath: '/' };
				}}
			>
				<Select.Trigger id="infisical-environment" class="w-full">
					<span>
						{environments.find((environment) => environment.id === target.environment)?.name ??
							m.project_secrets_select_environment()}
					</span>
				</Select.Trigger>
				<Select.Content>
					{#each environments as environment (environment.id)}
						<Select.Item value={environment.id}>{environment.name}</Select.Item>
					{/each}
				</Select.Content>
			</Select.Root>
		</div>
	</div>
{/if}

{#if target.projectId && target.environment}
	<div class="space-y-2">
		<Label for="infisical-path">{m.project_secrets_path()}</Label>
		<Input id="infisical-path" mono bind:value={target.secretPath} />
		<div class="flex flex-wrap items-center gap-1.5">
			{#if normalizePath(target.secretPath) !== '/'}
				<Button type="button" size="sm" variant="ghost" class="h-7" onclick={leaveFolder}
					><span class="font-mono">..</span></Button
				>
			{/if}
			{#if foldersQuery.isPending}
				<Spinner class="size-4" />
			{:else}
				{#each foldersQuery.data ?? [] as folder (folder.id)}
					<Button type="button" size="sm" variant="outline" class="h-7" onclick={() => enterFolder(folder.name)}>
						<FolderOpenIcon class="size-3.5" />
						<span class="font-mono">{folder.name}</span>
					</Button>
				{/each}
			{/if}
		</div>
	</div>
{/if}

<div class="grid gap-3">
	<SwitchWithLabel id="infisical-imports" label={m.project_secrets_include_imports()} bind:checked={target.includeImports} />
	<SwitchWithLabel
		id="infisical-references"
		label={m.project_secrets_expand_references()}
		bind:checked={target.expandReferences}
	/>
</div>
