<script lang="ts">
	import { createQuery, keepPreviousData } from '@tanstack/svelte-query';
	import { untrack } from 'svelte';
	import { SvelteSet } from 'svelte/reactivity';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import SwitchWithLabel from '#lib/components/form/labeled-switch.svelte';
	import SheetFooterActions from '#lib/components/sheets/sheet-footer-actions.svelte';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import * as RadioGroup from '#lib/components/ui/radio-group/index.js';
	import * as ResponsiveDialog from '#lib/components/ui/responsive-dialog/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import {
		AlertIcon,
		AlertTriangleIcon,
		ClockIcon,
		CloseIcon,
		FilterIcon,
		SuccessIcon,
		VariableIcon,
		ZapIcon
	} from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { secretSourceService } from '#lib/services/secret-source-service.js';
	import type {
		SecretSource,
		SetupEnvFile,
		SetupMode,
		SetupResult,
		SetupStep,
		SetupTarget,
		SetupValues,
		SetupVariable
	} from '#lib/types/secret-source.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	let {
		open = $bindable(false),
		environmentId,
		projectId,
		sources,
		onDone,
		onAddToCompose
	}: {
		open: boolean;
		environmentId: string;
		projectId: string;
		// Sources that can write: Infisical, Bitwarden, and Vault/OpenBao.
		sources: SecretSource[];
		onDone: () => void | Promise<void>;
		// Opens the compose reference picker for keys the compose files do not use yet.
		onAddToCompose?: (keys: string[]) => void;
	} = $props();

	const defaultEnvironments = ['dev', 'staging', 'prod'];

	// ---- Where ----
	let sourceId = $state(
		untrack(
			() => (sources.find((source) => source.hasSetupCredential || source.provider === 'bitwarden') ?? sources[0])?.id ?? ''
		)
	);
	const provider = $derived(sources.find((source) => source.id === sourceId)?.provider ?? 'infisical');
	function defaultMode(sourceProvider: string | undefined): SetupMode {
		if (sourceProvider === 'bitwarden') return 'new-folder';
		if (sourceProvider === 'vault') return 'kv-path';
		return 'new-project';
	}
	let mode = $state<SetupMode>(untrack(() => defaultMode(sources.find((source) => source.id === sourceId)?.provider)));
	const createsContainer = $derived(mode === 'new-project' || mode === 'new-folder');
	let projectName = $state('');
	let folderName = $state('');
	let existingFolderId = $state('');
	let existingProjectId = $state('');
	let environment = $state('prod');
	let secretPath = $state('');
	let vaultMount = $state('secret');
	let vaultKv = $state(2);

	// Typing in the name or path waits a moment before asking Infisical again.
	let debouncedTarget = $state<SetupTarget | null>(null);
	const target = $derived.by<SetupTarget | null>(() => {
		if (mode === 'kv-path') {
			return { mode, mount: vaultMount.trim(), kvVersion: vaultKv, secretPath: secretPath.trim() };
		}
		if (mode === 'new-folder') {
			return { mode, folderName: folderName.trim() };
		}
		if (mode === 'existing-folder') {
			if (!existingFolderId) return null;
			const folder = remoteFolders.find((candidate) => candidate.id === existingFolderId);
			return { mode, folderId: existingFolderId, folderName: folder?.name ?? '' };
		}
		if (mode === 'new-project') {
			return { mode, projectName: projectName.trim(), environment };
		}
		if (!existingProjectId) return null;
		return { mode, projectId: existingProjectId, environment, secretPath: secretPath.trim() };
	});
	$effect(() => {
		const next = target;
		const timer = setTimeout(() => (debouncedTarget = next), 400);
		return () => clearTimeout(timer);
	});

	const planQuery = createQuery(() => ({
		queryKey: queryKeys.secretSources.setupPlan(environmentId, projectId, sourceId, JSON.stringify(debouncedTarget)),
		queryFn: () => secretSourceService.planSetup(environmentId, projectId, { sourceId, target: debouncedTarget ?? undefined }),
		enabled: open && !!sourceId,
		placeholderData: keepPreviousData,
		retry: false
	}));
	const plan = $derived(planQuery.data);

	const projectsQuery = createQuery(() => ({
		queryKey: queryKeys.secretSources.browse(sourceId, 'projects'),
		queryFn: () => secretSourceService.browse(sourceId, { kind: 'projects' }),
		enabled: open && !!sourceId && (mode === 'existing-project' || mode === 'shared-folder'),
		retry: false
	}));
	const foldersQuery = createQuery(() => ({
		queryKey: queryKeys.secretSources.browse(sourceId, 'folders'),
		queryFn: () => secretSourceService.browse(sourceId, { kind: 'folders' }),
		enabled: open && !!sourceId && mode === 'existing-folder',
		retry: false
	}));
	const remoteFolders = $derived(foldersQuery.data ?? []);
	const remoteProjects = $derived(projectsQuery.data ?? []);
	const selectedRemoteProject = $derived(remoteProjects.find((project) => project.id === existingProjectId));
	const environmentOptions = $derived(
		mode === 'new-project' || !selectedRemoteProject?.options?.length
			? defaultEnvironments.map((slug) => ({ id: slug, name: slug }))
			: selectedRemoteProject.options
	);

	// Prefill names from the first plan.
	let prefilled = false;
	$effect(() => {
		if (!plan || prefilled) return;
		prefilled = true;
		untrack(() => {
			projectName = plan.suggestedProjectName;
			folderName = plan.suggestedFolderName;
			if (plan.provider === 'vault' && !secretPath) secretPath = plan.suggestedSecretPath;
			applyDefaultSelection(plan.variables);
		});
	});

	function changeMode(next: SetupMode) {
		mode = next;
		if (next === 'shared-folder') {
			secretPath = plan?.suggestedSecretPath ?? '';
		} else if (next === 'existing-project') {
			secretPath = '/';
		}
	}

	// ---- What ----
	const selected = new SvelteSet<string>();
	const overwrite = new SvelteSet<string>();
	let onlyCompose = $state(false);
	let onlyEnv = $state(false);
	let onlySecretLike = $state(false);

	function applyDefaultSelection(variables: SetupVariable[]) {
		selected.clear();
		for (const variable of variables) {
			const needed = variable.inEnvFile || (variable.inCompose && !variable.composeDefault);
			if (variable.secretLike && needed) selected.add(variable.key);
		}
	}

	const variables = $derived(plan?.variables ?? []);
	const visibleVariables = $derived(
		variables.filter(
			(variable) =>
				(!onlyCompose || variable.inCompose) && (!onlyEnv || variable.inEnvFile) && (!onlySecretLike || variable.secretLike)
		)
	);
	const allVisibleSelected = $derived(visibleVariables.length > 0 && visibleVariables.every((v) => selected.has(v.key)));

	function toggleVisible(checked: boolean) {
		for (const variable of visibleVariables) {
			if (checked) selected.add(variable.key);
			else selected.delete(variable.key);
		}
	}

	// ---- How ----
	let values = $state<SetupValues>('import');
	let envFile = $state<SetupEnvFile>('remove');
	let keepBackup = $state(true);
	let grantDeployIdentity = $state(true);
	let required = $state(true);
	let autoRedeploy = $state(false);

	$effect(() => {
		if (values === 'placeholder') untrack(() => (envFile = 'keep'));
	});

	// ---- Apply ----
	let applying = $state(false);
	let applyError = $state<string | null>(null);
	let result = $state<SetupResult | null>(null);

	const selectedKeys = $derived(variables.filter((variable) => selected.has(variable.key)).map((variable) => variable.key));
	const canApply = $derived(
		!!plan?.canWrite &&
			!!target &&
			(provider === 'bitwarden' || provider === 'vault' || !!environment) &&
			selectedKeys.length > 0 &&
			!(createsContainer && plan.projectNameTaken) &&
			!planQuery.isFetching &&
			!applying
	);

	async function apply() {
		if (!target || !canApply) return;
		applying = true;
		applyError = null;
		const response = await tryCatch(
			secretSourceService.applySetup(environmentId, projectId, {
				sourceId,
				target,
				keys: selectedKeys,
				values,
				overwriteKeys: values === 'import' ? selectedKeys.filter((key) => overwrite.has(key)) : [],
				envFile,
				keepBackup,
				grantDeployIdentity: provider === 'infisical' && grantDeployIdentity,
				required,
				autoRedeploy
			})
		);
		applying = false;
		if (response.error) {
			applyError = extractApiErrorMessage(response.error);
			return;
		}
		result = response.data;
		await onDone();
	}

	// Moved keys that no compose file references yet: without a reference,
	// compose does not pass them to any container.
	const unreferencedKeys = $derived(
		variables.filter((variable) => selected.has(variable.key) && !variable.inCompose).map((variable) => variable.key)
	);

	function modeOptions(current: string) {
		if (current === 'vault') {
			return [{ value: 'kv-path', label: m.secret_setup_mode_kv_path(), description: m.secret_setup_mode_kv_path_description() }];
		}
		if (current === 'bitwarden') {
			return [
				{
					value: 'new-folder',
					label: m.secret_setup_mode_new_folder(),
					description: m.secret_setup_mode_new_folder_description()
				},
				{
					value: 'existing-folder',
					label: m.secret_setup_mode_existing_folder(),
					description: m.secret_setup_mode_existing_folder_description()
				}
			];
		}
		return [
			{ value: 'new-project', label: m.secret_setup_mode_new(), description: m.secret_setup_mode_new_description() },
			{
				value: 'existing-project',
				label: m.secret_setup_mode_existing(),
				description: m.secret_setup_mode_existing_description()
			},
			{ value: 'shared-folder', label: m.secret_setup_mode_shared(), description: m.secret_setup_mode_shared_description() }
		];
	}

	function stepLabel(step: SetupStep): string {
		switch (step.id) {
			case 'project':
				return m.secret_setup_step_project();
			case 'folder':
				return m.secret_setup_step_folder();
			case 'secrets':
				return m.secret_setup_step_secrets();
			case 'grant':
				return m.secret_setup_step_grant();
			case 'binding':
				return m.secret_setup_step_binding();
			case 'verify':
				return m.secret_setup_step_verify();
			default:
				return m.secret_setup_step_env_file();
		}
	}

	function remoteBadge(variable: SetupVariable): { variant: 'green' | 'gray' | 'amber' | 'blue'; text: string } | null {
		switch (variable.remote) {
			case 'missing':
				return { variant: 'blue', text: m.secret_setup_remote_missing() };
			case 'same':
				return { variant: 'green', text: m.secret_setup_remote_same() };
			case 'different':
				return { variant: 'amber', text: m.secret_setup_remote_different() };
			case 'exists':
				return { variant: 'gray', text: m.secret_setup_remote_exists() };
			default:
				return null;
		}
	}
