<script lang="ts">
	import * as Sheet from '$lib/components/ui/sheet/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import FormInput from '$lib/components/form/form-input.svelte';
	import SwitchWithLabel from '$lib/components/form/labeled-switch.svelte';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import * as Select from '$lib/components/ui/select/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import type { GitOpsSync, GitOpsSyncCreateDto, GitOpsSyncUpdateDto } from '$lib/types/gitops.type';
	import { gitRepositoryService } from '$lib/services/git-repository-service';
	import { projectService } from '$lib/services/project-service';
	import { z } from 'zod/v4';
	import { createForm, preventDefault } from '$lib/utils/form.utils';
	import { m } from '$lib/paraglide/messages';
	import { onMount } from 'svelte';

	type GitOpsSyncFormProps = {
		open: boolean;
		syncToEdit: GitOpsSync | null;
		onSubmit: (detail: { sync: GitOpsSyncCreateDto | GitOpsSyncUpdateDto; isEditMode: boolean }) => void;
		isLoading: boolean;
	};

	let { open = $bindable(false), syncToEdit = $bindable(), onSubmit, isLoading }: GitOpsSyncFormProps = $props();

	let isEditMode = $derived(!!syncToEdit);
	let repositories = $state<any[]>([]);
	let projects = $state<any[]>([]);
	let loadingData = $state(true);

	const formSchema = z.object({
		name: z.string().min(1, m.common_name_required()),
		repositoryId: z.string().min(1, m.common_required()),
		branch: z.string().min(1, m.common_required()),
		composePath: z.string().min(1, m.common_required()),
		projectId: z.string().min(1, m.common_required()),
		autoSync: z.boolean().default(true),
		syncInterval: z.number().min(1).default(5),
		enabled: z.boolean().default(true)
	});

	let formData = $derived({
		name: open && syncToEdit ? syncToEdit.name : '',
		repositoryId: open && syncToEdit ? syncToEdit.repositoryId : '',
		branch: open && syncToEdit ? syncToEdit.branch : 'main',
		composePath: open && syncToEdit ? syncToEdit.composePath : 'docker-compose.yml',
		projectId: open && syncToEdit ? syncToEdit.projectId : '',
		autoSync: open && syncToEdit ? (syncToEdit.autoSync ?? true) : true,
		syncInterval: open && syncToEdit ? (syncToEdit.syncInterval ?? 5) : 5,
		enabled: open && syncToEdit ? (syncToEdit.enabled ?? true) : true
	});

	let { inputs, ...form } = $derived(createForm<typeof formSchema>(formSchema, formData));

	let selectedRepository = $state<{ value: string; label: string } | undefined>(undefined);
	let selectedProject = $state<{ value: string; label: string } | undefined>(undefined);

	async function loadData() {
		loadingData = true;
		try {
			const [reposResult, projectsResult] = await Promise.all([
				gitRepositoryService.getRepositories({ pagination: { page: 1, limit: 100 } }),
				projectService.getProjects({ pagination: { page: 1, limit: 100 } })
			]);
			repositories = reposResult.data || [];
			projects = projectsResult.data || [];

			if (syncToEdit) {
				const repo = repositories.find((r) => r.id === syncToEdit.repositoryId);
				if (repo) {
					selectedRepository = { value: repo.id, label: repo.name };
				}
				const proj = projects.find((p) => p.id === syncToEdit.projectId);
				if (proj) {
					selectedProject = { value: proj.id, label: proj.name };
				}
			}
		} catch (error) {
			console.error('Failed to load data:', error);
		} finally {
			loadingData = false;
		}
	}

	$effect(() => {
		if (open) {
			loadData();
		}
	});

	function handleSubmit() {
		const data = form.validate();
		if (!data) return;

		const payload: any = {
			name: data.name,
			repositoryId: selectedRepository?.value || data.repositoryId,
			branch: data.branch,
			composePath: data.composePath,
			projectId: selectedProject?.value || data.projectId,
			autoSync: data.autoSync,
			syncInterval: data.syncInterval,
			enabled: data.enabled
		};

		onSubmit({ sync: payload, isEditMode });
	}
