<script lang="ts">
	import { invalidateAll } from '$app/navigation';
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import SwitchWithLabel from '#lib/components/form/labeled-switch.svelte';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as Empty from '#lib/components/ui/empty/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import {
		AlertIcon,
		AlertTriangleIcon,
		ArrowDownIcon,
		ArrowUpIcon,
		CheckIcon,
		LockIcon,
		RefreshIcon,
		VariableIcon,
		ZapIcon
	} from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { projectService } from '#lib/services/project-service.js';
	import { secretSourceService } from '#lib/services/secret-source-service.js';
	import type {
		BindingTarget,
		BitwardenTarget,
		ComposeRefsResult,
		InfisicalTarget,
		ProjectSecretBinding,
		ProjectSecretCheckResult,
		ProjectSecretsCheck,
		SecretProvider,
		SecretSource
	} from '#lib/types/secret-source.js';
	import { extractApiErrorMessage, handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { confirmAndRun } from '#lib/utils/bulk-actions.js';
	import { formatRelativeTime } from '#lib/utils/formatting.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import BitwardenTargetFields from './bitwarden-target-fields.svelte';
	import ComposeRefsSheet from './compose-refs-sheet.svelte';
	import type { SecretRefsEditor } from './compose-refs.js';
	import InfisicalTargetFields from './infisical-target-fields.svelte';
	import ProviderIcon from './provider-icon.svelte';
	import ProviderTargetFields from './provider-target-fields.svelte';
	import {
		describeGenericTarget,
		isGenericProvider,
		isGenericTargetComplete,
		pickGenericTarget,
		providerCanSetUp,
		providerLabel
	} from './providers.js';
	import SecretSetupSheet from './secret-setup-sheet.svelte';

	let {
		environmentId,
		projectId,
		projectName,
		drafts = undefined
	}: {
		environmentId: string;
		projectId: string;
		projectName: string;
		// The project page's open compose and .env drafts. With them, references go
		// into the editor for review; without them, they are saved right away.
		drafts?: SecretRefsEditor;
	} = $props();

	const queryClient = useQueryClient();

	// Binding delivers whatever the source can read, so it also needs secret-sources:use.
	const canEdit = $derived(hasPermission('projects:update', environmentId) && hasPermission('secret-sources:use'));
	const canRemove = $derived(hasPermission('projects:update', environmentId));
	const canCheck = $derived(hasPermission('projects:deploy', environmentId));
	const canListSources = $derived(hasPermission('secret-sources:list') && hasPermission('secret-sources:read'));

	const bindingsQuery = createQuery(() => ({
		queryKey: queryKeys.secretSources.bindings(environmentId, projectId),
		queryFn: () => secretSourceService.listBindings(environmentId, projectId),
		enabled: !!environmentId && !!projectId
	}));
	const bindings = $derived<ProjectSecretBinding[]>(bindingsQuery.data ?? []);

	const sourcesQuery = createQuery(() => ({
		queryKey: queryKeys.secretSources.list(),
		queryFn: () => secretSourceService.list(),
		enabled: canListSources
	}));
	const sources = $derived(sourcesQuery.data ?? []);
	// Setup writes to Infisical and Vault/OpenBao (with a setup identity or token) and to Bitwarden through bw serve.
	const setupSources = $derived(sources.filter((source) => providerCanSetUp(source.provider)));
	const canSetup = $derived(canEdit && hasPermission('secret-sources:update'));

	// A binding can take more variables when its provider writes and its target
	// holds more than one item: a single Bitwarden item cannot.
	function canMoveMoreInto(binding: ProjectSecretBinding): boolean {
		if (!providerCanSetUp(binding.provider)) return false;
		if (binding.provider === 'bitwarden' && binding.target.bitwarden?.scope === 'item') return false;
		return setupSources.some((source) => source.id === binding.sourceId);
	}

	// ---- Guided setup ----
	let setupOpen = $state(false);
	let setupSession = $state(0);
	let setupChoices = $state<SecretSource[]>([]);

	// Without a binding, setup offers every writable source; from a binding, only its own.
	function openSetup(binding?: ProjectSecretBinding) {
		// Setup rewrites .env on the server, which would collide with unsaved edits.
		if (drafts?.dirty) {
			toast.warning(m.secret_setup_save_first());
			return;
		}
		setupChoices = binding ? setupSources.filter((source) => source.id === binding.sourceId) : setupSources;
		setupSession += 1;
		setupOpen = true;
	}

	// ---- Compose references ----
	let composeRefsOpen = $state(false);
	let composeRefsSession = $state(0);
	let composeRefsLoading = $state<string | null>(null);
	let composeRefsCompose = $state('');
	let composeRefsEnv = $state<string | null>(null);
	let composeRefsLocked = $state(false);
	let composeRefsKeys = $state<string[]>([]);
	// Opened from a binding with picked keys; they are cleared once added.
	let composeRefsBindingId = $state<string | null>(null);
	// With drafts, at least one of compose and .env must be editable.
	const composeRefsAllowed = $derived(!drafts || drafts.canEditCompose || drafts.canEditEnv);

	// Opens the picker for keys, or for every key of a binding. It works on the
	// editor's drafts when there are drafts, so unsaved edits are kept.
	async function openComposeRefs(keys?: string[], binding?: ProjectSecretBinding) {
		composeRefsLoading = binding?.id ?? 'setup';
		let compose = drafts?.compose ?? '';
		let env: string | null = drafts?.canEditEnv ? drafts.env : null;
		let loadError: unknown = null;
		if (!drafts) {
			const projectResult = await tryCatch(projectService.getProjectForEnvironment(environmentId, projectId));
			loadError = projectResult.error;
			compose = projectResult.data?.composeContent ?? '';
			env = projectResult.data?.envContent ?? '';
		}
		let keyList = keys;
		if (!loadError && !keyList && binding) {
			const keysResult = await tryCatch(secretSourceService.targetKeys(binding.sourceId, binding.target));
			keyList = keysResult.data?.keys;
			loadError = keysResult.error;
		}
		composeRefsLoading = null;
		if (loadError || !keyList) {
			toast.error(m.compose_refs_load_failed(), { description: extractApiErrorMessage(loadError) });
			return;
		}
		composeRefsCompose = compose;
		composeRefsEnv = env;
		composeRefsLocked = drafts ? !drafts.canEditCompose : false;
		composeRefsKeys = keyList;
		composeRefsBindingId = keys && binding ? binding.id : null;
		composeRefsSession += 1;
		composeRefsOpen = true;
	}

	async function saveComposeRefs(result: ComposeRefsResult) {
		const composeAdded = Object.values(result.added).reduce((total, keys) => total + keys.length, 0);
		const envAdded = result.envAdded.length;
		const added = composeAdded + envAdded;
		if (added === 0) return;
		const compose = composeAdded > 0 ? result.compose : undefined;
		const env = envAdded > 0 ? result.env : undefined;
		if (composeRefsBindingId) delete pickedKeys[composeRefsBindingId];
		checkResult = null;
		if (drafts) {
			drafts.apply({ compose, env });
			toast.success(m.compose_refs_in_editor({ count: added }));
			return;
		}
		const saved = await tryCatch(projectService.updateProject(projectId, undefined, compose, env));
		if (saved.error) {
			toast.error(m.compose_refs_failed(), { description: extractApiErrorMessage(saved.error) });
			return;
		}
		toast.success(m.compose_refs_saved({ count: added }));
		await invalidateAll();
	}

	// ---- Picked keys ----
	// Keys clicked in a binding's check results, by binding ID. Add to compose
	// offers only these when any are picked.
	let pickedKeys = $state<Record<string, string[]>>({});

	function togglePicked(bindingId: string, key: string) {
		const current = pickedKeys[bindingId] ?? [];
		const next = current.includes(key) ? current.filter((candidate) => candidate !== key) : [...current, key];
		if (next.length > 0) pickedKeys[bindingId] = next;
		else delete pickedKeys[bindingId];
	}

	// ---- Editor state ----
	// null: not editing; 'new': adding a binding; otherwise the ID being edited.
	let editingId = $state<string | null>(null);
	let saving = $state(false);
	let sourceId = $state('');
	let required = $state(true);
	let enabled = $state(true);
	let autoRedeploy = $state(false);
	let infisicalTarget = $state<InfisicalTarget>(emptyInfisicalTarget());
	let bitwardenTarget = $state<BitwardenTarget>(emptyBitwardenTarget());
	// Vault/OpenBao, Doppler, 1Password, Proton Pass, and HTTP targets.
	let genericTarget = $state<BindingTarget>({});

	function emptyInfisicalTarget(): InfisicalTarget {
		return { projectId: '', environment: '', secretPath: '/', includeImports: true, expandReferences: true };
	}

	function emptyBitwardenTarget(): BitwardenTarget {
		return { scope: 'folder', id: '', name: '' };
	}

	const draftProvider = $derived<SecretProvider | undefined>(sources.find((source) => source.id === sourceId)?.provider);

	function startEditing(binding?: ProjectSecretBinding) {
		sourceId = binding?.sourceId ?? (sources.length === 1 ? (sources[0]?.id ?? '') : '');
		required = binding?.required ?? true;
		enabled = binding?.enabled ?? true;
		autoRedeploy = binding?.autoRedeploy ?? false;
		infisicalTarget = binding?.target.infisical ? { ...binding.target.infisical } : emptyInfisicalTarget();
		bitwardenTarget = binding?.target.bitwarden ? { ...binding.target.bitwarden } : emptyBitwardenTarget();
		genericTarget = binding ? structuredClone(binding.target) : {};
		editingId = binding?.id ?? 'new';
	}

	function selectSource(value: string) {
		sourceId = value;
		infisicalTarget = emptyInfisicalTarget();
		bitwardenTarget = emptyBitwardenTarget();
		genericTarget = {};
	}

	const draftValid = $derived.by(() => {
		if (!sourceId) return false;
		if (draftProvider === 'infisical') return !!infisicalTarget.projectId.trim() && !!infisicalTarget.environment.trim();
		if (draftProvider === 'bitwarden') return !!bitwardenTarget.id;
		if (isGenericProvider(draftProvider)) return isGenericTargetComplete(draftProvider, genericTarget);
		return false;
	});

	// Shows Infisical project names instead of IDs in the summaries.
	const infisicalSourceIds = $derived([
		...new Set(bindings.filter((binding) => binding.provider === 'infisical').map((binding) => binding.sourceId))
	]);
	const infisicalProjectsQuery = createQuery(() => ({
		queryKey: ['secret-sources', 'infisical-project-names', ...infisicalSourceIds] as const,
		queryFn: async () => {
			const names = new Map<string, string>();
			for (const id of infisicalSourceIds) {
				const result = await tryCatch(secretSourceService.browse(id, { kind: 'projects' }));
				for (const project of result.data ?? []) names.set(`${id}/${project.id}`, project.name);
			}
			return names;
		},
		enabled: editingId === null && infisicalSourceIds.length > 0 && canListSources,
		retry: false
	}));

	function describeTarget(current: ProjectSecretBinding): string {
		const infisical = current.target.infisical;
		if (infisical) {
			const name = infisicalProjectsQuery.data?.get(`${current.sourceId}/${infisical.projectId}`);
			return `${name ?? infisical.projectId} · ${infisical.environment} · ${infisical.secretPath}`;
		}
		const bitwarden = current.target.bitwarden;
		if (bitwarden) {
			return `${scopeLabel(bitwarden.scope)} · ${bitwarden.name || bitwarden.id}`;
		}
		return describeGenericTarget(current.target);
	}

	function scopeLabel(scope: BitwardenTarget['scope']): string {
		switch (scope) {
			case 'collection':
				return m.bitwarden_scope_collection();
			case 'item':
				return m.bitwarden_scope_item();
			default:
				return m.bitwarden_scope_folder();
		}
	}

	async function invalidateBindings() {
		await queryClient.invalidateQueries({ queryKey: queryKeys.secretSources.bindings(environmentId, projectId) });
		await queryClient.invalidateQueries({ queryKey: queryKeys.secretSources.list() });
	}

	function targetForSave(): BindingTarget {
		if (draftProvider === 'bitwarden') return { bitwarden: bitwardenTarget };
		if (isGenericProvider(draftProvider)) return pickGenericTarget(draftProvider, genericTarget);
		return { infisical: infisicalTarget };
	}

	async function save() {
		if (!draftValid || editingId === null) return;
		const dto = { sourceId, target: targetForSave(), required, enabled, autoRedeploy };
		const request =
			editingId === 'new'
				? secretSourceService.createBinding(environmentId, projectId, dto)
				: secretSourceService.updateBinding(environmentId, projectId, editingId, dto);
		await handleApiResultWithCallbacks({
			result: await tryCatch(request),
			message: m.project_secrets_save_failed(),
			setLoadingState: (value) => (saving = value),
			onSuccess: async () => {
				toast.success(m.project_secrets_saved());
				editingId = null;
				checkResult = null;
				await invalidateBindings();
			}
		});
	}

	let reordering = $state(false);

	async function move(binding: ProjectSecretBinding, offset: number) {
		const index = bindings.findIndex((candidate) => candidate.id === binding.id);
		const position = index + offset;
		if (index < 0 || position < 0 || position >= bindings.length) return;
		await handleApiResultWithCallbacks({
			result: await tryCatch(
				secretSourceService.updateBinding(environmentId, projectId, binding.id, {
					sourceId: binding.sourceId,
					target: binding.target,
					required: binding.required,
					enabled: binding.enabled,
					autoRedeploy: binding.autoRedeploy,
					position
				})
			),
			message: m.project_secrets_reorder_failed(),
			setLoadingState: (value) => (reordering = value),
			onSuccess: async () => {
				toast.success(m.project_secrets_reordered());
				checkResult = null;
				await invalidateBindings();
			}
		});
	}

	function detach(binding: ProjectSecretBinding) {
		confirmAndRun({
			title: m.project_secrets_detach_one_title({ source: binding.sourceName, name: projectName }),
			message: m.project_secrets_detach_message(),
			confirmLabel: m.project_secrets_detach(),
			destructive: true,
			run: () => secretSourceService.deleteBinding(environmentId, projectId, binding.id),
			failureMessage: m.common_action_failed(),
			onSuccess: async () => {
				toast.success(m.project_secrets_detached());
				checkResult = null;
				await invalidateBindings();
			}
		});
	}

	// ---- Check ----
	let checking = $state(false);
	let checkResult = $state<ProjectSecretsCheck | null>(null);
	const checksById = $derived(new Map((checkResult?.bindings ?? []).map((check) => [check.bindingId, check])));

	async function checkNow() {
		await handleApiResultWithCallbacks({
			result: await tryCatch(secretSourceService.checkBindings(environmentId, projectId)),
			message: m.project_secrets_check_failed(),
			setLoadingState: (value) => (checking = value),
			onSuccess: (result) => {
				checkResult = result;
			},
			onError: () => {
				checkResult = null;
			}
		});
		await queryClient.invalidateQueries({ queryKey: queryKeys.secretSources.bindings(environmentId, projectId) });
	}
</script>

{#if environmentId !== '0'}
	<Alert.Root variant="info" icon={AlertIcon} heading={m.project_secrets_local_only()} />
{:else if bindingsQuery.isPending}
	<div class="flex justify-center py-12"><Spinner /></div>
{:else if editingId !== null}
	{@render editor()}
{:else if bindings.length === 0}
	<div class="rounded-xl border border-dashed border-border/70">
		<Empty.Root>
			<Empty.Header>
				<Empty.Media variant="icon">
					<LockIcon />
				</Empty.Media>
				<Empty.Title>{m.project_secrets_not_bound_title()}</Empty.Title>
				<Empty.Description>{m.project_secrets_not_bound_description()}</Empty.Description>
			</Empty.Header>
			{#if canEdit}
				<Empty.Content>
					{#if canListSources && sourcesQuery.isSuccess && sources.length === 0}
						<p class="text-sm text-muted-foreground">{m.project_secrets_no_sources()}</p>
						<ArcaneButton
							action="base"
							tone="outline-primary"
							href="/customize/secret-sources"
							icon={LockIcon}
							customLabel={m.secret_sources_title()}
						/>
					{:else}
						<div class="flex flex-wrap justify-center gap-2">
							{#if canSetup && setupSources.length > 0}
								<ArcaneButton
									action="base"
									icon={ZapIcon}
									customLabel={m.secret_setup_open()}
									disabled={!canListSources}
									onclick={() => openSetup()}
								/>
							{/if}
							<ArcaneButton
								action="base"
								tone="outline-primary"
								icon={LockIcon}
								customLabel={m.project_secrets_bind()}
								disabled={!canListSources}
								onclick={() => startEditing()}
							/>
						</div>
					{/if}
				</Empty.Content>
			{/if}
		</Empty.Root>
	</div>
{:else}
	{@render overview()}
{/if}

{#if setupSession > 0}
	{#key setupSession}
		<SecretSetupSheet
			bind:open={setupOpen}
			{environmentId}
			{projectId}
			sources={setupChoices}
			onDone={async () => {
				checkResult = null;
				await invalidateBindings();
				// Setup edits .env on the server; show the new files in the editor.
				await drafts?.reload();
			}}
			onAddToCompose={(keys) => {
				setupOpen = false;
				void openComposeRefs(keys);
			}}
		/>
	{/key}
{/if}

{#if composeRefsSession > 0}
	{#key composeRefsSession}
		<ComposeRefsSheet
			bind:open={composeRefsOpen}
			compose={composeRefsCompose}
			env={composeRefsEnv}
			composeLocked={composeRefsLocked}
			keys={composeRefsKeys}
			onApply={saveComposeRefs}
		/>
	{/key}
{/if}

{#snippet overview()}
	<div class="space-y-4">
		<div class="flex flex-wrap items-center gap-3">
			<h3 class="text-base font-semibold">{m.project_secrets_sources_heading()}</h3>
			<div class="flex flex-wrap items-center gap-1.5">
				{#each bindings as binding (binding.id)}
					<Badge variant={binding.enabled ? 'outline' : 'gray'} size="sm">
						<ProviderIcon provider={binding.provider} class="size-4 rounded-sm p-0 ring-0" />
						{binding.sourceName}
					</Badge>
				{/each}
			</div>
			{#if checkResult?.redeployNeeded}
				<Badge variant="amber" size="sm"><AlertTriangleIcon class="size-3" />{m.project_secrets_redeploy_needed()}</Badge>
			{/if}
			<div class="ml-auto flex flex-wrap items-center gap-2">
				{#if canCheck}
					<ArcaneButton
						action="refresh"
						tone="outline-primary"
						size="sm"
						icon={RefreshIcon}
						customLabel={m.project_secrets_check_now()}
						loading={checking}
						disabled={checking}
						onclick={checkNow}
					/>
				{/if}
				{#if canEdit}
					<ArcaneButton
						action="base"
						tone="outline-primary"
						size="sm"
						icon={LockIcon}
						customLabel={m.project_secrets_add_source()}
						disabled={!canListSources}
						onclick={() => startEditing()}
					/>
				{/if}
			</div>
		</div>

		{#if bindings.length > 1}
			<p class="text-xs text-muted-foreground">{m.project_secrets_order_hint()}</p>
		{/if}

		{#each bindings as binding, index (binding.id)}
			{@render bindingCard(binding, index)}
		{/each}

		{#if !checkResult}
			<p class="text-sm text-muted-foreground">{m.project_secrets_check_hint()}</p>
		{/if}

		<Alert.Root
			variant="primary-subtle"
			icon={LockIcon}
			description={m.project_secrets_usage_hint({
				example: 'environment: [DB_PASSWORD]',
				envExample: 'DB_PASSWORD=${DB_PASSWORD}'
			})}
		/>
	</div>
{/snippet}

{#snippet bindingCard(current: ProjectSecretBinding, index: number)}
	{@const check = checksById.get(current.id)}
	<div class="space-y-3 rounded-xl border border-border/70 bg-card/60 p-4 backdrop-blur-md">
		<div class="flex flex-wrap items-center gap-4">
			<ProviderIcon provider={current.provider} class="size-12 rounded-xl p-2.5" />
			<div class="min-w-0 flex-1 space-y-1">
				<div class="flex flex-wrap items-center gap-2">
					<h4 class="text-base font-semibold">{m.project_secrets_title({ provider: providerLabel(current.provider) })}</h4>
					{#if !current.enabled}
						<Badge variant="gray" size="sm">{m.project_secrets_disabled()}</Badge>
					{:else if current.required}
						<Badge variant="blue" size="sm">{m.project_secrets_required()}</Badge>
					{:else}
						<Badge variant="amber" size="sm">{m.project_secrets_optional()}</Badge>
					{/if}
					{#if current.autoRedeploy}
						<Badge variant="violet" size="sm">{m.project_secrets_auto_redeploy()}</Badge>
					{/if}
					{#if current.redeployNeeded}
						<Badge variant="amber" size="sm"><AlertTriangleIcon class="size-3" />{m.project_secrets_redeploy_needed()}</Badge>
					{/if}
				</div>
				<p class="truncate font-mono text-xs text-muted-foreground">
					{current.sourceName} · {describeTarget(current)}
				</p>
				<p class="text-xs text-muted-foreground">
					{#if current.lastFetchedAt}
						{m.project_secrets_last_fetched({ time: formatRelativeTime(current.lastFetchedAt) })}
					{:else}
						{m.project_secrets_never_fetched()}
					{/if}
					{#if current.lastCheckedAt}
						· {m.project_secrets_last_checked({ time: formatRelativeTime(current.lastCheckedAt) })}
					{/if}
				</p>
			</div>
			<div class="flex flex-wrap items-center gap-2">
				{#if canEdit && bindings.length > 1}
					<ArcaneButton
						action="base"
						tone="ghost"
						size="sm"
						icon={ArrowUpIcon}
						customLabel={m.project_secrets_move_up()}
						showLabel={false}
						disabled={index === 0 || reordering}
						onclick={() => void move(current, -1)}
					/>
					<ArcaneButton
						action="base"
						tone="ghost"
						size="sm"
						icon={ArrowDownIcon}
						customLabel={m.project_secrets_move_down()}
						showLabel={false}
						disabled={index === bindings.length - 1 || reordering}
						onclick={() => void move(current, 1)}
					/>
				{/if}
				{#if canSetup && canMoveMoreInto(current)}
					<ArcaneButton
						action="base"
						tone="outline"
						size="sm"
						icon={ZapIcon}
						customLabel={m.secret_setup_open_again()}
						disabled={!canListSources}
						onclick={() => openSetup(current)}
					/>
				{/if}
				{#if canEdit}
					{@const picked = pickedKeys[current.id] ?? []}
					<ArcaneButton
						action="base"
						tone={picked.length > 0 ? 'outline-primary' : 'outline'}
						size="sm"
						icon={VariableIcon}
						customLabel={picked.length > 0 ? m.compose_refs_open_picked({ count: picked.length }) : m.compose_refs_open()}
						loading={composeRefsLoading === current.id}
						disabled={composeRefsLoading !== null || !composeRefsAllowed}
						onclick={() => void openComposeRefs(picked.length > 0 ? [...picked].sort() : undefined, current)}
					/>
					<ArcaneButton action="edit" tone="outline" size="sm" onclick={() => startEditing(current)} disabled={!canListSources} />
				{/if}
				{#if canRemove}
					<ArcaneButton
						action="remove"
						tone="outline-destructive"
						size="sm"
						customLabel={m.project_secrets_detach()}
						onclick={() => detach(current)}
					/>
				{/if}
			</div>
		</div>

		{#if check?.error}
			<Alert.Root
				variant="destructive-subtle"
				icon={AlertIcon}
				heading={m.project_secrets_check_error()}
				description={check.error}
			/>
		{:else if current.lastFetchError && !check}
			<Alert.Root
				variant="destructive-subtle"
				icon={AlertIcon}
				heading={m.project_secrets_last_fetch_error()}
				description={current.lastFetchError}
			/>
		{/if}

		{#if check && !check.error}
			{@render checkDetails(check, current)}
		{/if}
	</div>
{/snippet}

{#snippet checkDetails(result: ProjectSecretCheckResult, current: ProjectSecretBinding)}
	{@const overridden = new Set(result.overriddenKeys)}
	{@const shadowed = new Set(result.shadowedKeys)}
	{@const unused = new Set(result.unusedKeys)}
	{#snippet keyBadge(key: string, isPicked: boolean)}
		<Badge
			variant={isPicked
				? 'blue'
				: shadowed.has(key)
					? 'gray'
					: unused.has(key)
						? 'secondary'
						: overridden.has(key)
							? 'amber'
							: 'outline'}
			size="sm"
			mono
		>
			{#if isPicked}<CheckIcon class="size-3" />{/if}{key}
		</Badge>
	{/snippet}
	<div class="space-y-3 border-t border-border/50 pt-3">
		<div class="flex flex-wrap items-center gap-2">
			{#if result.neverDeployed}
				<Badge variant="gray" size="sm">{m.project_secrets_never_deployed()}</Badge>
			{:else if result.redeployNeeded}
				<Badge variant="amber" size="sm"><AlertTriangleIcon class="size-3" />{m.project_secrets_redeploy_needed()}</Badge>
				<span class="text-xs text-muted-foreground">{m.project_secrets_redeploy_needed_description()}</span>
			{:else}
				<Badge variant="green" size="sm"><CheckIcon class="size-3" />{m.project_secrets_in_sync()}</Badge>
			{/if}
			<span class="ml-auto text-xs text-muted-foreground"
				>{result.keys.length === 1
					? m.project_secrets_keys_count_one()
					: m.project_secrets_keys_count({ count: result.keys.length })}</span
			>
		</div>

		{#if result.keys.length > 0}
			{@const picked = pickedKeys[current.id] ?? []}
			{#if canEdit}
				<p class="text-xs text-muted-foreground">{m.project_secrets_pick_hint()}</p>
			{/if}
			<div class="flex flex-wrap gap-1.5">
				{#each result.keys as key (key)}
					{@const status = shadowed.has(key)
						? m.project_secrets_key_shadowed()
						: unused.has(key)
							? m.project_secrets_key_unused()
							: overridden.has(key)
								? m.project_secrets_overrides()
								: undefined}
					{#if canEdit}
						<button
							type="button"
							class="rounded-md focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
							aria-pressed={picked.includes(key)}
							title={status}
							onclick={() => togglePicked(current.id, key)}
						>
							{@render keyBadge(key, picked.includes(key))}
						</button>
					{:else}
						<span title={status}>{@render keyBadge(key, false)}</span>
					{/if}
				{/each}
			</div>
		{:else}
			<Alert.Root
				variant="destructive-subtle"
				icon={AlertIcon}
				heading={m.project_secrets_empty_title()}
				description={current.required ? m.project_secrets_empty_required() : m.project_secrets_empty_optional()}
			/>
		{/if}

		{#if result.shadowedKeys.length > 0}
			<Alert.Root
				variant="info"
				icon={AlertIcon}
				heading={m.project_secrets_shadowed()}
				description={`${m.project_secrets_shadowed_description()} ${result.shadowedKeys.join(', ')}`}
			/>
		{/if}

		{#if result.unusedKeys.length > 0}
			<Alert.Root
				variant="info"
				icon={VariableIcon}
				heading={m.project_secrets_unused()}
				description={`${m.project_secrets_unused_description()} ${result.unusedKeys.join(', ')}`}
			/>
		{/if}

		{#if result.overriddenKeys.length > 0}
			<Alert.Root
				variant="warning-subtle"
				icon={AlertTriangleIcon}
				heading={m.project_secrets_overrides()}
				description={m.project_secrets_overrides_description()}
			/>
		{/if}

		{#if result.invalidKeys.length > 0}
			<Alert.Root
				variant="warning-subtle"
				icon={AlertTriangleIcon}
				heading={m.project_secrets_invalid_keys()}
				description={`${m.project_secrets_invalid_keys_description()} ${result.invalidKeys.join(', ')}`}
			/>
		{/if}
	</div>
{/snippet}

{#snippet editor()}
	<form
		class="grid max-w-2xl gap-4 rounded-xl border border-border/70 bg-card/60 p-4"
		onsubmit={(event) => {
			event.preventDefault();
			void save();
		}}
	>
		<h3 class="text-base font-semibold">
			{draftProvider ? m.project_secrets_title({ provider: providerLabel(draftProvider) }) : m.project_secrets_bind()}
		</h3>

		<div class="space-y-2">
			<Label for="secret-source">{m.project_secrets_source()}</Label>
			<Select.Root type="single" value={sourceId} onValueChange={selectSource}>
				<Select.Trigger id="secret-source" class="w-full">
					<span>{sources.find((source) => source.id === sourceId)?.name ?? m.project_secrets_select_source()}</span>
				</Select.Trigger>
				<Select.Content>
					{#each sources as source (source.id)}
						<Select.Item value={source.id}>
							<ProviderIcon provider={source.provider} class="size-6 rounded-md p-1" />
							<div class="flex flex-col">
								<span>{source.name}</span>
								<span class="text-xs text-muted-foreground">{providerLabel(source.provider)}</span>
							</div>
						</Select.Item>
					{/each}
				</Select.Content>
			</Select.Root>
		</div>

		{#if sourceId && draftProvider === 'infisical'}
			{#key sourceId}
				<InfisicalTargetFields {sourceId} bind:target={infisicalTarget} />
			{/key}
		{:else if sourceId && draftProvider === 'bitwarden'}
			{#key sourceId}
				<BitwardenTargetFields {sourceId} bind:target={bitwardenTarget} />
			{/key}
		{:else if sourceId && isGenericProvider(draftProvider)}
			{#key sourceId}
				<ProviderTargetFields {sourceId} provider={draftProvider} bind:target={genericTarget} />
			{/key}
		{/if}

		<div class="grid gap-3 border-t border-border/50 pt-4">
			<SwitchWithLabel
				id="secret-required"
				label={m.project_secrets_required()}
				description={m.project_secrets_required_description()}
				bind:checked={required}
			/>
			<SwitchWithLabel
				id="secret-enabled"
				label={m.common_enabled()}
				description={m.project_secrets_enabled_description()}
				bind:checked={enabled}
			/>
			<SwitchWithLabel
				id="secret-auto-redeploy"
				label={m.project_secrets_auto_redeploy()}
				description={m.project_secrets_auto_redeploy_description()}
				bind:checked={autoRedeploy}
			/>
		</div>

		<div class="flex gap-2">
			<ArcaneButton action="cancel" tone="outline" type="button" onclick={() => (editingId = null)} disabled={saving} />
			<ArcaneButton action="save" type="submit" loading={saving} disabled={saving || !draftValid} />
		</div>
	</form>
{/snippet}
