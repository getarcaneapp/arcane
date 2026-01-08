<script lang="ts">
	import { ResponsiveDialog } from '$lib/components/ui/responsive-dialog/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import { toast } from 'svelte-sonner';
	import type { ImportGitOpsSyncRequest } from '$lib/types/gitops.type';
	import { m } from '$lib/paraglide/messages';
	import { preventDefault } from '$lib/utils/form.utils';

	type GitOpsImportFormProps = {
		open: boolean;
		onSubmit: (data: ImportGitOpsSyncRequest[]) => void;
		isLoading: boolean;
	};

	let { open = $bindable(false), onSubmit, isLoading }: GitOpsImportFormProps = $props();

	let jsonContent = $state('');
	let error = $state<string | null>(null);

	function handleSubmit() {
		error = null;
		try {
			if (!jsonContent.trim()) {
				error = m.git_sync_import_json_required();
				return;
			}
			const data = JSON.parse(jsonContent);
			if (!Array.isArray(data)) {
				error = m.git_sync_import_json_invalid_array();
				return;
			}
			// Basic validation of structure
			if (data.length === 0) {
				error = m.git_sync_import_json_empty();
				return;
			}

			// Optional: validate each item has required fields
			for (let i = 0; i < data.length; i++) {
				const item = data[i];
				if (!item.syncName || !item.gitRepo || !item.branch || !item.dockerComposePath) {
					error = m.git_sync_import_missing_fields({ index: i });
					return;
				}
			}

			onSubmit(data as ImportGitOpsSyncRequest[]);
		} catch (e) {
			error = m.git_sync_import_invalid_json({ error: e instanceof Error ? e.message : String(e) });
		}
	}

	function handleFileUpload(event: Event) {
		const input = event.target as HTMLInputElement;
		if (input.files && input.files[0]) {
			const file = input.files[0];
			const reader = new FileReader();
			reader.onload = (e) => {
				if (e.target?.result) {
					jsonContent = e.target.result as string;
				}
			};
			reader.readAsText(file);
		}
	}
</script>

<ResponsiveDialog
	bind:open
	title={m.git_sync_import_title()}
	description={m.git_sync_import_description()}
	contentClass="sm:max-w-2xl"
>
	{#snippet children()}
		<div class="grid gap-y-3 py-4">
			<div class="space-y-1.5">
				<Label for="json-file">{m.git_sync_import_upload_label()}</Label>
				<input
					id="json-file"
					type="file"
					accept=".json"
					onchange={handleFileUpload}
					class="border-input bg-background ring-offset-background placeholder:text-muted-foreground focus-visible:ring-ring flex h-10 w-full rounded-md border px-3 py-2 text-sm file:border-0 file:bg-transparent file:text-sm file:font-medium focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-50"
				/>
			</div>

			<div class="space-y-1.5">
				<Label for="json-content">{m.git_sync_import_content_label()}</Label>
				<Textarea
					id="json-content"
					bind:value={jsonContent}
					placeholder={`[\n  {\n    "syncName": "example-sync",\n    "gitRepo": "my-repo",\n    "branch": "main",\n    "dockerComposePath": "docker-compose.yml",\n    "autoSync": true,\n    "syncInterval": 10,\n    "enabled": true\n  }\n]`}
					class="h-[300px] font-mono text-xs"
				/>
				{#if error}
					<p class="text-sm text-red-500">{error}</p>
				{/if}
			</div>
		</div>
	{/snippet}

	{#snippet footer()}
		<Button
			type="button"
			class="arcane-button-cancel flex-1"
			variant="outline"
			onclick={() => (open = false)}
			disabled={isLoading}
		>
			{m.common_cancel()}
		</Button>

		<Button onclick={handleSubmit} class="arcane-button-create flex-1" disabled={isLoading}>
			{#if isLoading}
				<Spinner class="mr-2 size-4" />
			{/if}
			{m.git_sync_import_button()}
		</Button>
	{/snippet}
</ResponsiveDialog>
