<script lang="ts">
	import { refreshAll } from '$app/navigation';
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';

	import { ActionButtonGroup, type ActionButton } from '#lib/components/action-button-group/index.js';
	import AgentCommandBlock from '#lib/components/agent-command-block.svelte';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import ImagePatchSettings from '#lib/components/settings/image-patch-settings.svelte';
	import LifecycleSecuritySettings from '#lib/components/settings/lifecycle-security-settings.svelte';
	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import SettingsSection from '#lib/components/settings/settings-section.svelte';
	import TrivySecuritySettings from '#lib/components/settings/trivy-security-settings.svelte';
	import { EmptyState } from '#lib/components/states/index.js';
	import { TabBar, type TabItem } from '#lib/components/tab-bar/index.js';
	import * as AlertDialog from '#lib/components/ui/alert-dialog/index.js';
	import { CopyButton } from '#lib/components/ui/copy-button/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import { featureDefinitions } from '#lib/config/features.js';
	import { useEasyJoinCandidates } from '#lib/hooks/use-easy-join-candidates.svelte.js';
	import { useUrlTab } from '#lib/hooks/use-url-tab.svelte.js';
	import {
		AlertIcon,
		DockerBrandIcon,
		SecurityIcon,
		JobsIcon,
		ResetIcon,
		ConnectionIcon,
		VolumesIcon,
		ScanIcon,
		ShieldCheckIcon,
		CodeIcon,
		SettingsIcon,
		ClockIcon,
		ActivityIcon
	} from '#lib/icons/index.js';
	import TabbedPageLayout from '#lib/layouts/tabbed-page-layout.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { environmentManagementService } from '#lib/services/env-mgmt-service.js';
	import { parseSettingValue, settingsService } from '#lib/services/settings-service.js';
	import settingsStore from '#lib/stores/config-store.svelte.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import { featureStore } from '#lib/stores/features.store.svelte.js';
	import type { DeploymentSnippets, Environment } from '#lib/types/environment.js';
	import type { FeatureID } from '#lib/types/features.js';
	import type { SettingKey, Settings } from '#lib/types/settings.js';
	import { cn } from '#lib/utils.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { isEnvironmentOnline, resolveEnvironmentStatus } from '#lib/utils/docker.js';
	import { changedFields } from '#lib/utils/payload.js';
	import { createSettingsForm } from '#lib/utils/settings-form.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import EditableName from '../../projects/components/editable-name.svelte';
	import EasyJoinDialog from '../../swarm/cluster/components/easy-join-dialog.svelte';
	import ActivityTab from './components/activity-tab.svelte';
	import BuildsTab from './components/builds-tab.svelte';
	import ConnectionAgentTab from './components/connection-agent-tab.svelte';
	import DockerTab from './components/docker-tab.svelte';
	import {
		environmentFormSchema,
		environmentUpdateSchema,
		requireTrivyServerUrl,
		type EnvironmentFormValues
	} from './components/environment-form-schema';
	import EnvironmentStatusSummary from './components/environment-status-summary.svelte';
	import JobsTab from './components/jobs-tab.svelte';
	import StorageTab from './components/storage-tab.svelte';
	import TimeoutsTab from './components/timeouts-tab.svelte';

	let { data } = $props();
	let { settings, versionInformation } = $derived(data);
	const swarmActive = $derived(data.swarmActive === true);
	let lastEnvironment: Environment | undefined;
	let loadedEnvironment = $derived((lastEnvironment = data.environment ?? lastEnvironment));
	let environment = $derived(loadedEnvironment);
	// Agent reachability only reaches the browser through the live environment stream; the certificate is detail-only.
	const liveEnvironment = $derived(
		environment.id === '0' ? undefined : environmentStore.available.find((env) => env.id === environment.id)
	);
	let runtimeEnvironment: Environment = $derived(
		liveEnvironment ? { ...liveEnvironment, agentMTLSCertificate: environment.agentMTLSCertificate } : environment
	);
	const queryClient = useQueryClient();

	let isRefreshing = $state(false);
	let isTestingConnection = $state(false);
	let isSyncing = $state(false);
	let isRegeneratingKey = $state(false);
	let showRegenerateDialog = $state(false);
	let regeneratedApiKey = $state<string | null>(null);
	let regeneratedSnippets = $state<DeploymentSnippets | null>(null);
	let easyJoinDialogOpen = $state(false);
	let easyJoinSession = $state(0);
	let nameInputRef = $state<HTMLInputElement | null>(null);
	const easyJoinCandidates = useEasyJoinCandidates();

	let currentStatus = $derived(resolveEnvironmentStatus(runtimeEnvironment));
	let isCurrentlyOnline = $derived(isEnvironmentOnline(runtimeEnvironment));

	const versionQuery = createQuery(() => {
		const environmentId = environment.id;
		return {
			queryKey: queryKeys.system.versionInfo(environmentId),
			queryFn: () => environmentManagementService.getVersion(environmentId),
			enabled: environmentId !== '0' && isCurrentlyOnline,
			retry: false
		};
	});
	const remoteVersion = $derived(versionQuery.data ?? null);
	const isLoadingVersion = $derived(versionQuery.isFetching);

	// Settings tabs stay listed while offline so the selected tab survives a temporary outage.
	let showSettingsTabs = $derived(runtimeEnvironment.enabled && hasPermission('settings:read', environment.id));
	let settingsAvailable = $derived(isCurrentlyOnline && settings !== null);
	let showJobsTab = $derived(showSettingsTabs && hasPermission('jobs:manage', environment.id));
	let hasMTLSAssets = $derived(Boolean(runtimeEnvironment.agentMTLSCertificate));
	let canPairEnvironments = $derived(hasPermission('environments:pair', environment.id));
	let showMTLSDownloads = $derived(
		canPairEnvironments && runtimeEnvironment.id !== '0' && (runtimeEnvironment.agentSecurityMode === 'mtls' || hasMTLSAssets)
	);
	let canEasyJoin = $derived(
		runtimeEnvironment.id !== '0' &&
			runtimeEnvironment.enabled &&
			isCurrentlyOnline &&
			easyJoinCandidates.isCandidate(runtimeEnvironment.id)
	);
	let headerActions = $derived.by((): ActionButton[] => {
		const actions: ActionButton[] = [];
		if (settingsForm.hasChanges) {
			actions.push({
				id: 'reset',
				action: 'base',
				placement: 'primary',
				label: m.common_reset(),
				onclick: resetForm,
				disabled: settingsForm.isLoading,
				icon: ResetIcon
			});
		}
		actions.push({
			id: 'save',
			action: 'save',
			placement: 'primary',
			label: m.common_save(),
			loadingLabel: m.common_saving(),
			onclick: onSubmit,
			disabled: !settingsForm.hasChanges || settingsForm.isLoading,
			loading: settingsForm.isLoading
		});
		actions.push({
			id: 'test',
			action: 'test',
			label: m.test_connection(),
			onclick: testConnection,
			disabled: isTestingConnection,
			loading: isTestingConnection
		});
		actions.push({
			id: 'refresh',
			action: 'refresh',
			placement: 'secondary',
			iconOnly: true,
			label: m.common_refresh(),
			onclick: refreshEnvironment,
			disabled: isRefreshing,
			loading: isRefreshing
		});
		if (environment.id !== '0') {
			actions.push({
				id: 'sync',
				action: 'sync',
				label: m.resource_sync_cap(),
				onclick: syncEnvironment,
				disabled: isSyncing,
				loading: isSyncing
			});
		}
		if (canEasyJoin) {
			actions.push({
				id: 'easy-join',
				action: 'create',
				label: m.swarm_easy_join_action(),
				onclick: () => {
					easyJoinSession += 1;
					easyJoinDialogOpen = true;
				},
				icon: ConnectionIcon
			});
		}

		return actions;
	});

	const tabItems = $derived.by((): TabItem[] => {
		const items: TabItem[] = [];

		// The local environment is the manager itself, so it has no connection to show.
		if (runtimeEnvironment.id !== '0') {
			items.push({
				value: 'connection',
				label: m.connection_agent(),
				icon: ConnectionIcon
			});
		}
		items.push({ value: 'features', label: m.features_title(), icon: SettingsIcon });

		if (showSettingsTabs) {
			items.push(
				{
					value: 'storage',
					label: m.storage_limits(),
					icon: VolumesIcon
				},
				{
					value: 'docker',
					label: m.environments_docker_settings_title(),
					icon: DockerBrandIcon
				},
				{
					value: 'security',
					label: m.security(),
					icon: SecurityIcon
				},
				{
					value: 'timeouts',
					label: m.timeouts_settings(),
					icon: ClockIcon
				},
				{
					value: 'activity',
					label: m.activity(),
					icon: ActivityIcon
				},
				{
					value: 'builds',
					label: m.builds(),
					icon: CodeIcon
				}
			);
		}

		if (showJobsTab) {
			items.push({
				value: 'jobs',
				label: m.automations(),
				icon: JobsIcon
			});
		}

		return items;
	});

	const urlTab = useUrlTab({
		validTabs: () => tabItems.map((tab) => tab.value),
		defaultTab: () => tabItems[0]?.value ?? 'features'
	});
	const activeTab = $derived(urlTab.value);

	const vulnerabilityManagementEnabled = $derived(featureStore.isEnabled('vulnerabilityManagement', environment.id));
	let securitySubTab = $state('trivy');
	const activeSecuritySubTab = $derived(
		!vulnerabilityManagementEnabled && securitySubTab === 'trivy' ? 'patching' : securitySubTab
	);
	const securityTabItems: TabItem[] = $derived([
		...(vulnerabilityManagementEnabled
			? [{ value: 'trivy', label: m.security_vulnerability_scanning_heading(), icon: ScanIcon }]
			: []),
		{ value: 'patching', label: m.security_image_patching_heading(), icon: ShieldCheckIcon },
		{ value: 'lifecycle', label: m.security_lifecycle_hooks_heading(), icon: CodeIcon }
	]);

	const snapshotValues = $derived(
		settings?.values ??
			(Object.fromEntries(
				Object.values(settingsStore.resolved?.meta ?? {}).map((meta) => [meta.key, parseSettingValue(meta.type, meta.default)])
			) as Partial<Settings>)
	);
	const formSchema = $derived(
		environmentFormSchema
			.pick(
				Object.fromEntries(
					Object.keys(environmentFormSchema.shape)
						.filter(
							(key) =>
								key in environmentUpdateSchema.shape ||
								key in snapshotValues ||
								featureDefinitions.some((feature) => feature.settingKey === key)
						)
						.map((key) => [key, true] as const)
				) as Partial<Record<keyof typeof environmentFormSchema.shape, true>>
			)
			.superRefine(requireTrivyServerUrl)
	);
	const settingFormKeys = $derived(
		(Object.keys(formSchema.shape) as (keyof EnvironmentFormValues)[]).filter((key) => !(key in environmentUpdateSchema.shape))
	);
	const currentSettings = $derived({
		...Object.fromEntries(settingFormKeys.map((key) => [key, snapshotValues[key as SettingKey]])),
		...(settingFormKeys.includes('buildProvider') && !depotReady('', '') && { buildProvider: 'local' }),
		...(settingFormKeys.includes('depotToken') && { depotToken: '' }),
		name: loadedEnvironment.name,
		enabled: loadedEnvironment.enabled
	} as EnvironmentFormValues);

	function settingLock(key: SettingKey) {
		const meta = settings?.meta[key];
		return meta?.locked ? meta : undefined;
	}

	function depotReady(projectId: string, token: string): boolean {
		if (settings?.runtime.depotConfigured) return true;
		return (
			Boolean(projectId.trim() || settings?.values.depotProjectId?.trim()) &&
			Boolean(token.trim() || settings?.values.depotToken?.trim())
		);
	}

	// Custom save handler for environment-specific settings
	async function saveEnvironmentSettings(formData: EnvironmentFormValues) {
		const environmentId = environment.id;
		const submittedInputs = formInputs;
		const changedFeatures = featureDefinitions.filter(
			(feature) => formData[feature.settingKey] !== currentSettings[feature.settingKey]
		);
		if (
			changedFeatures.length > 0 &&
			(!hasPermission('settings:write', environmentId) ||
				changedFeatures.some((feature) => !featureStore.isSupported(feature.id, environmentId)))
		) {
			throw new Error(m.features_unavailable());
		}
		if (formData.name !== environment.name || formData.enabled !== environment.enabled) {
			const update = changedFields(environment, environmentUpdateSchema.parse(formData));
			const result = await tryCatch(environmentManagementService.update(environmentId, update));
			if (result.error) {
				throw new Error(result.error.message);
			}
			if (environment.id === environmentId) {
				environment = result.data;
			}
		}
		if (settings) {
			await settingsService.updateSettingsForEnvironment(environmentId, settings, {
				...Object.fromEntries(settingFormKeys.map((key) => [key, formData[key]])),
				trivyCpuLimit: formData.trivyResourceLimitsEnabled ? formData.trivyCpuLimit : 0,
				trivyMemoryLimitMb: formData.trivyResourceLimitsEnabled ? formData.trivyMemoryLimitMb : 0,
				buildProvider: depotReady(formData.depotProjectId, formData.depotToken) ? formData.buildProvider : 'local',
				depotToken: formData.depotToken || undefined
			});
			submittedInputs.depotToken.value = '';
		}
		if (changedFeatures.length > 0) {
			await featureStore.refresh(environmentId);
			if (featureStore.status(environmentId) !== 'ready') throw new Error(m.features_unavailable());
			const overridden = changedFeatures.find(
				(feature) => featureStore.isEnabled(feature.id, environmentId) !== formData[feature.settingKey]
			);
			if (overridden) throw new Error(featureOverrideMessage(overridden.id));
		}

		// The root load's refresh re-reads the environment list into the store.
		await queryClient.invalidateQueries({ queryKey: queryKeys.environments.all });
		await queryClient.invalidateQueries({ queryKey: queryKeys.jobs.list(environmentId) });
		await refreshEnvironment();
	}

	function featureOverrideMessage(id: FeatureID): string {
		switch (id) {
			case 'swarm':
				return m.features_swarm_environment_override();
			case 'vulnerabilityManagement':
				return m.features_environment_override();
		}
	}

	function featureSwitchDisabled(id: FeatureID): boolean {
		return (
			!isCurrentlyOnline ||
			!settings ||
			settings.readOnly ||
			!hasPermission('settings:write', environment.id) ||
			!featureStore.isSupported(id, environment.id)
		);
	}

	let { formInputs, settingsForm, resetForm, onSubmit } = $derived(
		createSettingsForm({
			schema: formSchema,
			currentSettings,
			getCurrentSettings: () => ({
				...currentSettings,
				name: environment.name,
				enabled: environment.enabled
			}),
			onSave: saveEnvironmentSettings,
			successMessage: m.common_update_success({ resource: m.resource_environment_cap() }),
			errorMessage: m.common_update_failed({ resource: m.resource_environment() }),
			onReset: () => toast.info(m.changes_reset())
		})
	);

	const shellOptions = [
		{ value: '/bin/sh', label: '/bin/sh', description: m.docker_shell_sh_description() },
		{ value: '/bin/bash', label: '/bin/bash', description: m.docker_shell_bash_description() },
		{ value: '/bin/ash', label: '/bin/ash', description: m.docker_shell_ash_description() },
		{ value: '/bin/zsh', label: '/bin/zsh', description: m.docker_shell_zsh_description() }
	];

	let shellSelectValue = $derived.by((): string => {
		if (!settings) return 'custom';
		return shellOptions.find((o) => o.value === settings.values.defaultShell)?.value ?? 'custom';
	});

	function handleShellSelectChange(value: string) {
		if (value !== 'custom') {
			formInputs.defaultShell.value = value;
		}
	}

	async function refreshEnvironment() {
		if (isRefreshing) return;
		try {
			const operationResult = await tryCatch(
				(async () => {
					isRefreshing = true;
					await featureStore.refresh(environment.id);
					if (environment.id !== '0' && isCurrentlyOnline) await versionQuery.refetch();
					await refreshAll();
				})()
			);
			if (operationResult.error !== null) {
				const err = operationResult.error;

				console.error('Failed to refresh environment:', err);
				toast.error(m.common_refresh_failed({ resource: m.resource_environment() }));
			}
		} finally {
			isRefreshing = false;
		}
	}

	async function syncEnvironment() {
		if (isSyncing) return;
		try {
			const operationResult = await tryCatch(
				(async () => {
					isSyncing = true;
					await environmentManagementService.sync(environment.id);
					toast.success(m.sync_environment_success());
				})()
			);
			if (operationResult.error !== null) {
				const error = operationResult.error;

				console.error('Failed to sync environment:', error);
				toast.error(m.sync_environment_failed());
			}
		} finally {
			isSyncing = false;
		}
	}

	async function testConnection() {
		if (isTestingConnection) return;
		try {
			const operationResult = await tryCatch(
				(async () => {
					isTestingConnection = true;
					const result = await environmentManagementService.testConnection(environment.id);

					if (result.status === 'online') {
						toast.success(m.environments_test_connection_success());
					} else {
						toast.error(m.environments_test_connection_error());
					}

					await refreshAll();
				})()
			);
			if (operationResult.error !== null) {
				const error = operationResult.error;

				toast.error(m.environments_test_connection_failed(), { description: extractApiErrorMessage(error) });
			}
		} finally {
			isTestingConnection = false;
		}
	}

	async function handleRegenerateApiKey() {
		try {
			const operationResult = await tryCatch(
				(async () => {
					isRegeneratingKey = true;

					// Delete the old API key and create a new one
					const result = await environmentManagementService.update(environment.id, {
						regenerateApiKey: true
					});

					if (result.apiKey) {
						regeneratedApiKey = result.apiKey;
						const snippets = await tryCatch(
							queryClient.query({
								queryKey: queryKeys.environments.deploymentSnippets(environment.id),
								queryFn: () => environmentManagementService.getDeploymentSnippets(environment.id),
								staleTime: 0
							})
						);
						regeneratedSnippets = snippets.data;
						toast.success(m.environments_regenerate_key_success());
						await refreshAll();
					} else {
						toast.error(m.environments_regenerate_key_failed());
					}
				})()
			);
			if (operationResult.error !== null) {
				const error = operationResult.error;

				console.error('Failed to regenerate API key:', error);
				toast.error(m.environments_regenerate_key_failed());
			}
		} finally {
			isRegeneratingKey = false;
			showRegenerateDialog = false;
		}
	}
