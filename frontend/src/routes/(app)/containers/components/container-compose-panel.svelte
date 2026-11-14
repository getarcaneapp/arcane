<script lang="ts">
	import CodePanel from '#lib/components/code-panel.svelte';
	import { ComposeEditorWrapper } from '#lib/components/compose/index.js';
	import { projectService } from '#lib/services/project-service.js';
	import { projectWorkspaceService } from '#lib/services/project-workspace-service.js';
	import type { Project, ProjectIncludeFile } from '#lib/types/project.js';
	import { environmentHref } from '#lib/utils/environments.js';

	let {
		project,
		environmentId,
		serviceName,
		includeFile = null,
		rootFilename = 'compose.yml'
	}: {
		project: Project;
		environmentId: string;
		serviceName: string;
		includeFile?: ProjectIncludeFile | null;
		rootFilename?: string;
	} = $props();

	const gitOwnsFiles = $derived(!!project.gitOpsManagedBy && project.gitOpsSyncMode !== 'bidirectional');

	const sourceContent = $derived(includeFile ? (includeFile.content ?? '') : (project.composeContent ?? ''));
	// This component is keyed by compose source identity in the parent route, so capturing the initial source is intentional.
	// svelte-ignore state_referenced_locally
	let composeContent = $state(sourceContent);

	const isDirty = $derived(composeContent !== sourceContent);

	let panelOpen = $state(true);

	const fileTitle = $derived(includeFile ? includeFile.relativePath : rootFilename);

	async function save() {
		if (includeFile) {
			await projectWorkspaceService.updateWorkspace(
				project.id,
				{ fileChanges: [{ operation: 'update_file', relativePath: includeFile.relativePath, uploadIndex: 0 }] },
				[new File([composeContent], includeFile.relativePath, { type: 'text/yaml' })],
				environmentId
			);
		} else {
			await projectService.updateProject(project.id, { composeContent }, environmentId);
		}
	}
</script>

<ComposeEditorWrapper
	projectHref={environmentHref(`/projects/${project.id}`, environmentId)}
	projectName={project.name}
	gitOpsManagedBy={gitOwnsFiles ? project.gitOpsManagedBy : undefined}
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
		readOnly={gitOwnsFiles}
		fileId="container-compose-{project.id}{includeFile ? `-${includeFile.relativePath.replace(/[^a-zA-Z0-9_-]/g, '-')}` : ''}"
	/>
</ComposeEditorWrapper>
