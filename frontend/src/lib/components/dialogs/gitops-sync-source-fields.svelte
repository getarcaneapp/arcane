<script lang="ts">
	import type { Snippet } from 'svelte';

	import FormInput from '#lib/components/form/form-input.svelte';
	import { Label } from '#lib/components/ui/label/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { BranchInfo, GitRepository } from '#lib/types/automation.js';

	import type { GitOpsSyncBrowseTarget, GitOpsSyncFormState } from './gitops-sync-form.svelte';

	type Props = {
		form: GitOpsSyncFormState;
		repositories: GitRepository[];
		branches: BranchInfo[];
		loadingBranches: boolean;
		selectedRepository: { value: string; label: string } | undefined;
		selectedBranch: string;
		showComposePath: boolean;
		composePlaceholder: string;
		browseButton: Snippet<[GitOpsSyncBrowseTarget]>;
	};

	let {
		form,
		repositories,
		branches,
		loadingBranches,
		selectedRepository,
		selectedBranch,
		showComposePath,
		composePlaceholder,
		browseButton
	}: Props = $props();

	const inputs = $derived(form.inputs);
</script>

<div class="grid gap-3 sm:grid-cols-content-aside-48">
	<div class="space-y-2">
		<Label for="repository">{m.git_sync_repository()}</Label>
		<Select.Root
			type="single"
			value={selectedRepository?.value}
			onValueChange={(v) => {
				if (v && repositories.some((r) => r.id === v)) {
					inputs.repositoryId.value = v;
				}
			}}
		>
			<Select.Trigger id="repository" class="w-full" aria-invalid={inputs.repositoryId.error ? 'true' : undefined}>
				<span>{selectedRepository?.label ?? m.common_select_placeholder()}</span>
			</Select.Trigger>
			<Select.Content class="w-(--bits-select-anchor-width)">
				{#each repositories as repo (repo.id)}
					<Select.Item value={repo.id}><span class="block min-w-0 truncate">{repo.name}</span></Select.Item>
				{/each}
			</Select.Content>
		</Select.Root>
		{#if inputs.repositoryId.error}
			<p class="mt-1 text-sm text-destructive">{inputs.repositoryId.error}</p>
		{/if}
	</div>

	<div class="space-y-2">
		<Label for="branch">{m.git_sync_branch()}</Label>
		{#if loadingBranches}
			<div class="flex h-10 items-center gap-2 rounded-md border px-3">
				<Spinner class="size-4" />
				<span class="text-sm text-muted-foreground">{m.common_loading()}</span>
			</div>
		{:else if branches.length > 0}
			<Select.Root
				type="single"
				value={selectedBranch}
				onValueChange={(v) => {
					if (v) {
						inputs.branch.value = v;
					}
				}}
			>
				<Select.Trigger id="branch" class="w-full" aria-invalid={inputs.branch.error ? 'true' : undefined}>
					<span>{selectedBranch || m.common_select_placeholder()}</span>
				</Select.Trigger>
				<Select.Content class="w-(--bits-select-anchor-width)">
					{#each branches as branch (branch.name)}
						<Select.Item value={branch.name}>
							<span class="block min-w-0 truncate">
								{branch.name}
								{#if branch.isDefault}
									<span class="ml-2 text-xs text-muted-foreground">({m.common_default()})</span>
								{/if}
							</span>
						</Select.Item>
					{/each}
				</Select.Content>
			</Select.Root>
		{:else}
			<FormInput type="text" placeholder="main" bind:input={inputs.branch} />
		{/if}
		{#if inputs.branch.error}
			<p class="mt-1 text-sm text-destructive">{inputs.branch.error}</p>
		{/if}
	</div>
</div>

{#if showComposePath}
	<div class="space-y-2">
		<Label for="composePath">{m.git_sync_compose_path()}</Label>
		<div class="flex gap-2">
			<div class="flex-1">
				<FormInput type="text" placeholder={composePlaceholder} bind:input={inputs.composePath} />
			</div>
			{@render browseButton('compose')}
		</div>
	</div>
{/if}
