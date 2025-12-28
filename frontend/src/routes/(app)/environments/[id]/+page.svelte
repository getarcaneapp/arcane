<script lang="ts">
	import * as Card from '$lib/components/ui/card/index.js';
	import * as Tabs from '$lib/components/ui/tabs/index.js';
	import { TabBar, type TabItem } from '$lib/components/tab-bar';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import * as AlertDialog from '$lib/components/ui/alert-dialog';
	import * as ArcaneTooltip from '$lib/components/arcane-tooltip';
	import { goto, invalidateAll } from '$app/navigation';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { toast } from 'svelte-sonner';
	import Label from '$lib/components/ui/label/label.svelte';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Switch } from '$lib/components/ui/switch/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { m } from '$lib/paraglide/messages';
	import { environmentManagementService } from '$lib/services/env-mgmt-service.js';
	import { settingsService } from '$lib/services/settings-service';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import { CopyButton } from '$lib/components/ui/copy-button';
	import type { AppVersionInformation } from '$lib/types/application-configuration';
	import SelectWithLabel from '$lib/components/form/select-with-label.svelte';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
	import MobileFloatingFormActions from '$lib/components/form/mobile-floating-form-actions.svelte';
	import {
		ArrowLeftIcon,
		EnvironmentsIcon,
		AlertIcon,
		TestIcon,
		RegistryIcon,
		ResetIcon,
		ApiKeyIcon,
		DockerBrandIcon,
		SettingsIcon
	} from '$lib/icons';

	let { data } = $props();
	let { environment, settings, versionInformation } = $derived(data);

	let currentEnvironment = $derived(environmentStore.selected);

	let activeTab = $state('general');

	const tabItems: TabItem[] = [
		{
			value: 'general',
			label: m.general_title(),
			icon: SettingsIcon
		},
		{
			value: 'docker',
			label: m.environments_docker_settings_title(),
			icon: DockerBrandIcon
		}
	];

	let isRefreshing = $state(false);
	let isTestingConnection = $state(false);
	let isSaving = $state(false);
	let isSyncingRegistries = $state(false);
	let isRegeneratingKey = $state(false);
	let showRegenerateDialog = $state(false);
	let regeneratedApiKey = $state<string | null>(null);

	// Version state
	let remoteVersion = $state<AppVersionInformation | null>(null);
	let isLoadingVersion = $state(false);

	// Form state
	let formName = $state('');
	let formEnabled = $state(false);
	let formApiUrl = $state('');

	// Settings form state
	let formPollingEnabled = $state(false);
	let formPollingInterval = $state(60);
	let formAutoUpdate = $state(false);
	let formAutoUpdateInterval = $state(1440);
	let formAutoInjectEnv = $state(false);
	let formPruneMode = $state<'all' | 'dangling'>('dangling');
	let formDefaultShell = $state('/bin/sh');
	let formProjectsDirectory = $state('data/projects');
	let formDiskUsagePath = $state('data/projects');
	let formMaxImageUploadSize = $state(500);
	let formBaseServerUrl = $state('http://localhost');

	type PollingIntervalMode = 'hourly' | 'daily' | 'weekly' | 'custom';

	const imagePollingOptions: Array<{
		value: PollingIntervalMode;
		label: string;
		description: string;
		minutes?: number;
	}> = [
		{ value: 'hourly', minutes: 60, label: m.hourly(), description: m.polling_hourly_description() },
		{ value: 'daily', minutes: 1440, label: m.daily(), description: m.polling_daily_description() },
		{ value: 'weekly', minutes: 10080, label: m.weekly(), description: m.polling_weekly_description() },
		{ value: 'custom', label: m.custom(), description: m.use_custom_polling_value() }
	];

	const presetToMinutes = Object.fromEntries(
		imagePollingOptions.filter((o) => o.value !== 'custom').map((o) => [o.value, o.minutes!])
	) as Record<Exclude<PollingIntervalMode, 'custom'>, number>;

	let pollingIntervalMode = $state<PollingIntervalMode>('custom');

	const pruneModeOptions = [
		{ value: 'all', label: m.docker_prune_all(), description: m.docker_prune_all_description() },
		{ value: 'dangling', label: m.docker_prune_dangling(), description: m.docker_prune_dangling_description() }
	];

	let pruneModeDescription = $derived(
		pruneModeOptions.find((o) => o.value === formPruneMode)?.description ?? m.docker_prune_mode_description()
	);

	const shellOptions = [
		{ value: '/bin/sh', label: '/bin/sh', description: m.docker_shell_sh_description() },
		{ value: '/bin/bash', label: '/bin/bash', description: m.docker_shell_bash_description() },
		{ value: '/bin/ash', label: '/bin/ash', description: m.docker_shell_ash_description() },
		{ value: '/bin/zsh', label: '/bin/zsh', description: m.docker_shell_zsh_description() }
	];

	let shellSelectValue = $state<string>('custom');

	// Track current status separately from environment data
	let currentStatus = $state<'online' | 'offline' | 'error' | 'pending'>('offline');

	// Initialize form values and status
	$effect(() => {
		formName = environment.name;
		formEnabled = environment.enabled;
		formApiUrl = environment.apiUrl;
		currentStatus = environment.status;

		if (settings) {
			formPollingEnabled = settings.pollingEnabled;
			formPollingInterval = settings.pollingInterval;
			formAutoUpdate = settings.autoUpdate;
			formAutoUpdateInterval = settings.autoUpdateInterval;
			formAutoInjectEnv = settings.autoInjectEnv;
			formPruneMode = settings.dockerPruneMode || 'dangling';
			formDefaultShell = settings.defaultShell || '/bin/sh';
			formProjectsDirectory = settings.projectsDirectory || 'data/projects';
			formDiskUsagePath = settings.diskUsagePath || 'data/projects';
			formMaxImageUploadSize = settings.maxImageUploadSize || 500;
			formBaseServerUrl = settings.baseServerUrl || 'http://localhost';

			// Initialize derived states
			pollingIntervalMode = imagePollingOptions.find((o) => o.minutes === settings.pollingInterval)?.value ?? 'custom';
			shellSelectValue = shellOptions.find((o) => o.value === settings.defaultShell)?.value ?? 'custom';
		}
	});

	// Sync polling mode select with form value
	$effect(() => {
		if (pollingIntervalMode !== 'custom') {
			formPollingInterval = presetToMinutes[pollingIntervalMode];
		}
	});

	// Sync shell select with form value
	$effect(() => {
		if (shellSelectValue !== 'custom') {
			formDefaultShell = shellSelectValue;
		}
	});

	// Fetch version when environment is online
	$effect(() => {
		if (environment.id !== '0' && currentStatus === 'online' && !remoteVersion && !isLoadingVersion) {
			fetchVersion();
		}
	});

	async function fetchVersion() {
		try {
			isLoadingVersion = true;
			remoteVersion = await environmentManagementService.getVersion(environment.id);
		} catch (err) {
			console.error('Failed to fetch environment version:', err);
		} finally {
			isLoadingVersion = false;
		}
	}

	// Track changes
	let hasChanges = $derived(
		formName !== environment.name ||
			formEnabled !== environment.enabled ||
			(environment.id !== '0' && formApiUrl !== environment.apiUrl) ||
			(settings &&
				(formPollingEnabled !== settings.pollingEnabled ||
					formPollingInterval !== settings.pollingInterval ||
					formAutoUpdate !== settings.autoUpdate ||
					formAutoUpdateInterval !== settings.autoUpdateInterval ||
					formAutoInjectEnv !== settings.autoInjectEnv ||
					formPruneMode !== (settings.dockerPruneMode || 'dangling') ||
					formDefaultShell !== (settings.defaultShell || '/bin/sh') ||
					formProjectsDirectory !== (settings.projectsDirectory || 'data/projects') ||
					formDiskUsagePath !== (settings.diskUsagePath || 'data/projects') ||
					formMaxImageUploadSize !== (settings.maxImageUploadSize || 500) ||
					formBaseServerUrl !== (settings.baseServerUrl || 'http://localhost')))
	);

	async function refreshEnvironment() {
		if (isRefreshing) return;
		try {
			isRefreshing = true;
			await invalidateAll();
			// Update form values after refresh
			formName = environment.name;
			formEnabled = environment.enabled;
			formApiUrl = environment.apiUrl;
			currentStatus = environment.status;

			if (settings) {
				formPollingEnabled = settings.pollingEnabled;
				formPollingInterval = settings.pollingInterval;
				formAutoUpdate = settings.autoUpdate;
				formAutoUpdateInterval = settings.autoUpdateInterval;
				formAutoInjectEnv = settings.autoInjectEnv;
				formPruneMode = settings.dockerPruneMode || 'dangling';
				formDefaultShell = settings.defaultShell || '/bin/sh';

				// Initialize derived states
				pollingIntervalMode = imagePollingOptions.find((o) => o.minutes === settings.pollingInterval)?.value ?? 'custom';
				shellSelectValue = shellOptions.find((o) => o.value === settings.defaultShell)?.value ?? 'custom';
			}

			// Reset version to trigger re-fetch if online
			remoteVersion = null;
		} catch (err) {
			console.error('Failed to refresh environment:', err);
			toast.error(m.common_refresh_failed({ resource: m.resource_environment() }));
		} finally {
			isRefreshing = false;
		}
	}

	async function syncRegistries() {
		if (isSyncingRegistries) return;
		try {
			isSyncingRegistries = true;
			await environmentManagementService.syncRegistries(environment.id);
			toast.success('Registries synced successfully');
		} catch (error) {
			console.error('Failed to sync registries:', error);
			toast.error('Failed to sync registries');
		} finally {
			isSyncingRegistries = false;
		}
	}

	async function testConnection() {
		if (isTestingConnection) return;
		try {
			isTestingConnection = true;
			const customUrl = formApiUrl !== environment.apiUrl ? formApiUrl : undefined;
			const result = await environmentManagementService.testConnection(environment.id, customUrl);

			// Update current status based on test result
			currentStatus = result.status;

			if (result.status === 'online') {
				toast.success(m.environments_test_connection_success());
			} else {
				toast.error(m.environments_test_connection_error());
			}

			// If testing with saved URL (not custom), refresh to get backend's updated status
			if (!customUrl) {
				await invalidateAll();
			}
		} catch (error) {
			// Update status to offline on error
			currentStatus = 'offline';
			toast.error(m.environments_test_connection_failed());
			console.error(error);
		} finally {
			isTestingConnection = false;
		}
	}

	async function handleSave() {
		if (!hasChanges || isSaving) return;

		try {
			isSaving = true;

			// Update environment basic info
			await environmentManagementService.update(environment.id, {
				name: formName,
				enabled: formEnabled,
				apiUrl: formApiUrl
			});

			// Update environment settings if they exist
			if (settings) {
				await settingsService.updateSettingsForEnvironment(environment.id, {
					pollingEnabled: formPollingEnabled,
					pollingInterval: formPollingInterval,
					autoUpdate: formAutoUpdate,
					autoUpdateInterval: formAutoUpdateInterval,
					autoInjectEnv: formAutoInjectEnv,
					dockerPruneMode: formPruneMode,
					defaultShell: formDefaultShell,
					projectsDirectory: formProjectsDirectory,
					diskUsagePath: formDiskUsagePath,
					maxImageUploadSize: formMaxImageUploadSize,
					baseServerUrl: formBaseServerUrl
				});
			}

			toast.success(m.common_update_success({ resource: m.resource_environment_cap() }));
			await refreshEnvironment();

			// Update environment store if this is the current environment
			if (currentEnvironment?.id === environment.id) {
				await environmentStore.initialize(
					(
						await environmentManagementService.getEnvironments({
							pagination: { page: 1, limit: 1000 }
						})
					).data
				);
			}
		} catch (error) {
			console.error('Failed to save environment:', error);
			toast.error(m.common_update_failed({ resource: m.resource_environment() }));
		} finally {
			isSaving = false;
		}
	}

	function handleReset() {
		formName = environment.name;
		formEnabled = environment.enabled;
		formApiUrl = environment.apiUrl;

		if (settings) {
			formPollingEnabled = settings.pollingEnabled;
			formPollingInterval = settings.pollingInterval;
			formAutoUpdate = settings.autoUpdate;
			formAutoUpdateInterval = settings.autoUpdateInterval;
			formAutoInjectEnv = settings.autoInjectEnv;
			formPruneMode = settings.dockerPruneMode || 'dangling';
			formDefaultShell = settings.defaultShell || '/bin/sh';
			formProjectsDirectory = settings.projectsDirectory || 'data/projects';
			formDiskUsagePath = settings.diskUsagePath || 'data/projects';
			formMaxImageUploadSize = settings.maxImageUploadSize || 500;
			formBaseServerUrl = settings.baseServerUrl || 'http://localhost';

			// Initialize derived states
			pollingIntervalMode = imagePollingOptions.find((o) => o.minutes === settings.pollingInterval)?.value ?? 'custom';
			shellSelectValue = shellOptions.find((o) => o.value === settings.defaultShell)?.value ?? 'custom';
		}

		toast.info(m.environments_changes_reset());
	}

	async function handleRegenerateApiKey() {
		try {
			isRegeneratingKey = true;

			// Delete the old API key and create a new one
			const result = await environmentManagementService.update(environment.id, {
				regenerateApiKey: true
			});

			if (result.apiKey) {
				regeneratedApiKey = result.apiKey;
				toast.success(m.environments_regenerate_key_success());
				await invalidateAll();
			} else {
				toast.error(m.environments_regenerate_key_failed());
			}
		} catch (error) {
			console.error('Failed to regenerate API key:', error);
			toast.error(m.environments_regenerate_key_failed());
		} finally {
			isRegeneratingKey = false;
			showRegenerateDialog = false;
		}
	}
