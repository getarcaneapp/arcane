<script lang="ts">
	import * as Alert from '$lib/components/ui/alert/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button';
	import CodePanel from '../../projects/components/CodePanel.svelte';
	import { projectService } from '$lib/services/project-service';
	import { toast } from 'svelte-sonner';
	import type { Project, IncludeFile } from '$lib/types/project.type';
	import { AlertIcon, ExternalLinkIcon } from '$lib/icons';
	import * as m from '$lib/paraglide/messages';
	import { invalidateAll } from '$app/navigation';

	let {
		project,
		serviceName,
		includeFile = null
	}: {
		project: Project;
		serviceName: string;
		includeFile?: IncludeFile | null;
	} = $props();

	const sourceContent = $derived(includeFile ? includeFile.content : (project.composeContent ?? ''));

	let composeContent = $state(sourceContent);

	// Track file identity to detect context switches (different container/file)
	const fileIdentity = $derived(`${project.id}:${includeFile?.relativePath ?? 'compose.yml'}`);
	let prevFileIdentity = $state(fileIdentity);

	// Update composeContent when source changes
	// Force reset on file identity change (context switch to different file)
	// Skip update only if same file has unsaved edits (isDirty)
	let prevSourceContent = $state(sourceContent);
	const isDirty = $derived(composeContent !== sourceContent);

	$effect(() => {
		const identityChanged = fileIdentity !== prevFileIdentity;
		const sourceChanged = sourceContent !== prevSourceContent;

		if (identityChanged || (sourceChanged && !isDirty)) {
			composeContent = sourceContent;
			prevSourceContent = sourceContent;
			prevFileIdentity = fileIdentity;
		}
	});

	let panelOpen = $state(true);
	let isSaving = $state(false);

	const isReadOnly = $derived(!!project.gitOpsManagedBy);
	const fileTitle = $derived(includeFile ? includeFile.relativePath : 'compose.yml');

	async function handleSave() {
		isSaving = true;
		try {
			if (includeFile) {
				await projectService.updateProjectIncludeFile(project.id, includeFile.relativePath, composeContent);
			} else {
				await projectService.updateProject(project.id, undefined, composeContent);
			}
			await invalidateAll();
			toast.success(m.container_compose_save_success());
		} catch (err: any) {
			toast.error(err?.message ?? m.container_compose_save_failed());
		} finally {
			isSaving = false;
		}
	}
</script>

<div class="flex h-full min-h-0 flex-col gap-4 p-4">
	{#if project.gitOpsManagedBy}
		<Alert.Root variant="default">
			<AlertIcon class="size-4" />
			<Alert.Title>{m.container_compose_gitops_managed_title()}</Alert.Title>
			<Alert.Description>
				{m.container_compose_gitops_managed_description({ provider: project.gitOpsManagedBy })}
			</Alert.Description>
		</Alert.Root>
	{/if}

	<div class="bg-muted flex items-start gap-2 rounded-lg border px-4 py-3 text-sm">
		<span>
			{@html isReadOnly
				? m.container_compose_viewing_info({ file: fileTitle, project: project.name, service: serviceName })
				: m.container_compose_editing_info({ file: fileTitle, project: project.name, service: serviceName })}
		</span>
	</div>

	<div class="flex min-h-0 flex-1 flex-col">
		<CodePanel
			title={fileTitle}
			bind:open={panelOpen}
			language="yaml"
			bind:value={composeContent}
			readOnly={isReadOnly}
			fileId="container-compose-{project.id}{includeFile ? `-${includeFile.relativePath}` : ''}"
		/>
	</div>

	<div class="flex shrink-0 items-center gap-2">
		{#if !isReadOnly}
			<ArcaneButton action="save" loading={isSaving} onclick={handleSave} />
		{/if}
		<ArcaneButton
			action="base"
			href="/projects/{project.id}"
			icon={ExternalLinkIcon}
			customLabel={m.container_compose_view_project()}
		/>
	</div>
</div>