</script>

<Sheet.Root bind:open>
	<Sheet.Content class="p-6">
		<Sheet.Header class="space-y-3 border-b pb-6">
			<div class="flex items-center gap-3">
				<div class="bg-primary/10 flex size-10 shrink-0 items-center justify-center rounded-lg">
					<RefreshCwIcon class="text-primary size-5" />
				</div>
				<div>
					<Sheet.Title class="text-xl font-semibold">
						{isEditMode ? m.gitops_sync_edit_title() : m.gitops_sync_add_title()}
					</Sheet.Title>
					<Sheet.Description class="text-muted-foreground mt-1 text-sm">
						{isEditMode ? m.common_edit_description() : m.common_add_description()}
					</Sheet.Description>
				</div>
			</div>
		</Sheet.Header>

		{#if loadingData}
			<div class="flex items-center justify-center py-8">
				<Spinner class="size-6" />
			</div>
		{:else}
			<form onsubmit={preventDefault(handleSubmit)} class="grid gap-4 py-4">
				<FormInput label={m.gitops_sync_name()} type="text" placeholder={m.common_name_placeholder()} bind:input={$inputs.name} />

				<div class="space-y-2">
					<Label for="repository">{m.gitops_sync_repository()}</Label>
					<Select.Root
						type="single"
						value={selectedRepository?.value}
						onValueChange={(v) => {
							if (v) {
								const repo = repositories.find((r) => r.id === v);
								if (repo) {
									selectedRepository = { value: repo.id, label: repo.name };
									$inputs.repositoryId.value = v;
								}
							}
						}}
					>
						<Select.Trigger id="repository">
							<span>{selectedRepository?.label ?? m.common_select_placeholder()}</span>
						</Select.Trigger>
						<Select.Content>
							{#each repositories as repo}
								<Select.Item value={repo.id}>{repo.name}</Select.Item>
							{/each}
						</Select.Content>
					</Select.Root>
				</div>

				<FormInput label={m.gitops_sync_branch()} type="text" placeholder="main" bind:input={$inputs.branch} />

				<FormInput
					label={m.gitops_sync_compose_path()}
					type="text"
					placeholder="docker-compose.yml"
					bind:input={$inputs.composePath}
				/>

				<div class="space-y-2">
					<Label for="project">{m.gitops_sync_project()}</Label>
					<Select.Root
						type="single"
						value={selectedProject?.value}
						onValueChange={(v) => {
							if (v) {
								const proj = projects.find((p) => p.id === v);
								if (proj) {
									selectedProject = { value: proj.id, label: proj.name };
									$inputs.projectId.value = v;
								}
							}
						}}
					>
						<Select.Trigger id="project">
							<span>{selectedProject?.label ?? m.common_select_placeholder()}</span>
						</Select.Trigger>
						<Select.Content>
							{#each projects as project}
								<Select.Item value={project.id}>{project.name}</Select.Item>
							{/each}
						</Select.Content>
					</Select.Root>
				</div>

				<SwitchWithLabel
					id="autoSyncSwitch"
					label={m.gitops_sync_auto_sync()}
					description={m.common_auto_sync_description()}
					bind:checked={$inputs.autoSync.value}
				/>

				<FormInput label={m.gitops_sync_sync_interval()} type="number" placeholder="5" bind:input={$inputs.syncInterval} />

				<SwitchWithLabel
					id="isEnabledSwitch"
					label={m.common_enabled()}
					description={m.common_enabled_description()}
					bind:checked={$inputs.enabled.value}
				/>

				<Sheet.Footer class="flex flex-row gap-2">
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
						{isEditMode ? m.common_save_changes() : m.common_add_button({ resource: m.resource_sync_cap() })}
					</Button>
				</Sheet.Footer>
			</form>
		{/if}
	</Sheet.Content>
</Sheet.Root>
