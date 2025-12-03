<script lang="ts">
	import * as Sheet from '$lib/components/ui/sheet/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import FormInput from '$lib/components/form/form-input.svelte';
	import SwitchWithLabel from '$lib/components/form/labeled-switch.svelte';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import GitBranchIcon from '@lucide/svelte/icons/git-branch';
	import type { GitOpsRepository } from '$lib/types/gitops.type';
	import type { GitOpsRepositoryCreateDto, GitOpsRepositoryUpdateDto } from '$lib/types/gitops.type';
	import { z } from 'zod/v4';
	import { createForm, preventDefault } from '$lib/utils/form.utils';
	import { m } from '$lib/paraglide/messages';

	type GitOpsRepositoryFormProps = {
		open: boolean;
		repositoryToEdit: GitOpsRepository | null;
		onSubmit: (detail: { repository: GitOpsRepositoryCreateDto | GitOpsRepositoryUpdateDto; isEditMode: boolean }) => void;
		isLoading: boolean;
	};

	let { open = $bindable(false), repositoryToEdit = $bindable(), onSubmit, isLoading }: GitOpsRepositoryFormProps = $props();

	let isEditMode = $derived(!!repositoryToEdit);

	const formSchema = z.object({
		url: z.string().min(1, 'Repository URL is required'),
		branch: z.string().optional(),
		username: z.string().optional(),
		token: z.string().optional(),
		composePath: z.string().min(1, 'Compose file path is required'),
		projectName: z.string().optional(),
		description: z.string().optional(),
		autoSync: z.boolean().default(false),
		syncInterval: z.number().min(1).optional(),
		enabled: z.boolean().default(true)
	});

	let formData = $derived({
		url: open && repositoryToEdit ? repositoryToEdit.url : '',
		branch: open && repositoryToEdit ? repositoryToEdit.branch : 'main',
		username: open && repositoryToEdit ? repositoryToEdit.username : '',
		token: '',
		composePath: open && repositoryToEdit ? repositoryToEdit.composePath : 'docker-compose.yml',
		projectName: open && repositoryToEdit ? repositoryToEdit.projectName || '' : '',
		description: open && repositoryToEdit ? repositoryToEdit.description || '' : '',
		autoSync: open && repositoryToEdit ? (repositoryToEdit.autoSync ?? false) : false,
		syncInterval: open && repositoryToEdit ? repositoryToEdit.syncInterval : 60,
		enabled: open && repositoryToEdit ? (repositoryToEdit.enabled ?? true) : true
	});

	let { inputs, ...form } = $derived(createForm<typeof formSchema>(formSchema, formData));

	function handleSubmit() {
		const data = form.validate();
		if (!data) return;
		onSubmit({ repository: data, isEditMode });
	}
</script>

<Sheet.Root bind:open>
	<Sheet.Content class="p-6">
		<Sheet.Header class="space-y-3 border-b pb-6">
			<div class="flex items-center gap-3">
				<div class="bg-primary/10 flex size-10 shrink-0 items-center justify-center rounded-lg">
					<GitBranchIcon class="text-primary size-5" />
				</div>
				<div>
					<Sheet.Title class="text-xl font-semibold">
						{isEditMode ? 'Edit GitOps Repository' : 'Add GitOps Repository'}
					</Sheet.Title>
					<Sheet.Description class="text-muted-foreground mt-1 text-sm">
						{isEditMode
							? 'Update the GitOps repository configuration'
							: 'Configure a Git repository containing Docker Compose files'}
					</Sheet.Description>
				</div>
			</div>
		</Sheet.Header>
		<form onsubmit={preventDefault(handleSubmit)} class="grid gap-4 py-4">
			<div class="space-y-4">
				<h3 class="text-sm leading-none font-medium">Repository Details</h3>
				<div class="grid gap-4">
					<FormInput
						label="Repository URL"
						type="text"
						placeholder="https://github.com/user/repo.git"
						description="The Git repository URL (HTTPS format)"
						bind:input={$inputs.url}
					/>
					<div class="grid grid-cols-2 gap-4">
						<FormInput
							label="Branch"
							type="text"
							placeholder="main"
							description="Git branch to use"
							bind:input={$inputs.branch}
						/>
						<FormInput
							label="Compose File Path"
							type="text"
							placeholder="docker-compose.yml"
							description="Path to compose file"
							bind:input={$inputs.composePath}
						/>
					</div>
					<FormInput
						label={m.gitops_project_name()}
						type="text"
						placeholder={m.gitops_project_name_placeholder()}
						description={m.gitops_project_name_description()}
						bind:input={$inputs.projectName}
					/>
					<FormInput
						label={m.common_description()}
						type="text"
						placeholder="Production environment compose files"
						bind:input={$inputs.description}
					/>
				</div>
			</div>

			<div class="space-y-4">
				<h3 class="text-sm leading-none font-medium">Authentication (Optional)</h3>
				<div class="grid grid-cols-2 gap-4">
					<FormInput
						label={m.common_username()}
						type="text"
						placeholder="github_username"
						description="Git username"
						bind:input={$inputs.username}
					/>
					<FormInput
						label="Personal Access Token"
						type="password"
						placeholder={isEditMode ? 'Leave blank to keep current' : 'ghp_xxxxxxxxxxxxx'}
						description="Git token or password"
						bind:input={$inputs.token}
					/>
				</div>
			</div>

			<div class="space-y-4">
				<h3 class="text-sm leading-none font-medium">Sync Settings</h3>
				<div class="grid gap-4">
					<div class="grid grid-cols-2 gap-4">
						<FormInput
							label="Sync Interval (minutes)"
							type="number"
							placeholder="60"
							description="Check frequency"
							bind:input={$inputs.syncInterval}
						/>
						<div class="flex flex-col justify-end pb-2">
							<SwitchWithLabel
								id="autoSyncSwitch"
								label="Auto Sync"
								description="Automatically deploy changes"
								bind:checked={$inputs.autoSync.value}
							/>
						</div>
					</div>
					<SwitchWithLabel
						id="isEnabledSwitch"
						label={m.common_enabled()}
						description="Enable or disable this GitOps repository"
						bind:checked={$inputs.enabled.value}
					/>
				</div>
			</div>

			<Sheet.Footer class="flex flex-row gap-2 pt-4">
				<Button
					type="button"
					class="arcane-button-cancel flex-1"
					variant="outline"
					onclick={() => (open = false)}
					disabled={isLoading}
				>
					{m.common_cancel()}
				</Button>

				<Button type="submit" class="arcane-button-create flex-1" disabled={isLoading}>
					{#if isLoading}
						<Spinner class="mr-2 size-4" />
					{/if}
					{isEditMode ? 'Save Changes' : 'Add Repository'}
				</Button>
			</Sheet.Footer>
		</form>
	</Sheet.Content>
</Sheet.Root>
