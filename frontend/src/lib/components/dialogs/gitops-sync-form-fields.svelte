<script lang="ts">
	import { resolve } from '$app/paths';
	import type { Snippet } from 'svelte';

	import FormInput from '#lib/components/form/form-input.svelte';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import * as Collapsible from '#lib/components/ui/collapsible/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import { ArrowRightIcon, CodeIcon, InfoIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';

	import type { GitOpsSyncBrowseTarget, GitOpsSyncFormState, GitOpsSyncTargetType } from './gitops-sync-form.svelte';
	import GitOpsSyncScheduleFields from './gitops-sync-schedule-fields.svelte';

	type Props = {
		form: GitOpsSyncFormState;
		isEditMode: boolean;
		selectedTargetType: GitOpsSyncTargetType;
		onTargetTypeChange: (value: GitOpsSyncTargetType) => void;
		projectOptions: { value: string; label: string }[];
		lockSyncDirectory: boolean;
		lifecycleEnabled: boolean;
		canManageLifecycle: boolean;
		hookConfigured: boolean;
		source: Snippet<[boolean]>;
		browseButton: Snippet<[GitOpsSyncBrowseTarget]>;
	};

	let {
		form,
		isEditMode,
		selectedTargetType,
		onTargetTypeChange,
		projectOptions,
		lockSyncDirectory,
		lifecycleEnabled,
		canManageLifecycle,
		hookConfigured,
		source,
		browseButton
	}: Props = $props();

	const inputs = $derived(form.inputs);

	const targetTypeOptions = [
		{ value: 'project', label: m.project() },
		{ value: 'swarm_stack', label: m.swarm_stack() }
	] satisfies { value: GitOpsSyncTargetType; label: string }[];

	const newProjectOption = '__new__';
	const deployProjectOptions = $derived([{ value: newProjectOption, label: m.create_new_project() }, ...projectOptions]);
</script>

<div class="grid gap-3 sm:grid-cols-2">
	<FormInput label={m.git_sync_name()} type="text" placeholder={m.common_name_placeholder()} bind:input={inputs.name} />

	<SelectWithLabel
		id="targetType"
		label={m.target_type()}
		value={selectedTargetType}
		options={targetTypeOptions}
		onValueChange={(value) => onTargetTypeChange(value as GitOpsSyncTargetType)}
	/>
</div>

