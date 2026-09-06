<script lang="ts">
	import { ComposeEditorWrapper } from '#lib/components/compose/index.js';
	import CodePanel from '#lib/components/code-panel.svelte';
	import { projectService } from '#lib/services/project-service.js';
	import { projectWorkspaceService } from '#lib/services/project-workspace-service.js';
	import type { Project, IncludeFile } from '#lib/types/swarm.js';

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

	const sourceContent = $derived(includeFile ? (includeFile.content ?? '') : (project.composeContent ?? ''));
	// This component is keyed by compose source identity in the parent route, so capturing the initial source is intentional.
	// svelte-ignore state_referenced_locally
	let composeContent = $state(sourceContent);

	const isDirty = $derived(composeContent !== sourceContent);

	let panelOpen = $state(true);

	const fileTitle = $derived(includeFile ? includeFile.relativePath : rootFilename);

	async function save() {
		if (includeFile) {
			const workspace = await projectWorkspaceService.getWorkspace(project.id);
			await projectWorkspaceService.updateWorkspace(
				project.id,
				{
					fileTreeRevision: workspace.fileTreeRevision,
					fileChanges: [{ operation: 'update_file', relativePath: includeFile.relativePath, uploadIndex: 0 }]
				},
				[new File([composeContent], includeFile.relativePath, { type: 'text/yaml' })]
			);
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
