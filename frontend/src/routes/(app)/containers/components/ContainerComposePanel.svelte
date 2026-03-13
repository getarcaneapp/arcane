<script lang="ts">
	import * as Alert from '$lib/components/ui/alert/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button';
	import CodePanel from '../../projects/components/CodePanel.svelte';
	import { projectService } from '$lib/services/project-service';
	import { toast } from 'svelte-sonner';
	import type { Project, IncludeFile } from '$lib/types/project.type';
	import { AlertIcon, ExternalLinkIcon } from '$lib/icons';
	import { untrack } from 'svelte';

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

	let composeContent = $state(untrack(() => sourceContent));
	$effect(() => {
		composeContent = sourceContent;
	});

	let panelOpen = $state(true);
	let isSaving = $state(false);
	let saveError = $state('');

	const isReadOnly = $derived(!!project.gitOpsManagedBy);
	const fileTitle = $derived(includeFile ? includeFile.relativePath : 'compose.yml');

	async function handleSave() {
		isSaving = true;
		saveError = '';
		try {
			if (includeFile) {
				await projectService.updateProjectIncludeFile(project.id, includeFile.relativePath, composeContent);
			} else {
				await projectService.updateProject(project.id, undefined, composeContent);
			}
			toast.success('Compose file saved successfully');
		} catch (err: any) {
			saveError = err?.message ?? 'Failed to save compose file';
			toast.error(saveError as string);
		} finally {
			isSaving = false;
		}
	}
</script>

<div class="flex h-full min-h-0 flex-col gap-4 p-4">
	{#if project.gitOpsManagedBy}
		<Alert.Root variant="default">
			<AlertIcon class="size-4" />
			<Alert.Title>GitOps Managed — Read Only</Alert.Title>
			<Alert.Description>
				This project is managed by GitOps (<strong>{project.gitOpsManagedBy}</strong>). The compose file is read-only and can only
				be changed via your Git repository.
			</Alert.Description>
		</Alert.Root>
	{/if}

	<div class="bg-muted flex items-start gap-2 rounded-lg border px-4 py-3 text-sm">
		<span>
			Editing <strong>{fileTitle}</strong> for project
			<a href="/projects/{project.id}" class="text-primary font-medium hover:underline">{project.name}</a>. This container runs as
			the <strong>{serviceName}</strong> service.
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
		<ArcaneButton action="base" href="/projects/{project.id}" icon={ExternalLinkIcon} customLabel="View Project" />
	</div>
</div>