{#if !isEditMode && selectedTargetType === 'project'}
	<SelectWithLabel
		id="deployProject"
		label={m.project()}
		description={m.link_existing_project_description()}
		value={inputs.projectId.value || newProjectOption}
		options={deployProjectOptions}
		error={inputs.projectId.error}
		onValueChange={(value) => (inputs.projectId.value = value === newProjectOption ? '' : value)}
	/>
{/if}

{@render source(true)}

<div class="divide-y divide-border/50 border-y border-border/50">
	<GitOpsSyncScheduleFields
		{form}
		switchId="autoSyncSwitch"
		intervalId="syncInterval"
		label={m.git_sync_auto_sync()}
		description={m.common_auto_sync_description()}
		showAutoSyncError
	/>

	<div class="flex items-center justify-between gap-4 py-3">
		<div class="min-w-0">
			<Label for="syncDirectorySwitch" class="mb-0">{m.git_sync_sync_files()}</Label>
			<p class="text-xs text-muted-foreground">
				{lockSyncDirectory ? m.git_sync_sync_files_locked_hint() : m.git_sync_sync_files_description()}
			</p>
			{#if inputs.syncDirectory.error}
				<p class="text-xs font-medium text-destructive">{inputs.syncDirectory.error}</p>
			{/if}
		</div>
		<Switch id="syncDirectorySwitch" bind:checked={inputs.syncDirectory.value} disabled={lockSyncDirectory} />
	</div>

	<div class="flex items-center justify-between gap-4 py-3">
		<div class="min-w-0">
			<Label for="pullImageAfterSyncSwitch" class="mb-0">{m.git_sync_pull_image_after_sync()}</Label>
			<p class="text-xs text-muted-foreground">
				{inputs.redeployAfterSync.value
					? m.git_sync_pull_image_after_sync_redundant_description()
					: m.git_sync_pull_image_after_sync_description()}
			</p>
			{#if inputs.pullImageAfterSync.error}
				<p class="text-xs font-medium text-destructive">{inputs.pullImageAfterSync.error}</p>
			{/if}
		</div>
		<Switch
			id="pullImageAfterSyncSwitch"
			bind:checked={inputs.pullImageAfterSync.value}
			disabled={inputs.redeployAfterSync.value}
		/>
	</div>

	<div class="flex items-center justify-between gap-4 py-3">
		<div class="min-w-0">
			<Label for="redeployAfterSyncSwitch" class="mb-0">{m.git_sync_redeploy_after_sync()}</Label>
			<p class="text-xs text-muted-foreground">{m.git_sync_redeploy_after_sync_description()}</p>
			{#if inputs.redeployAfterSync.error}
				<p class="text-xs font-medium text-destructive">{inputs.redeployAfterSync.error}</p>
			{/if}
		</div>
		<Switch id="redeployAfterSyncSwitch" bind:checked={inputs.redeployAfterSync.value} />
	</div>
</div>

<Collapsible.Root class="group/collapsible">
	<Collapsible.Trigger>
		{#snippet child({ props })}
			<button
				{...props}
				type="button"
				class="flex w-full items-center justify-between gap-3 py-1 text-left text-sm font-medium text-muted-foreground transition-colors hover:text-foreground"
			>
				{m.git_sync_per_sync_file_limits_title()}
				<ArrowRightIcon class="size-4 shrink-0 transition-transform group-data-[state=open]/collapsible:rotate-90" />
			</button>
		{/snippet}
	</Collapsible.Trigger>
	<Collapsible.Content>
		<div class="space-y-3 pt-3">
			<p class="text-xs text-muted-foreground">{m.git_sync_per_sync_file_limits_description()}</p>
			<div class="grid gap-3 sm:grid-cols-3">
				<FormInput
					label={m.git_sync_max_files_label()}
					type="number"
					placeholder="0"
					helpText={m.git_sync_max_files_per_sync_help()}
					bind:input={inputs.maxSyncFiles}
				/>
				<FormInput
					label={m.git_sync_max_total_size_label()}
					type="number"
					placeholder="0"
					helpText={m.git_sync_max_total_size_per_sync_help()}
					bind:input={inputs.maxSyncTotalSizeMb}
				/>
				<FormInput
					label={m.git_sync_max_binary_size_label()}
					type="number"
					placeholder="0"
					helpText={m.git_sync_max_binary_size_per_sync_help()}
					bind:input={inputs.maxSyncBinarySizeMb}
				/>
			</div>
		</div>
	</Collapsible.Content>
</Collapsible.Root>

{#if lifecycleEnabled && canManageLifecycle && selectedTargetType === 'project'}
	<Collapsible.Root class="group/collapsible">
		<Collapsible.Trigger>
			{#snippet child({ props })}
				<button
					{...props}
					type="button"
					class="flex w-full items-center justify-between gap-3 py-1 text-left text-sm font-medium text-muted-foreground transition-colors hover:text-foreground"
				>
					{m.git_sync_pre_deploy_title()}
					<ArrowRightIcon class="size-4 shrink-0 transition-transform group-data-[state=open]/collapsible:rotate-90" />
				</button>
			{/snippet}
		</Collapsible.Trigger>
		<Collapsible.Content>
			<div class="space-y-3 pt-3">
				<Alert.Root variant="warning-subtle" size="sm" class="[&>svg]:top-1/2 [&>svg]:-translate-y-1/2">
					<InfoIcon class="size-4" />
					<Alert.Description>
						{m.git_sync_pre_deploy_acknowledgement()}
					</Alert.Description>
				</Alert.Root>

				<p class="text-xs text-muted-foreground">{m.git_sync_pre_deploy_description()}</p>

				<div class="space-y-2">
					<Label for="preDeployScriptPath">
						{m.git_sync_pre_deploy_script_path_label()}
					</Label>
					<div class="flex gap-2">
						<div class="flex-1">
							<Input
								id="preDeployScriptPath"
								type="text"
								placeholder={m.git_sync_pre_deploy_script_path_placeholder()}
								bind:value={inputs.preDeployScriptPath.value}
								aria-invalid={inputs.preDeployScriptPath.error ? 'true' : undefined}
							/>
						</div>
						{@render browseButton('preDeployScript')}
					</div>
					<p class="text-xs text-muted-foreground">{m.git_sync_pre_deploy_script_path_help()}</p>
					{#if inputs.preDeployScriptPath.error}
						<p class="text-xs font-medium text-destructive">{inputs.preDeployScriptPath.error}</p>
					{/if}
				</div>

				<FormInput
					label={m.git_sync_pre_deploy_runner_image_label()}
					type="text"
					placeholder={m.git_sync_pre_deploy_runner_image_placeholder()}
					helpText={m.git_sync_pre_deploy_runner_image_help()}
					bind:input={inputs.preDeployRunnerImage}
				/>

				<div class="grid gap-3 sm:grid-cols-2">
					<FormInput
						label={m.git_sync_pre_deploy_timeout_label()}
						type="number"
						placeholder="60"
						helpText={m.git_sync_pre_deploy_timeout_help()}
						bind:input={inputs.preDeployTimeoutSec}
					/>
					<FormInput
						label={m.resource_network_cap()}
						type="text"
						placeholder={m.git_sync_pre_deploy_network_mode_placeholder()}
						helpText={m.git_sync_pre_deploy_network_mode_help()}
						bind:input={inputs.preDeployNetworkMode}
					/>
				</div>

				<FormInput
					type="textarea"
					label={m.git_sync_pre_deploy_env_label()}
					placeholder={m.git_sync_pre_deploy_env_placeholder()}
					helpText={m.git_sync_pre_deploy_env_help()}
					bind:input={inputs.preDeployEnv}
					class="[&_textarea]:font-mono [&_textarea]:text-xs"
				/>

				<FormInput
					type="textarea"
					label={m.git_sync_pre_deploy_extra_mounts_label()}
					placeholder={m.git_sync_pre_deploy_extra_mounts_placeholder()}
					helpText={m.git_sync_pre_deploy_extra_mounts_help()}
					bind:input={inputs.preDeployExtraMounts}
					class="[&_textarea]:font-mono [&_textarea]:text-xs"
				/>
			</div>
		</Collapsible.Content>
	</Collapsible.Root>
{:else if lifecycleEnabled && canManageLifecycle}
	<p class="text-xs text-muted-foreground">
		<span class="font-medium">{m.git_sync_pre_deploy_title()}</span> · {m.git_sync_pre_deploy_swarm_unsupported()}
	</p>
{:else if lifecycleEnabled}
	<p class="flex items-center justify-between gap-3 text-xs text-muted-foreground">
		<span><span class="font-medium">{m.git_sync_pre_deploy_title()}</span> · {m.git_sync_pre_deploy_managed_hint()}</span>
		<span class="inline-flex shrink-0 items-center gap-1.5">
			{#if hookConfigured}
				<CodeIcon class="size-3.5" />
				{m.common_configured()}
			{:else}
				{m.common_not_configured()}
			{/if}
		</span>
	</p>
{/if}

<p class="text-xs text-muted-foreground">
	{m.webhook_hint_description()}
	<a href={resolve('/(app)/settings/webhooks')} class="text-foreground underline">{m.webhook_page_title()}</a>
	{m.git_sync_webhook_hint_suffix()}
</p>
