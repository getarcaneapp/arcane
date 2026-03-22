<script lang="ts">
	import { ComposeEditorWrapper } from '$lib/components/compose';
	import CodePanel from '../../projects/components/CodePanel.svelte';
	import { projectService } from '$lib/services/project-service';
	import type { Project, IncludeFile } from '$lib/types/project.type';

	let {
		project,
		serviceName,
		includeFile = null,
		rootFilename = 'compose.yml'
	}: {
		project: Project;
		serviceName: string;
		includeFile?: IncludeFile | null;
		rootFilename?: string;
	} = $props();

	const sourceContent = $derived(includeFile ? includeFile.content : (project.composeContent ?? ''));

	let composeContent = $state(includeFile ? includeFile.content : (project.composeContent ?? ''));

	const isDirty = $derived(composeContent !== sourceContent);

	let panelOpen = $state(true);

	const fileTitle = $derived(includeFile ? includeFile.relativePath : rootFilename);

	async function save() {
		if (includeFile) {
			await projectService.updateProjectIncludeFile(project.id, includeFile.relativePath, composeContent);
		} else {
			await projectService.updateProject(project.id, undefined, composeContent);
		}
	}
</script>

<ComposeEditorWrapper
	projectId={project.id}
	projectName={project.name}
	gitOpsManagedBy={project.gitOpsManagedBy}
	{fileTitle}
	{serviceName}
	{isDirty}
	onSave={save}
>
	<CodePanel
		title={fileTitle}
		bind:open={panelOpen}
		language="yaml"
		bind:value={composeContent}
		readOnly={!!project.gitOpsManagedBy}
		fileId="container-compose-{project.id}{includeFile ? `-${includeFile.relativePath.replace(/[^a-zA-Z0-9_-]/g, '-')}` : ''}"
	/>
</ComposeEditorWrapper>