</script>

{#snippet settingsOffline()}
	<EmptyState
		icon={ConnectionIcon}
		title={m.environments_settings_offline_title()}
		description={m.environments_settings_offline_description()}
		actionLabel={isTestingConnection ? undefined : m.test_connection()}
		onAction={testConnection}
	/>
{/snippet}

{#snippet enabledIndicator()}
	<label for="env-enabled-header" class="flex shrink-0 cursor-pointer items-center gap-2 text-sm">
		<span
			class={cn(
				'size-2 rounded-full transition-colors',
				formInputs.enabled.value ? 'bg-success shadow-glow shadow-success' : 'bg-muted-foreground/40'
			)}
		></span>
		<span class="hidden sm:inline">{formInputs.enabled.value ? m.common_enabled() : m.common_disabled()}</span>
		<Switch id="env-enabled-header" bind:checked={formInputs.enabled.value} />
	</label>
{/snippet}

{#snippet environmentHeader()}
	<div class="flex min-w-0 flex-col gap-1">
		<div class="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
			<EditableName
				bind:value={formInputs.name.value}
				bind:ref={nameInputRef}
				variant="inline"
				error={formInputs.name.error ?? undefined}
				originalValue={environment.name}
				placeholder={m.environments_name_placeholder()}
				class="max-w-56 min-w-0 sm:max-w-80 md:max-w-104"
			/>
			<EnvironmentStatusSummary
				environment={runtimeEnvironment}
				{currentStatus}
				{isLoadingVersion}
				{remoteVersion}
				{versionInformation}
			/>
		</div>
		{@render apiUrlLine()}
	</div>
{/snippet}

{#snippet apiUrlLine()}
	<div class="flex min-w-0 items-center gap-1">
		<span class="block min-w-0 truncate px-1 font-mono text-xs text-muted-foreground">{environment.apiUrl}</span>
		<CopyButton text={environment.apiUrl} size="icon" class="size-6 shrink-0" />
		<span class="shrink-0 font-mono text-xs text-muted-foreground/70">#{environment.id}</span>
	</div>
{/snippet}

{#snippet environmentHeaderActions()}
	{#if settingsForm.hasChanges}
		<span class="hidden text-xs text-warning md:inline">{m.common_unsaved_changes()}</span>
	{/if}
	{#if environment.id !== '0'}
		{@render enabledIndicator()}
	{/if}
	<ActionButtonGroup buttons={headerActions} />
{/snippet}

{#snippet environmentSubHeader()}
	{#if !environment.enabled || !isCurrentlyOnline || !settings}
		<div class="flex items-start gap-3 rounded-lg border border-warning/30 bg-warning/10 p-4 text-warning">
			<AlertIcon class="mt-0.5 size-5 shrink-0 text-warning" />
			<div class="flex-1 space-y-1">
				<p class="text-sm font-medium">
					{#if !environment.enabled}
						{m.environments_warning_disabled()}
					{:else if !isCurrentlyOnline}
						{m.common_status()}: {currentStatus === 'pending'
							? m.common_pending()
							: currentStatus === 'error'
								? m.common_error()
								: m.common_offline()}
					{:else if !settings}
						{m.environments_warning_no_settings()}
					{/if}
				</p>
			</div>
		</div>
	{/if}
	{#if regeneratedApiKey}
		<div class="rounded-lg border border-success/30 bg-success/10 p-4 text-success">
			<div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
				<div class="min-w-0 flex-1 space-y-2">
					<p class="text-sm font-medium">{m.environments_new_api_key()}</p>
					<div class="flex items-center gap-2">
						<code class="flex-1 rounded-md bg-background/70 px-3 py-2 font-mono text-sm break-all">
							{regeneratedApiKey}
						</code>
						<CopyButton text={regeneratedApiKey} size="icon" class="size-8 shrink-0" />
					</div>
					<p class="text-sm">{m.environments_api_key_save_warning()}</p>
					{#if regeneratedSnippets}
						<AgentCommandBlock
							dockerRunLabel={m.docker_run_command()}
							dockerRun={regeneratedSnippets.dockerRun}
							dockerComposeLabel={m.common_docker_compose()}
							dockerCompose={regeneratedSnippets.dockerCompose}
						/>
					{/if}
				</div>

				<ArcaneButton
					action="base"
					tone="outline"
					onclick={() => {
						regeneratedApiKey = null;
						regeneratedSnippets = null;
					}}
					customLabel={m.common_dismiss()}
					class="shrink-0"
				/>
			</div>
		</div>
	{/if}
{/snippet}

<!-- The compact header stays sticky; the floating pill would leave an empty band in its place. -->
<TabbedPageLayout
	backUrl="/environments"
	backLabel={m.common_back()}
	{tabItems}
	selectedTab={activeTab}
	onTabChange={urlTab.select}
	showFloatingHeader={false}
>
	{#snippet headerInfo()}
		{@render environmentHeader()}
	{/snippet}

	{#snippet headerActions()}
		{@render environmentHeaderActions()}
	{/snippet}

	{#snippet subHeader()}
		{@render environmentSubHeader()}
	{/snippet}

	{#snippet tabContent()}
		<Tabs.Content value="features">
			<SettingsSection
				id="features"
				title={m.features_title()}
				description={featureDefinitions.some((feature) => !featureStore.isSupported(feature.id, environment.id))
					? m.features_unsupported()
					: undefined}
			>
				{#if featureStore.status(environment.id) === 'ready'}
					<SettingsRow
						for="vulnerability-management"
						label={m.features_vulnerability_management()}
						description={m.features_vulnerability_description()}
						error={formInputs.featureVulnerabilityManagementEnabled.error}
						layout="switch"
					>
						<Switch
							id="vulnerability-management"
							bind:checked={formInputs.featureVulnerabilityManagementEnabled.value}
							disabled={featureSwitchDisabled('vulnerabilityManagement')}
						/>
					</SettingsRow>
					{#if swarmActive}
						<!-- An active cluster keeps Swarm on, so the stored toggle is shown locked. -->
						<SettingsRow for="swarm" label={m.swarm()} description={m.features_swarm_locked()} layout="switch">
							<Switch id="swarm" checked={true} disabled />
						</SettingsRow>
					{:else}
						<SettingsRow
							for="swarm"
							label={m.swarm()}
							description={m.features_swarm_description()}
							error={formInputs.featureSwarmEnabled.error}
							layout="switch"
						>
							<Switch id="swarm" bind:checked={formInputs.featureSwarmEnabled.value} disabled={featureSwitchDisabled('swarm')} />
						</SettingsRow>
					{/if}
				{:else}
					<div class="flex flex-col items-start gap-3 px-5 py-4">
						<p role="status" class="text-sm text-muted-foreground">{m.features_unavailable()}</p>
						<ArcaneButton action="base" customLabel={m.common_retry()} onclick={refreshEnvironment} />
					</div>
				{/if}
			</SettingsSection>
		</Tabs.Content>

		{#if runtimeEnvironment.id !== '0'}
			<Tabs.Content value="connection">
				<ConnectionAgentTab
					environment={runtimeEnvironment}
					{currentStatus}
					{showMTLSDownloads}
					{isRegeneratingKey}
					onRegenerateApiKey={() => (showRegenerateDialog = true)}
				/>
			</Tabs.Content>
		{/if}

		{#if showSettingsTabs && settingsAvailable}
			<Tabs.Content value="storage">
				<StorageTab bind:formInputs />
			</Tabs.Content>

			<Tabs.Content value="docker">
				<DockerTab bind:formInputs environmentId={environment.id} {shellSelectValue} {handleShellSelectChange} {shellOptions} />
			</Tabs.Content>

			<Tabs.Content value="security">
				<Tabs.Root value={activeSecuritySubTab} class="w-full">
					<div class="mb-4">
						<TabBar
							items={securityTabItems}
							value={activeSecuritySubTab}
							onValueChange={(value) => {
								securitySubTab = value;
							}}
						/>
					</div>
					<Tabs.Content value="trivy">
						<TrivySecuritySettings bind:formInputs environmentId={environment.id} />
					</Tabs.Content>
					<Tabs.Content value="patching">
						<ImagePatchSettings bind:formInputs environmentId={environment.id} />
					</Tabs.Content>
					<Tabs.Content value="lifecycle">
						<LifecycleSecuritySettings bind:formInputs />
					</Tabs.Content>
				</Tabs.Root>
			</Tabs.Content>

			<Tabs.Content value="timeouts">
				<TimeoutsTab bind:formInputs lock={settingLock} />
			</Tabs.Content>

			<Tabs.Content value="activity">
				<ActivityTab bind:formInputs lock={settingLock} />
			</Tabs.Content>

			<Tabs.Content value="builds">
				<BuildsTab bind:formInputs settings={settings!} lock={settingLock} />
			</Tabs.Content>
		{:else if showSettingsTabs}
			{#each ['storage', 'docker', 'security', 'timeouts', 'activity', 'builds'] as tab (tab)}
				<Tabs.Content value={tab}>
					{@render settingsOffline()}
				</Tabs.Content>
			{/each}
		{/if}

		{#if showJobsTab && settingsAvailable}
			<Tabs.Content value="jobs">
				<JobsTab bind:formInputs environmentId={environment.id} />
			</Tabs.Content>
		{:else if showJobsTab}
			<Tabs.Content value="jobs">
				{@render settingsOffline()}
			</Tabs.Content>
		{/if}
	{/snippet}
</TabbedPageLayout>

<AlertDialog.Root bind:open={showRegenerateDialog}>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>{m.environments_regenerate_dialog_title()}</AlertDialog.Title>
			<AlertDialog.Description>
				{m.environments_regenerate_dialog_message()}
			</AlertDialog.Description>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel>{m.common_cancel()}</AlertDialog.Cancel>
			<AlertDialog.Action onclick={handleRegenerateApiKey}>
				{m.environments_regenerate_api_key()}
			</AlertDialog.Action>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
{#key `${easyJoinSession}:${easyJoinCandidates.managerFor(runtimeEnvironment.id)}`}
	<EasyJoinDialog
		bind:open={easyJoinDialogOpen}
		managerEnvironmentId={easyJoinCandidates.managerFor(runtimeEnvironment.id)}
		targetEnvironmentId={runtimeEnvironment.id}
		onComplete={easyJoinCandidates.refresh}
	/>
{/key}