</script>

<ResponsiveDialog.Root
	bind:open
	variant="sheet"
	title={m.secret_setup_title()}
	description={m.secret_setup_description()}
	contentClass="sm:max-w-2xl"
>
	{#snippet children()}
		<div class="grid gap-6 py-6">
			{#if result}
				{@render resultView(result)}
			{:else if sources.length === 0}
				<Alert.Root variant="info" icon={AlertIcon} description={m.secret_setup_no_sources()} />
				<ArcaneButton
					action="base"
					tone="outline-primary"
					href="/customize/secret-sources"
					customLabel={m.secret_sources_title()}
				/>
			{:else}
				{@render form()}
			{/if}
		</div>
	{/snippet}

	{#snippet footer()}
		{#if result}
			<ArcaneButton action="base" class="w-full" customLabel={m.common_close()} onclick={() => (open = false)} />
		{:else}
			<SheetFooterActions
				bind:open
				cancelDisabled={applying}
				submitAction="base"
				submitLabel={selectedKeys.length === 1
					? m.secret_setup_apply_one()
					: m.secret_setup_apply({ count: selectedKeys.length })}
				submitDisabled={!canApply}
				submitLoading={applying}
				onSubmit={() => void apply()}
			/>
		{/if}
	{/snippet}
</ResponsiveDialog.Root>

{#snippet form()}
	<section class="grid gap-4">
		<div class="space-y-2">
			<Label for="setup-source">{m.project_secrets_source()}</Label>
			<Select.Root
				type="single"
				value={sourceId}
				onValueChange={(value) => {
					sourceId = value;
					existingProjectId = '';
					existingFolderId = '';
					mode = defaultMode(sources.find((source) => source.id === value)?.provider);
					secretPath = '';
					prefilled = false;
				}}
			>
				<Select.Trigger id="setup-source" class="w-full">
					<span>{sources.find((source) => source.id === sourceId)?.name ?? m.project_secrets_select_source()}</span>
				</Select.Trigger>
				<Select.Content>
					{#each sources as source (source.id)}
						<Select.Item value={source.id}>{source.name}</Select.Item>
					{/each}
				</Select.Content>
			</Select.Root>
		</div>

		{#if planQuery.isPending}
			<div class="flex items-center gap-2 text-sm text-muted-foreground"><Spinner class="size-4" />{m.common_loading()}</div>
		{:else if planQuery.isError}
			<Alert.Root
				variant="destructive-subtle"
				icon={AlertIcon}
				heading={m.secret_setup_plan_failed()}
				description={extractApiErrorMessage(planQuery.error)}
			/>
		{:else if plan && !plan.canWrite}
			<Alert.Root
				variant="warning-subtle"
				icon={AlertTriangleIcon}
				heading={m.secret_setup_no_setup_identity()}
				description={provider === 'vault'
					? m.secret_setup_no_setup_token_description()
					: m.secret_setup_no_setup_identity_description()}
			/>
			<ArcaneButton
				action="base"
				tone="outline-primary"
				href="/customize/secret-sources"
				customLabel={m.secret_sources_title()}
			/>
		{/if}
	</section>

	{#if plan?.canWrite}
		{#if plan.alreadyBound}
			<Alert.Root variant="info" icon={AlertIcon} description={m.secret_setup_already_bound()} />
		{/if}

		<section class="grid gap-4">
			<h3 class="text-sm font-semibold">{m.secret_setup_where()}</h3>
			<RadioGroup.Root value={mode} onValueChange={(value) => changeMode(value as SetupMode)}>
				<div class="grid gap-2">
					{#each modeOptions(provider) as option (option.value)}
						<label class="flex cursor-pointer items-start gap-3 rounded-md border border-border/50 p-3 hover:bg-accent/40">
							<RadioGroup.Item value={option.value} class="mt-0.5" />
							<div class="grid gap-1 leading-none">
								<span class="text-sm font-medium">{option.label}</span>
								<span class="text-xs text-muted-foreground">{option.description}</span>
							</div>
						</label>
					{/each}
				</div>
			</RadioGroup.Root>

			{#if provider === 'bitwarden'}
				{@render bitwardenFields()}
			{:else if provider === 'vault'}
				<div class="flex flex-col gap-4 sm:flex-row sm:flex-wrap">
					<div class="flex-1 space-y-2">
						<Label for="setup-vault-mount">{m.vault_mount()}</Label>
						<Input id="setup-vault-mount" mono placeholder="secret" bind:value={vaultMount} />
					</div>
					<div class="space-y-2">
						<Label for="setup-vault-kv">{m.vault_kv_version()}</Label>
						<Select.Root type="single" value={String(vaultKv)} onValueChange={(value) => (vaultKv = Number(value))}>
							<Select.Trigger id="setup-vault-kv" class="w-28"><span>KV v{vaultKv}</span></Select.Trigger>
							<Select.Content>
								<Select.Item value="2">KV v2</Select.Item>
								<Select.Item value="1">KV v1</Select.Item>
							</Select.Content>
						</Select.Root>
					</div>
					<div class="w-full space-y-2">
						<Label for="setup-vault-path">{m.vault_secret_path()}</Label>
						<Input id="setup-vault-path" mono bind:value={secretPath} placeholder={plan.suggestedSecretPath} />
						<p class="text-xs text-muted-foreground">{m.secret_setup_kv_path_hint()}</p>
					</div>
				</div>
			{:else}
				<div class="grid gap-4 sm:grid-cols-2">
					{#if mode === 'new-project'}
						<div class="space-y-2">
							<Label for="setup-project-name">{m.secret_setup_project_name()}</Label>
							<Input id="setup-project-name" bind:value={projectName} maxlength={64} />
						</div>
					{:else}
						<div class="space-y-2">
							<Label for="setup-project">{m.project_secrets_infisical_project()}</Label>
							{#if projectsQuery.isError || (projectsQuery.isSuccess && remoteProjects.length === 0)}
								<Input
									id="setup-project"
									mono
									placeholder={m.project_secrets_infisical_project_id()}
									bind:value={existingProjectId}
								/>
							{:else}
								<Select.Root type="single" value={existingProjectId} onValueChange={(value) => (existingProjectId = value)}>
									<Select.Trigger id="setup-project" class="w-full">
										<span>{selectedRemoteProject?.name ?? m.project_secrets_select_project()}</span>
									</Select.Trigger>
									<Select.Content>
										{#each remoteProjects as project (project.id)}
											<Select.Item value={project.id}>{project.name}</Select.Item>
										{/each}
									</Select.Content>
								</Select.Root>
							{/if}
						</div>
					{/if}
					<div class="space-y-2">
						<Label for="setup-environment">{m.project_secrets_environment()}</Label>
						<Select.Root type="single" value={environment} onValueChange={(value) => (environment = value)}>
							<Select.Trigger id="setup-environment" class="w-full">
								<span>{environmentOptions.find((option) => option.id === environment)?.name ?? environment}</span>
							</Select.Trigger>
							<Select.Content>
								{#each environmentOptions as option (option.id)}
									<Select.Item value={option.id}>{option.name}</Select.Item>
								{/each}
							</Select.Content>
						</Select.Root>
					</div>
					{#if mode !== 'new-project'}
						<div class="space-y-2 sm:col-span-2">
							<Label for="setup-path">{m.project_secrets_path()}</Label>
							<Input
								id="setup-path"
								mono
								bind:value={secretPath}
								placeholder={mode === 'shared-folder' ? plan.suggestedSecretPath : '/'}
							/>
						</div>
					{/if}
				</div>
			{/if}

			{#if createsContainer && plan.projectNameTaken}
				<Alert.Root variant="warning-subtle" icon={AlertTriangleIcon} description={m.secret_setup_name_taken()} />
			{/if}
			{#if plan.remoteError}
				<Alert.Root
					variant="warning-subtle"
					icon={AlertTriangleIcon}
					heading={m.secret_setup_remote_error()}
					description={plan.remoteError}
				/>
			{/if}
		</section>

		<section class="grid gap-3">
			<div class="flex flex-wrap items-center gap-2">
				<h3 class="mr-auto text-sm font-semibold">
					{m.secret_setup_variables({ selected: selectedKeys.length, total: variables.length })}
				</h3>
				<FilterIcon class="size-4 text-muted-foreground" />
				{@render filterToggle(m.secret_setup_filter_compose(), onlyCompose, (value) => (onlyCompose = value))}
				{@render filterToggle(m.secret_setup_filter_env(), onlyEnv, (value) => (onlyEnv = value))}
				{@render filterToggle(m.secret_setup_filter_secret(), onlySecretLike, (value) => (onlySecretLike = value))}
			</div>

			{#if variables.length === 0}
				<p class="text-sm text-muted-foreground">{m.secret_setup_no_variables()}</p>
			{:else}
				<div class="divide-y divide-border/50 rounded-lg border border-border/50">
					<label class="flex items-center gap-3 px-3 py-2 text-xs text-muted-foreground">
						<Checkbox checked={allVisibleSelected} onCheckedChange={(checked) => toggleVisible(checked === true)} />
						{m.secret_setup_select_visible({ count: visibleVariables.length })}
					</label>
					{#each visibleVariables as variable (variable.key)}
						{@render variableRow(variable)}
					{/each}
				</div>
			{/if}
		</section>

		<section class="grid gap-4">
			<h3 class="text-sm font-semibold">{m.secret_setup_values()}</h3>
			<RadioGroup.Root value={values} onValueChange={(value) => (values = value as SetupValues)}>
				<div class="grid gap-2 sm:grid-cols-2">
					{@render choice('import', m.secret_setup_values_import(), m.secret_setup_values_import_description())}
					{@render choice('placeholder', m.secret_setup_values_placeholder(), m.secret_setup_values_placeholder_description())}
				</div>
			</RadioGroup.Root>

			<h3 class="text-sm font-semibold">{m.secret_setup_env_file()}</h3>
			<RadioGroup.Root
				value={envFile}
				onValueChange={(value) => (envFile = value as SetupEnvFile)}
				disabled={values !== 'import'}
			>
				<div class="grid gap-2 sm:grid-cols-2">
					{@render choice('remove', m.secret_setup_env_remove(), m.secret_setup_env_remove_description())}
					{@render choice('keep', m.secret_setup_env_keep(), m.secret_setup_env_keep_description())}
				</div>
			</RadioGroup.Root>
			{#if envFile === 'remove'}
				<SwitchWithLabel
					id="setup-keep-backup"
					label={m.secret_setup_keep_backup()}
					description={m.secret_setup_keep_backup_description()}
					bind:checked={keepBackup}
				/>
				{#if plan.hasGitSource}
					<Alert.Root variant="info" icon={AlertIcon} description={m.secret_setup_git_note()} />
				{/if}
			{/if}
		</section>

		<section class="grid gap-3 border-t border-border/50 pt-4">
			{#if provider === 'infisical'}
				<SwitchWithLabel
					id="setup-grant"
					label={m.secret_setup_grant()}
					description={plan.deployIdentity
						? m.secret_setup_grant_description({ name: plan.deployIdentity.name })
						: (plan.deployIdentityError ?? '')}
					bind:checked={grantDeployIdentity}
				/>
			{:else}
				<Alert.Root variant="info" icon={AlertIcon} description={m.secret_setup_bitwarden_note()} />
			{/if}
			<SwitchWithLabel
				id="setup-required"
				label={m.project_secrets_required()}
				description={m.project_secrets_required_description()}
				bind:checked={required}
			/>
			<SwitchWithLabel
				id="setup-auto-redeploy"
				label={m.project_secrets_auto_redeploy()}
				description={m.project_secrets_auto_redeploy_description()}
				bind:checked={autoRedeploy}
			/>
		</section>

		{#if applyError}
			<Alert.Root variant="destructive-subtle" icon={AlertIcon} heading={m.secret_setup_failed()} description={applyError} />
		{/if}
	{/if}
{/snippet}

{#snippet filterToggle(label: string, active: boolean, set: (value: boolean) => void)}
	<ArcaneButton
		action="base"
		size="sm"
		tone={active ? 'outline-primary' : 'ghost'}
		customLabel={label}
		aria-pressed={active}
		onclick={() => set(!active)}
	/>
{/snippet}

{#snippet choice(value: string, label: string, description: string)}
	<label class="flex cursor-pointer items-start gap-3 rounded-md border border-border/50 p-3 hover:bg-accent/40">
		<RadioGroup.Item {value} class="mt-0.5" />
		<div class="grid gap-1 leading-none">
			<span class="text-sm font-medium">{label}</span>
			<span class="text-xs text-muted-foreground">{description}</span>
		</div>
	</label>
{/snippet}

{#snippet variableRow(variable: SetupVariable)}
	{@const remote = remoteBadge(variable)}
	<div class="flex flex-wrap items-center gap-2 px-3 py-2">
		<Checkbox
			id={`setup-var-${variable.key}`}
			checked={selected.has(variable.key)}
			onCheckedChange={(checked) => (checked === true ? selected.add(variable.key) : selected.delete(variable.key))}
		/>
		<label for={`setup-var-${variable.key}`} class="mr-auto font-mono text-sm">{variable.key}</label>
		{#if variable.composeRequired}
			<Badge variant="red" size="sm">{m.secret_setup_badge_required()}</Badge>
		{:else if variable.inCompose && variable.composeDefault}
			<Badge variant="gray" size="sm">{m.secret_setup_badge_default()}</Badge>
		{:else if variable.inCompose}
			<Badge variant="blue" size="sm">{m.secret_setup_badge_compose()}</Badge>
		{/if}
		{#if variable.inEnvFile}
			<Badge variant={variable.envEmpty ? 'amber' : 'outline'} size="sm">
				{variable.envEmpty ? m.secret_setup_badge_env_empty() : m.secret_setup_badge_env()}
			</Badge>
		{/if}
		{#if variable.fromGit}
			<Badge variant="gray" size="sm">{m.secret_setup_badge_git()}</Badge>
		{/if}
		{#if variable.secretLike}
			<Badge variant="violet" size="sm">{m.secret_setup_badge_secret()}</Badge>
		{/if}
		{#if remote}
			<Badge variant={remote.variant} size="sm">{remote.text}</Badge>
		{/if}
		{#if variable.remote === 'different' && values === 'import' && selected.has(variable.key)}
			<label class="flex w-full items-center gap-2 pl-7 text-xs text-muted-foreground">
				<Checkbox
					checked={overwrite.has(variable.key)}
					onCheckedChange={(checked) => (checked === true ? overwrite.add(variable.key) : overwrite.delete(variable.key))}
				/>
				{m.secret_setup_overwrite()}
			</label>
		{/if}
	</div>
{/snippet}

{#snippet resultView(current: SetupResult)}
	<Alert.Root
		variant={current.ok ? 'primary-subtle' : 'warning-subtle'}
		icon={current.ok ? SuccessIcon : AlertTriangleIcon}
		heading={current.ok ? m.secret_setup_done() : m.secret_setup_partial()}
		description={current.ok ? m.secret_setup_done_description() : m.secret_setup_partial_description()}
	/>
	{#if current.binding && unreferencedKeys.length > 0 && onAddToCompose}
		<Alert.Root
			variant="warning-subtle"
			icon={VariableIcon}
			heading={m.secret_setup_unreferenced({ count: unreferencedKeys.length })}
			description={m.secret_setup_unreferenced_description({ keys: unreferencedKeys.join(', ') })}
		/>
		<ArcaneButton
			action="base"
			tone="outline-primary"
			icon={VariableIcon}
			customLabel={m.compose_refs_open()}
			onclick={() => onAddToCompose?.(unreferencedKeys)}
		/>
	{/if}
	<ol class="grid gap-2">
		{#each current.steps as step (step.id)}
			<li class="flex items-start gap-3 rounded-md border border-border/50 p-3">
				{#if step.status === 'done'}
					<SuccessIcon class="mt-0.5 size-4 shrink-0 text-success" />
				{:else if step.status === 'failed'}
					<CloseIcon class="mt-0.5 size-4 shrink-0 text-destructive" />
				{:else if step.status === 'pending'}
					<ClockIcon class="mt-0.5 size-4 shrink-0 text-warning" />
				{:else}
					<ZapIcon class="mt-0.5 size-4 shrink-0 text-muted-foreground" />
				{/if}
				<div class="grid gap-0.5">
					<span class="text-sm font-medium">{stepLabel(step)}</span>
					{#if step.detail}
						<span class="text-xs text-muted-foreground">{step.detail}</span>
					{/if}
				</div>
			</li>
		{/each}
	</ol>
{/snippet}

{#snippet bitwardenFields()}
	<div class="grid gap-4">
		{#if mode === 'new-folder'}
			<div class="space-y-2">
				<Label for="setup-folder-name">{m.secret_setup_folder_name()}</Label>
				<Input id="setup-folder-name" bind:value={folderName} />
			</div>
		{:else}
			<div class="space-y-2">
				<Label for="setup-folder">{m.bitwarden_select_folder()}</Label>
				{#if foldersQuery.isPending}
					<div class="flex items-center gap-2 text-sm text-muted-foreground"><Spinner class="size-4" />{m.common_loading()}</div>
				{:else}
					<Select.Root type="single" value={existingFolderId} onValueChange={(value) => (existingFolderId = value)}>
						<Select.Trigger id="setup-folder" class="w-full">
							<span>{remoteFolders.find((folder) => folder.id === existingFolderId)?.name ?? m.bitwarden_select_folder()}</span>
						</Select.Trigger>
						<Select.Content>
							{#each remoteFolders as folder (folder.id)}
								<Select.Item value={folder.id}>{folder.name}</Select.Item>
							{/each}
						</Select.Content>
					</Select.Root>
				{/if}
			</div>
		{/if}
	</div>
{/snippet}
