<script lang="ts">
	import CodePanel from '#lib/components/code-panel.svelte';
	import RowListEditor from '#lib/components/form/row-list-editor.svelte';
	import { Input } from '#lib/components/ui/input/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { PodmanQuadletFile } from '#lib/types/podman.js';
	import { workspaceFileLanguage } from '#lib/utils/workspace-files.js';

	let {
		files = $bindable([]),
		disabled = false
	}: {
		files?: PodmanQuadletFile[];
		disabled?: boolean;
	} = $props();
</script>

<RowListEditor bind:rows={files} createRow={() => ({ name: '', content: '' })} {disabled} addLabel={m.podman_quadlet_add_file()}>
	{#snippet children(file)}
		<div class="min-w-0 flex-1 space-y-2">
			<Input placeholder={m.podman_quadlet_file_name_placeholder()} bind:value={file.name} {disabled} />
			<CodePanel
				title={file.name || m.podman_quadlet_extra_file()}
				language={workspaceFileLanguage(file.name)}
				validationMode="none"
				bind:value={file.content}
				readOnly={disabled}
				autoHeight
			/>
		</div>
	{/snippet}
</RowListEditor>