</script>

<div class="container mx-auto max-w-full space-y-6 overflow-hidden p-2 sm:p-6">
	<div class="space-y-3 sm:space-y-4">
		<ArcaneButton
			action="base"
			tone="ghost"
			onclick={() => goto('/environments')}
			class="w-fit gap-2"
			icon={ArrowLeftIcon}
			customLabel={m.common_back_to({ resource: m.environments_title() })}
		/>

		<div class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
			<div class="flex-1">
				<h1 class="text-xl font-bold wrap-break-word sm:text-2xl">{environment.name}</h1>
				<p class="text-muted-foreground mt-1.5 text-sm wrap-break-word sm:text-base">{m.environments_page_subtitle()}</p>
			</div>

			<div class="flex flex-wrap items-center gap-2">
				<div class="hidden items-center gap-2 sm:flex">
					{#if hasChanges}
						<span class="text-xs text-orange-600 dark:text-orange-400">{m.environments_unsaved_changes()}</span>
					{:else}
						<span class="text-xs text-green-600 dark:text-green-400">{m.environments_all_changes_saved()}</span>
					{/if}

					{#if hasChanges}
						<ArcaneButton
							action="restart"
							tone="outline"
							onclick={handleReset}
							disabled={isSaving}
							customLabel={m.common_reset()}
						/>
					{/if}

					<ArcaneButton
						action="save"
						onclick={handleSave}
						disabled={!hasChanges || isSaving}
						loading={isSaving}
						customLabel={m.common_save()}
						loadingLabel={m.common_saving()}
					/>
				</div>

				{#if environment.id !== '0'}
					<ArcaneButton
						action="base"
						tone="outline"
						onclick={syncRegistries}
						disabled={isSyncingRegistries}
						loading={isSyncingRegistries}
						icon={RegistryIcon}
						customLabel={m.sync_registries()}
					/>
				{/if}

				<ArcaneButton
					action="refresh"
					tone="outline"
					onclick={refreshEnvironment}
					disabled={isRefreshing}
					loading={isRefreshing}
				/>
			</div>
		</div>

		<div class="flex flex-wrap items-center gap-2">
			<Badge variant="outline" class="gap-1">
				<div class="size-2 rounded-full {currentStatus === 'online' ? 'bg-green-500' : 'bg-red-500'}"></div>
				{currentStatus === 'online' ? m.common_online() : m.common_offline()}
			</Badge>
			<Badge variant="outline" class="gap-1">
				{environment.enabled ? m.common_enabled() : m.common_disabled()}
			</Badge>
			{#if environment.id === '0'}
				<Badge variant="outline">{m.environments_local_badge()}</Badge>
			{/if}
		</div>

		{#if !environment.enabled || currentStatus === 'offline' || !settings}
			<div
				class="flex items-start gap-3 rounded-lg border border-amber-500/30 bg-amber-500/10 p-4 text-amber-900 dark:text-amber-200"
			>
				<AlertIcon class="mt-0.5 size-5 shrink-0 text-amber-600 dark:text-amber-400" />
				<div class="flex-1 space-y-1">
					<p class="text-sm font-medium">
						{#if !environment.enabled}
							{m.environments_warning_disabled()}
						{:else if currentStatus === 'offline'}
							{m.environments_warning_offline()}
						{:else if !settings}
							{m.environments_warning_no_settings()}
						{/if}
					</p>
				</div>
			</div>
		{/if}
	</div>

	<div class="grid gap-6 gap-x-6 gap-y-6 lg:grid-cols-2">
		<Card.Root class="flex flex-col">
			<Card.Header icon={EnvironmentsIcon}>
				<div class="flex flex-col space-y-1.5">
					<Card.Title>
						<h2>{m.environments_overview_title()}</h2>
					</Card.Title>
					<Card.Description>{m.environments_basic_info_description()}</Card.Description>
				</div>
			</Card.Header>
			<Card.Content class="space-y-4 p-4">
				<div>
					<Label for="env-name" class="text-sm font-medium">{m.common_name()}</Label>
					<Input
						id="env-name"
						type="text"
						bind:value={formName}
						class="mt-1.5 w-full"
						placeholder={m.environments_name_placeholder()}
					/>
				</div>

				<div>
					<Label for="api-url" class="text-sm font-medium">{m.environments_api_url()}</Label>
					<div class="mt-1.5 flex items-center gap-2">
						{#if environment.id === '0'}
							<ArcaneTooltip.Root>
								<ArcaneTooltip.Trigger class="w-full">
									<Input
										id="api-url"
										type="url"
										bind:value={formApiUrl}
										class="w-full font-mono"
										placeholder={m.environments_api_url_placeholder()}
										disabled={true}
										required
									/>
								</ArcaneTooltip.Trigger>
								<ArcaneTooltip.Content>
									<p>{m.environments_local_setting_disabled()}</p>
								</ArcaneTooltip.Content>
							</ArcaneTooltip.Root>
						{:else}
							<Input
								id="api-url"
								type="url"
								bind:value={formApiUrl}
								class="w-full font-mono"
								placeholder={m.environments_api_url_placeholder()}
								required
							/>
						{/if}
						<ArcaneButton
							action="base"
							onclick={testConnection}
							disabled={isTestingConnection}
							loading={isTestingConnection}
							icon={TestIcon}
							customLabel={m.environments_test_connection()}
							loadingLabel={m.environments_testing_connection()}
							class="shrink-0"
						/>
					</div>
					<p class="text-muted-foreground mt-1.5 text-xs">{m.environments_api_url_help()}</p>
				</div>

				<div class="flex items-center justify-between rounded-lg border p-4">
					<div class="space-y-0.5">
						<Label for="env-enabled" class="text-sm font-medium">{m.common_enabled()}</Label>
						<div class="text-muted-foreground text-xs">{m.environments_enable_disable_description()}</div>
					</div>
					{#if environment.id === '0'}
						<ArcaneTooltip.Root>
							<ArcaneTooltip.Trigger>
								<Switch id="env-enabled" disabled={true} bind:checked={formEnabled} />
							</ArcaneTooltip.Trigger>
							<ArcaneTooltip.Content>
								<p>{m.environments_local_setting_disabled()}</p>
							</ArcaneTooltip.Content>
						</ArcaneTooltip.Root>
					{:else}
						<Switch id="env-enabled" bind:checked={formEnabled} />
					{/if}
				</div>

				<div class="grid grid-cols-2 gap-4 rounded-lg border p-4">
					<div>
						<Label class="text-muted-foreground text-xs font-medium">{m.environments_environment_id_label()}</Label>
						<div class="mt-1 font-mono text-sm">{environment.id}</div>
					</div>
					<div>
						<Label class="text-muted-foreground text-xs font-medium">{m.common_status()}</Label>
						<div class="mt-1">
							<StatusBadge
								text={currentStatus === 'online' ? m.common_online() : m.common_offline()}
								variant={currentStatus === 'online' ? 'green' : 'red'}
							/>
						</div>
					</div>
					<div class="col-span-2 border-t pt-4">
						<Label class="text-muted-foreground text-xs font-medium">{m.version_info_version()}</Label>
						<div class="mt-1 flex items-center gap-2">
							{#if environment.id === '0'}
								<span class="font-mono text-sm">{versionInformation?.currentVersion || 'Unknown'}</span>
								{#if versionInformation?.updateAvailable}
									<Badge variant="secondary" class="bg-amber-500/10 text-amber-600 hover:bg-amber-500/20 dark:text-amber-400">
										{m.sidebar_update_available()}: {versionInformation.newestVersion}
									</Badge>
								{/if}
							{:else if isLoadingVersion}
								<Spinner />
								<span class="text-muted-foreground text-sm">{m.common_action_checking()}</span>
							{:else if remoteVersion}
								<span class="font-mono text-sm">{remoteVersion.currentVersion}</span>
								{#if remoteVersion.updateAvailable}
									<Badge variant="secondary" class="bg-amber-500/10 text-amber-600 hover:bg-amber-500/20 dark:text-amber-400">
										{m.sidebar_update_available()}: {remoteVersion.newestVersion}
									</Badge>
									{#if remoteVersion.releaseUrl}
										<a
											href={remoteVersion.releaseUrl}
											target="_blank"
											rel="noopener noreferrer"
											class="text-xs text-blue-500 hover:underline"
										>
											{m.version_info_view_release()}
										</a>
									{/if}
								{/if}
							{:else if currentStatus === 'online'}
								<span class="text-muted-foreground text-sm">Version information unavailable</span>
							{:else}
								<span class="text-muted-foreground text-sm">{m.common_offline()}</span>
							{/if}
						</div>
					</div>
				</div>
			</Card.Content>
		</Card.Root>

		{#if settings}
			<Card.Root class="flex flex-col">
				<Card.Header icon={SettingsIcon}>
					<div class="flex flex-col space-y-1.5">
						<Card.Title>
							<h2>{m.settings_title()}</h2>
						</Card.Title>
						<Card.Description>{m.environments_config_description()}</Card.Description>
					</div>
				</Card.Header>
				<Card.Content class="p-0">
					<Tabs.Root bind:value={activeTab} class="w-full">
						<div class="border-b px-4 py-2">
							<div class="w-fit">
								<TabBar items={tabItems} value={activeTab} onValueChange={(value) => (activeTab = value)} />
							</div>
						</div>
						<Tabs.Content value="general" class="space-y-6 p-4">
							<div class="grid gap-6 sm:grid-cols-2">
								<div class="space-y-2">
									<TextInputWithLabel
										id="projects-directory"
										label={m.general_projects_directory_label()}
										bind:value={formProjectsDirectory}
										helpText={m.general_projects_directory_help()}
									/>
								</div>
								<div class="space-y-2">
									<TextInputWithLabel
										id="disk-usage-path"
										label={m.disk_usage_settings()}
										bind:value={formDiskUsagePath}
										helpText={m.disk_usage_settings_description()}
									/>
								</div>
								<div class="space-y-2">
									<TextInputWithLabel
										id="base-server-url"
										label={m.general_base_url_label()}
										bind:value={formBaseServerUrl}
										helpText={m.general_base_url_help()}
									/>
								</div>
								<div class="space-y-2">
									<TextInputWithLabel
										id="max-upload-size"
										type="number"
										label={m.docker_max_upload_size_label()}
										bind:value={formMaxImageUploadSize}
										helpText={m.docker_max_upload_size_description()}
									/>
								</div>
							</div>
						</Tabs.Content>
						<Tabs.Content value="docker" class="space-y-6 p-4">
							<div class="grid gap-6 sm:grid-cols-2">
								<!-- Polling Settings -->
								<div class="space-y-4 rounded-lg border p-4">
									<div class="flex items-center justify-between">
										<div class="space-y-0.5">
											<Label for="polling-enabled" class="text-sm font-medium">{m.docker_enable_polling_label()}</Label>
											<div class="text-muted-foreground text-xs">{m.docker_enable_polling_description()}</div>
										</div>
										<Switch id="polling-enabled" bind:checked={formPollingEnabled} />
									</div>

									{#if formPollingEnabled}
										<div class="space-y-3 pt-2">
											<SelectWithLabel
												id="pollingIntervalMode"
												name="pollingIntervalMode"
												bind:value={pollingIntervalMode}
												label={m.docker_polling_interval_label()}
												placeholder={m.docker_polling_interval_placeholder_select()}
												options={imagePollingOptions.map(({ value, label, description }) => ({
													value,
													label,
													description
												}))}
											/>

											{#if pollingIntervalMode === 'custom'}
												<TextInputWithLabel
													bind:value={formPollingInterval}
													label={m.custom_polling_interval()}
													placeholder={m.docker_polling_interval_placeholder()}
													helpText={m.docker_polling_interval_description()}
													type="number"
												/>
											{/if}

											{#if formPollingInterval < 30}
												<div
													class="flex items-start gap-3 rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-amber-900 dark:text-amber-200"
												>
													<AlertIcon class="mt-0.5 size-4 shrink-0 text-amber-600 dark:text-amber-400" />
													<div class="flex-1 space-y-1">
														<p class="text-sm font-medium">{m.docker_rate_limit_warning_title()}</p>
														<p class="text-xs">{m.docker_rate_limit_warning_description()}</p>
													</div>
												</div>
											{/if}
										</div>
									{/if}
								</div>

								<!-- Auto Update Settings -->
								<div class="space-y-4 rounded-lg border p-4">
									<div class="flex items-center justify-between">
										<div class="space-y-0.5">
											<Label for="auto-update" class="text-sm font-medium">{m.docker_auto_update_label()}</Label>
											<div class="text-muted-foreground text-xs">{m.docker_auto_update_description()}</div>
										</div>
										<Switch id="auto-update" bind:checked={formAutoUpdate} disabled={!formPollingEnabled} />
									</div>

									{#if formAutoUpdate && formPollingEnabled}
										<div class="pt-2">
											<TextInputWithLabel
												bind:value={formAutoUpdateInterval}
												label={m.docker_auto_update_interval_label()}
												placeholder={m.docker_auto_update_interval_placeholder()}
												helpText={m.docker_auto_update_interval_description()}
												type="number"
											/>
										</div>
									{/if}
								</div>

								<!-- Prune Mode -->
								<div class="space-y-2">
									<SelectWithLabel
										id="dockerPruneMode"
										name="pruneMode"
										bind:value={formPruneMode}
										label={m.docker_prune_action_label()}
										description={pruneModeDescription}
										placeholder={m.docker_prune_placeholder()}
										options={pruneModeOptions}
										onValueChange={(v) => (formPruneMode = v as 'all' | 'dangling')}
									/>
								</div>

								<!-- Default Shell -->
								<div class="space-y-2">
									<SelectWithLabel
										id="shellSelectValue"
										name="shellSelectValue"
										bind:value={shellSelectValue}
										label={m.docker_default_shell_label()}
										description={m.docker_default_shell_description()}
										placeholder={m.docker_default_shell_placeholder()}
										options={[
											...shellOptions,
											{ value: 'custom', label: m.custom(), description: m.docker_shell_custom_description() }
										]}
									/>

									{#if shellSelectValue === 'custom'}
										<div class="pt-2">
											<TextInputWithLabel
												bind:value={formDefaultShell}
												label={m.custom()}
												placeholder={m.docker_shell_custom_path_placeholder()}
												helpText={m.docker_shell_custom_path_help()}
												type="text"
											/>
										</div>
									{/if}
								</div>

								<div class="space-y-4 rounded-lg border p-4">
									<div class="flex items-center justify-between">
										<div class="space-y-0.5">
											<Label for="auto-inject-env" class="text-sm font-medium">{m.docker_auto_inject_env_label()}</Label>
											<div class="text-muted-foreground text-xs">{m.docker_auto_inject_env_description()}</div>
										</div>
										<Switch id="auto-inject-env" bind:checked={formAutoInjectEnv} />
									</div>
								</div>
							</div>
						</Tabs.Content>
					</Tabs.Root>
				</Card.Content>
			</Card.Root>
		{/if}

		{#if environment.id !== '0'}
			<Card.Root class="flex flex-col">
				<Card.Header icon={ApiKeyIcon}>
					<div class="flex flex-col space-y-1.5">
						<Card.Title>
							<h2>{m.environments_agent_config_title()}</h2>
						</Card.Title>
						<Card.Description>{m.environments_agent_config_description()}</Card.Description>
					</div>
				</Card.Header>
				<Card.Content class="space-y-4 p-4">
					{#if regeneratedApiKey}
						<div class="space-y-4">
							<div class="space-y-2">
								<div class="text-sm font-medium">{m.environments_new_api_key()}</div>
								<div class="flex items-center gap-2">
									<code class="bg-muted flex-1 rounded-md px-3 py-2 font-mono text-sm break-all">
										{regeneratedApiKey}
									</code>
									<CopyButton text={regeneratedApiKey} size="icon" class="size-7" />
								</div>
								<p class="text-muted-foreground text-xs">{m.environments_api_key_save_warning()}</p>
							</div>
							<ArcaneButton
								action="base"
								tone="outline"
								onclick={() => (regeneratedApiKey = null)}
								customLabel={m.common_dismiss()}
								class="w-full"
							/>
						</div>
					{:else}
						<div class="rounded-lg border border-amber-500/30 bg-amber-500/10 p-4 text-sm text-amber-900 dark:text-amber-200">
							<p class="font-medium">{m.environments_regenerate_warning_title()}</p>
							<p class="mt-1">{m.environments_regenerate_warning_message()}</p>
						</div>
						<ArcaneButton
							action="remove"
							onclick={() => (showRegenerateDialog = true)}
							disabled={isRegeneratingKey}
							loading={isRegeneratingKey}
							icon={ResetIcon}
							customLabel={m.environments_regenerate_api_key()}
							class="w-full"
						/>
					{/if}
				</Card.Content>
			</Card.Root>
		{/if}
	</div>

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
</div>

<MobileFloatingFormActions {hasChanges} isLoading={isSaving} onSave={handleSave} onReset={handleReset} />
