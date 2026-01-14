<script lang="ts">
	import { z } from 'zod/v4';
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
	import { createSettingsForm } from '$lib/utils/settings-form.util';
	import {
		ArrowLeftIcon,
		EnvironmentsIcon,
		AlertIcon,
		TestIcon,
		RefreshIcon,
		ResetIcon,
		ApiKeyIcon,
		DockerBrandIcon,
		SettingsIcon,
		GitBranchIcon,
		ArrowRightIcon,
		JobsIcon
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
		},
		{
			value: 'maintenance',
			label: m.maintenance_title(),
			icon: JobsIcon
		}
	];

	let isRefreshing = $state(false);
	let isTestingConnection = $state(false);
	let isSyncing = $state(false);
	let isRegeneratingKey = $state(false);
	let showRegenerateDialog = $state(false);
	let regeneratedApiKey = $state<string | null>(null);

	// Version state
	let remoteVersion = $state<AppVersionInformation | null>(null);
	let isLoadingVersion = $state(false);

	// Track current status separately from environment data
	let currentStatus = $state<'online' | 'offline' | 'error' | 'pending'>('offline');

	// Initialize status from environment
	$effect(() => {
		currentStatus = environment.status;
	});

	// Form schema combining environment info and settings
	const formSchema = z.object({
		// Environment basic info
		name: z.string().min(1),
		enabled: z.boolean(),
		apiUrl: z.string(),
		// Settings
		pollingEnabled: z.boolean(),
		pollingInterval: z.coerce.number(),
		autoUpdate: z.boolean(),
		autoUpdateInterval: z.coerce.number(),
		autoInjectEnv: z.boolean(),
		dockerPruneMode: z.enum(['all', 'dangling']),
		defaultShell: z.string(),
		projectsDirectory: z.string(),
		diskUsagePath: z.string(),
		maxImageUploadSize: z.coerce.number(),
		baseServerUrl: z.string(),
		scheduledPruneEnabled: z.boolean(),
		scheduledPruneInterval: z.coerce.number().min(60).max(10080),
		scheduledPruneContainers: z.boolean(),
		scheduledPruneImages: z.boolean(),
		scheduledPruneVolumes: z.boolean(),
		scheduledPruneNetworks: z.boolean(),
		scheduledPruneBuildCache: z.boolean()
	});

	// Build current settings object from environment and settings data
	const currentSettings = $derived({
		name: environment.name,
		enabled: environment.enabled,
		apiUrl: environment.apiUrl,
		pollingEnabled: settings?.pollingEnabled ?? false,
		pollingInterval: settings?.pollingInterval ?? 60,
		autoUpdate: settings?.autoUpdate ?? false,
		autoUpdateInterval: settings?.autoUpdateInterval ?? 1440,
		autoInjectEnv: settings?.autoInjectEnv ?? false,
		dockerPruneMode: (settings?.dockerPruneMode as 'all' | 'dangling') || 'dangling',
		defaultShell: settings?.defaultShell || '/bin/sh',
		projectsDirectory: settings?.projectsDirectory || '/app/data/projects',
		diskUsagePath: settings?.diskUsagePath || '/app/data/projects',
		maxImageUploadSize: settings?.maxImageUploadSize || 500,
		baseServerUrl: settings?.baseServerUrl || 'http://localhost',
		scheduledPruneEnabled: settings?.scheduledPruneEnabled ?? false,
		scheduledPruneInterval: settings?.scheduledPruneInterval ?? 1440,
		scheduledPruneContainers: settings?.scheduledPruneContainers ?? true,
		scheduledPruneImages: settings?.scheduledPruneImages ?? true,
		scheduledPruneVolumes: settings?.scheduledPruneVolumes ?? false,
		scheduledPruneNetworks: settings?.scheduledPruneNetworks ?? true,
		scheduledPruneBuildCache: settings?.scheduledPruneBuildCache ?? false
	});

	// Custom save handler for environment-specific settings
	async function saveEnvironmentSettings(formData: z.infer<typeof formSchema>) {
		const sanitizedScheduledPruneInterval = Math.min(Math.max(formData.scheduledPruneInterval || 0, 60), 10080);

		// Update environment basic info
		await environmentManagementService.update(environment.id, {
			name: formData.name,
			enabled: formData.enabled,
			apiUrl: formData.apiUrl
		});

		// Update environment settings if they exist
		if (settings) {
			await settingsService.updateSettingsForEnvironment(environment.id, {
				pollingEnabled: formData.pollingEnabled,
				pollingInterval: formData.pollingInterval,
				autoUpdate: formData.autoUpdate,
				autoUpdateInterval: formData.autoUpdateInterval,
				autoInjectEnv: formData.autoInjectEnv,
				dockerPruneMode: formData.dockerPruneMode,
				defaultShell: formData.defaultShell,
				projectsDirectory: formData.projectsDirectory,
				diskUsagePath: formData.diskUsagePath,
				maxImageUploadSize: formData.maxImageUploadSize,
				baseServerUrl: formData.baseServerUrl,
				scheduledPruneEnabled: formData.scheduledPruneEnabled,
				scheduledPruneInterval: sanitizedScheduledPruneInterval,
				scheduledPruneContainers: formData.scheduledPruneContainers,
				scheduledPruneImages: formData.scheduledPruneImages,
				scheduledPruneVolumes: formData.scheduledPruneVolumes,
				scheduledPruneNetworks: formData.scheduledPruneNetworks,
				scheduledPruneBuildCache: formData.scheduledPruneBuildCache
			});
		}

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
	}

	let { formInputs, settingsForm, resetForm, onSubmit } = $derived(
		createSettingsForm({
			schema: formSchema,
			currentSettings,
			getCurrentSettings: () => currentSettings,
			onSave: saveEnvironmentSettings,
			successMessage: m.common_update_success({ resource: m.resource_environment_cap() }),
			errorMessage: m.common_update_failed({ resource: m.resource_environment() }),
			onReset: () => toast.info(m.environments_changes_reset())
		})
	);

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

	let pollingIntervalMode = $derived.by((): PollingIntervalMode => {
		if (!settings) return 'custom';
		return imagePollingOptions.find((o) => o.minutes === settings.pollingInterval)?.value ?? 'custom';
	});

	const pruneModeOptions = [
		{ value: 'all', label: m.docker_prune_all(), description: m.docker_prune_all_description() },
		{ value: 'dangling', label: m.docker_prune_dangling(), description: m.docker_prune_dangling_description() }
	];

	let pruneModeDescription = $derived(
		pruneModeOptions.find((o) => o.value === $formInputs.dockerPruneMode.value)?.description ?? m.docker_prune_mode_description()
	);

	const shellOptions = [
		{ value: '/bin/sh', label: '/bin/sh', description: m.docker_shell_sh_description() },
		{ value: '/bin/bash', label: '/bin/bash', description: m.docker_shell_bash_description() },
		{ value: '/bin/ash', label: '/bin/ash', description: m.docker_shell_ash_description() },
		{ value: '/bin/zsh', label: '/bin/zsh', description: m.docker_shell_zsh_description() }
	];

	let shellSelectValue = $derived.by((): string => {
		if (!settings) return 'custom';
		return shellOptions.find((o) => o.value === settings.defaultShell)?.value ?? 'custom';
	});

	function handlePollingIntervalModeChange(value: string) {
		if (value !== 'custom') {
			$formInputs.pollingInterval.value = presetToMinutes[value as Exclude<PollingIntervalMode, 'custom'>];
		}
	}

	function handleShellSelectChange(value: string) {
		if (value !== 'custom') {
			$formInputs.defaultShell.value = value;
		}
	}

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

	async function refreshEnvironment() {
		if (isRefreshing) return;
		try {
			isRefreshing = true;
			await invalidateAll();
			currentStatus = environment.status;
			// Reset version to trigger re-fetch if online
			remoteVersion = null;
		} catch (err) {
			console.error('Failed to refresh environment:', err);
			toast.error(m.common_refresh_failed({ resource: m.resource_environment() }));
		} finally {
			isRefreshing = false;
		}
	}

	async function syncEnvironment() {
		if (isSyncing) return;
		try {
			isSyncing = true;
			await environmentManagementService.sync(environment.id);
			toast.success(m.sync_environment_success());
		} catch (error) {
			console.error('Failed to sync environment:', error);
			toast.error(m.sync_environment_failed());
		} finally {
			isSyncing = false;
		}
	}

	async function testConnection() {
		if (isTestingConnection) return;
		try {
			isTestingConnection = true;
			const customUrl = $formInputs.apiUrl.value !== environment.apiUrl ? $formInputs.apiUrl.value : undefined;
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
					{#if settingsForm.hasChanges}
						<span class="text-xs text-orange-600 dark:text-orange-400">{m.environments_unsaved_changes()}</span>
					{:else}
						<span class="text-xs text-green-600 dark:text-green-400">{m.environments_all_changes_saved()}</span>
					{/if}

					{#if settingsForm.hasChanges}
						<ArcaneButton
							action="restart"
							tone="outline"
							onclick={resetForm}
							disabled={settingsForm.isLoading}
							customLabel={m.common_reset()}
						/>
					{/if}

					<ArcaneButton
						action="save"
						onclick={onSubmit}
						disabled={!settingsForm.hasChanges || settingsForm.isLoading}
						loading={settingsForm.isLoading}
						customLabel={m.common_save()}
						loadingLabel={m.common_saving()}
					/>
				</div>

				{#if environment.id !== '0'}
					<ArcaneButton
						action="base"
						tone="outline"
						onclick={syncEnvironment}
						disabled={isSyncing}
						loading={isSyncing}
						icon={RefreshIcon}
						customLabel={m.sync_environment()}
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
						bind:value={$formInputs.name.value}
						class="mt-1.5 w-full {$formInputs.name.error ? 'border-destructive' : ''}"
						placeholder={m.environments_name_placeholder()}
					/>
					{#if $formInputs.name.error}
						<p class="text-destructive mt-1 text-[0.8rem] font-medium">{$formInputs.name.error}</p>
					{/if}
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
										bind:value={$formInputs.apiUrl.value}
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
								bind:value={$formInputs.apiUrl.value}
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
								<Switch id="env-enabled" disabled={true} bind:checked={$formInputs.enabled.value} />
							</ArcaneTooltip.Trigger>
							<ArcaneTooltip.Content>
								<p>{m.environments_local_setting_disabled()}</p>
							</ArcaneTooltip.Content>
						</ArcaneTooltip.Root>
					{:else}
						<Switch id="env-enabled" bind:checked={$formInputs.enabled.value} />
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
										bind:value={$formInputs.projectsDirectory.value}
										error={$formInputs.projectsDirectory.error}
										helpText={m.general_projects_directory_help()}
									/>
								</div>
								<div class="space-y-2">
									<TextInputWithLabel
										id="disk-usage-path"
										label={m.disk_usage_settings()}
										bind:value={$formInputs.diskUsagePath.value}
										error={$formInputs.diskUsagePath.error}
										helpText={m.disk_usage_settings_description()}
									/>
								</div>
								<div class="space-y-2">
									<TextInputWithLabel
										id="base-server-url"
										label={m.general_base_url_label()}
										bind:value={$formInputs.baseServerUrl.value}
										error={$formInputs.baseServerUrl.error}
										helpText={m.general_base_url_help()}
									/>
								</div>
								<div class="space-y-2">
									<TextInputWithLabel
										id="max-upload-size"
										type="number"
										label={m.docker_max_upload_size_label()}
										bind:value={$formInputs.maxImageUploadSize.value}
										error={$formInputs.maxImageUploadSize.error}
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
										<Switch id="polling-enabled" bind:checked={$formInputs.pollingEnabled.value} />
									</div>

									{#if $formInputs.pollingEnabled.value}
										<div class="space-y-3 pt-2">
											<SelectWithLabel
												id="pollingIntervalMode"
												name="pollingIntervalMode"
												bind:value={pollingIntervalMode}
												onValueChange={handlePollingIntervalModeChange}
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
													bind:value={$formInputs.pollingInterval.value}
													error={$formInputs.pollingInterval.error}
													label={m.custom_polling_interval()}
													placeholder={m.docker_polling_interval_placeholder()}
													helpText={m.docker_polling_interval_description()}
													type="number"
												/>
											{/if}

											{#if $formInputs.pollingInterval.value < 30}
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
										<Switch
											id="auto-update"
											bind:checked={$formInputs.autoUpdate.value}
											disabled={!$formInputs.pollingEnabled.value}
										/>
									</div>

									{#if $formInputs.autoUpdate.value && $formInputs.pollingEnabled.value}
										<div class="pt-2">
											<TextInputWithLabel
												bind:value={$formInputs.autoUpdateInterval.value}
												error={$formInputs.autoUpdateInterval.error}
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
										bind:value={$formInputs.dockerPruneMode.value}
										label={m.docker_prune_action_label()}
										description={pruneModeDescription}
										placeholder={m.docker_prune_placeholder()}
										options={pruneModeOptions}
										onValueChange={(v) => ($formInputs.dockerPruneMode.value = v as 'all' | 'dangling')}
									/>
								</div>

								<!-- Default Shell -->
								<div class="space-y-2">
									<SelectWithLabel
										id="shellSelectValue"
										name="shellSelectValue"
										bind:value={shellSelectValue}
										onValueChange={handleShellSelectChange}
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
												bind:value={$formInputs.defaultShell.value}
												error={$formInputs.defaultShell.error}
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
										<Switch id="auto-inject-env" bind:checked={$formInputs.autoInjectEnv.value} />
									</div>
								</div>
							</div>
						</Tabs.Content>
						<Tabs.Content value="maintenance" class="space-y-6 p-4">
							<div class="space-y-4 rounded-lg border p-4">
								<div class="flex items-center justify-between">
									<div class="space-y-0.5">
										<Label class="text-sm font-medium">{m.scheduled_prune_title()}</Label>
										<div class="text-muted-foreground text-xs">
											{m.scheduled_prune_description()}
										</div>
									</div>
									<Switch bind:checked={$formInputs.scheduledPruneEnabled.value} />
								</div>

								{#if $formInputs.scheduledPruneEnabled.value}
									<div class="space-y-4 pt-2">
										<TextInputWithLabel
											id="scheduled-prune-interval"
											label={m.scheduled_prune_interval_label()}
											bind:value={$formInputs.scheduledPruneInterval.value}
											error={$formInputs.scheduledPruneInterval.error}
											placeholder="1440"
											helpText={m.scheduled_prune_interval_description()}
											type="number"
										/>

										<div class="grid gap-3 sm:grid-cols-2">
											<div class="flex items-start justify-between rounded-lg border p-3">
												<div class="space-y-0.5">
													<Label class="text-sm font-medium">{m.scheduled_prune_containers_label()}</Label>
													<p class="text-muted-foreground text-xs">{m.scheduled_prune_containers_description()}</p>
												</div>
												<Switch bind:checked={$formInputs.scheduledPruneContainers.value} />
											</div>
											<div class="flex items-start justify-between rounded-lg border p-3">
												<div class="space-y-0.5">
													<Label class="text-sm font-medium">{m.scheduled_prune_images_label()}</Label>
													<p class="text-muted-foreground text-xs">{m.scheduled_prune_images_description()}</p>
												</div>
												<Switch bind:checked={$formInputs.scheduledPruneImages.value} />
											</div>
											<div class="flex items-start justify-between rounded-lg border p-3">
												<div class="space-y-0.5">
													<Label class="text-sm font-medium">{m.scheduled_prune_volumes_label()}</Label>
													<p class="text-muted-foreground text-xs">{m.scheduled_prune_volumes_description()}</p>
												</div>
												<Switch bind:checked={$formInputs.scheduledPruneVolumes.value} />
											</div>
											<div class="flex items-start justify-between rounded-lg border p-3">
												<div class="space-y-0.5">
													<Label class="text-sm font-medium">{m.scheduled_prune_networks_label()}</Label>
													<p class="text-muted-foreground text-xs">{m.scheduled_prune_networks_description()}</p>
												</div>
												<Switch bind:checked={$formInputs.scheduledPruneNetworks.value} />
											</div>
											<div class="flex items-start justify-between rounded-lg border p-3">
												<div class="space-y-0.5">
													<Label class="text-sm font-medium">{m.scheduled_prune_build_cache_label()}</Label>
													<p class="text-muted-foreground text-xs">{m.scheduled_prune_build_cache_description()}</p>
												</div>
												<Switch bind:checked={$formInputs.scheduledPruneBuildCache.value} />
											</div>
										</div>

										{#if $formInputs.scheduledPruneVolumes.value}
											<div
												class="flex items-start gap-3 rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-amber-900 dark:text-amber-200"
											>
												<AlertIcon class="mt-0.5 size-4 shrink-0 text-amber-600 dark:text-amber-400" />
												<div class="space-y-1 text-sm">
													<p class="font-medium">{m.scheduled_prune_volumes_warning()}</p>
												</div>
											</div>
										{/if}
									</div>
								{/if}
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

		<Card.Root class="flex flex-col">
			<Card.Header icon={GitBranchIcon}>
				<div class="flex flex-col space-y-1.5">
					<Card.Title>
						<h2>{m.git_syncs_title()}</h2>
					</Card.Title>
					<Card.Description>{m.git_subtitle()}</Card.Description>
				</div>
			</Card.Header>
			<Card.Content class="p-4">
				<p class="text-muted-foreground mb-4 text-sm">{m.git_environment_card_description()}</p>
				<ArcaneButton
					action="base"
					onclick={() => goto(`/environments/${environment.id}/gitops`)}
					icon={ArrowRightIcon}
					customLabel={m.git_manage_syncs()}
					class="w-full"
				/>
			</Card.Content>
		</Card.Root>
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

<MobileFloatingFormActions
	hasChanges={settingsForm.hasChanges}
	isLoading={settingsForm.isLoading}
	onSave={onSubmit}
	onReset={resetForm}
/>
