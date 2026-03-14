<script lang="ts">
	import * as Alert from '$lib/components/ui/alert/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button';
	import CodePanel from '../../projects/components/CodePanel.svelte';
	import { projectService } from '$lib/services/project-service';
	import { toast } from 'svelte-sonner';
	import type { Project, IncludeFile } from '$lib/types/project.type';
	import { AlertIcon, ExternalLinkIcon } from '$lib/icons';
	import * as m from '$lib/paraglide/messages';

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

	// Update composeContent when source changes (e.g., switching containers)
	// Only if there are no unsaved edits
	let prevSourceContent = $state(sourceContent);
	const isDirty = $derived(composeContent !== sourceContent);
	
	$effect(() => {
		if (sourceContent !== prevSourceContent && !isDirty) {
			composeContent = sourceContent;
			prevSourceContent = sourceContent;
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
				This project is managed by GitOps (<strong>{project.gitOpsManagedBy}</strong>). The compose file is read-only and can only be changed via your Git repository.
			</Alert.Description>
		</Alert.Root>
	{/if}

	<div class="bg-muted flex items-start gap-2 rounded-lg border px-4 py-3 text-sm">
		<span>
			{isReadOnly ? 'Viewing' : 'Editing'} <strong>{fileTitle}</strong> for project
			<a href="/projects/{project.id}" class="text-primary font-medium hover:underline">{project.name}</a>.
			This container runs as the <strong>{serviceName}</strong> service.
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
