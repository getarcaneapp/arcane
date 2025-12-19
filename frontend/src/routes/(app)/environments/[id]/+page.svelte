<script lang="ts">
	import * as Card from '$lib/components/ui/card/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
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
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import { CopyButton } from '$lib/components/ui/copy-button';
	import {
		ArrowLeftIcon,
		RefreshIcon,
		ConnectionIcon,
		SaveIcon,
		EnvironmentsIcon,
		AlertIcon,
		TestIcon,
		RegistryIcon,
		ResetIcon,
		ApiKeyIcon,
		DockerBrandIcon
	} from '$lib/icons';

	let { data } = $props();
	let { environment, settings } = $derived(data);

	let showSwitchDialog = $state(false);

	let currentEnvironment = $derived(environmentStore.selected);

	let isRefreshing = $state(false);
	let isTestingConnection = $state(false);
	let isSaving = $state(false);
	let isSyncingRegistries = $state(false);
	let isRegeneratingKey = $state(false);
	let showRegenerateDialog = $state(false);
	let regeneratedApiKey = $state<string | null>(null);

	// Form state
	let formName = $state('');
	let formEnabled = $state(false);
	let formApiUrl = $state('');

	// Track current status separately from environment data
	let currentStatus = $state<'online' | 'offline' | 'error' | 'pending'>('offline');

	// Initialize form values and status
	$effect(() => {
		formName = environment.name;
		formEnabled = environment.enabled;
		formApiUrl = environment.apiUrl;
		currentStatus = environment.status;
	});

	// Track changes
	let hasChanges = $derived(
		formName !== environment.name ||
			formEnabled !== environment.enabled ||
			(environment.id !== '0' && formApiUrl !== environment.apiUrl)
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

			await environmentManagementService.update(environment.id, {
				name: formName,
				enabled: formEnabled,
				apiUrl: formApiUrl
			});

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

	async function confirmSwitchAndEdit() {
		try {
			await environmentStore.setEnvironment(environment);
			showSwitchDialog = false;
			goto('/settings');
		} catch (error) {
			console.error('Failed to switch environment:', error);
			toast.error(m.common_action_failed());
		}
	}
</script>

<div class="container mx-auto max-w-full space-y-6 overflow-hidden p-2 sm:p-6">
	<div class="space-y-3 sm:space-y-4">
		<Button variant="ghost" onclick={() => goto('/environments')} class="w-fit gap-2">
			<ArrowLeftIcon class="size-4" />
			<span>{m.common_back_to({ resource: m.environments_title() })}</span>
		</Button>

		<div class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
			<div class="flex-1">
				<h1 class="text-xl font-bold wrap-break-word sm:text-2xl">{environment.name}</h1>
				<p class="text-muted-foreground mt-1.5 text-sm wrap-break-word sm:text-base">{m.environments_page_subtitle()}</p>
			</div>

			<div class="flex flex-wrap items-center gap-2">
				{#if hasChanges}
					<span class="text-xs text-orange-600 dark:text-orange-400">{m.environments_unsaved_changes()}</span>
				{:else}
					<span class="text-xs text-green-600 dark:text-green-400">{m.environments_all_changes_saved()}</span>
				{/if}

				{#if hasChanges}
					<Button variant="outline" size="sm" onclick={handleReset} disabled={isSaving}>
						<ResetIcon class="mr-2 size-4" />
						{m.common_reset()}
					</Button>
				{/if}

				<Button size="sm" onclick={handleSave} disabled={!hasChanges || isSaving}>
					{#if isSaving}
						<div class="border-background mr-2 size-4 animate-spin rounded-full border-2 border-t-transparent"></div>
						{m.common_saving()}
					{:else}
						<SaveIcon class="mr-2 size-4" />
						{m.common_save()}
					{/if}
				</Button>

				{#if environment.id !== '0'}
					<Button variant="outline" onclick={syncRegistries} disabled={isSyncingRegistries}>
						{#if isSyncingRegistries}
							<Spinner class="size-4" />
						{:else}
							<RegistryIcon class="size-4" />
						{/if}
						{m.sync_registries()}
					</Button>
				{/if}

				<Button variant="outline" onclick={refreshEnvironment} disabled={isRefreshing}>
					{#if isRefreshing}
						<Spinner class="size-4" />
					{:else}
						<RefreshIcon class="size-4" />
					{/if}
					{m.common_refresh()}
				</Button>
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
						<Label class="text-muted-foreground text-xs font-medium">Status</Label>
						<div class="mt-1">
							<StatusBadge
								text={currentStatus === 'online' ? m.common_online() : m.common_offline()}
								variant={currentStatus === 'online' ? 'green' : 'red'}
							/>
						</div>
					</div>
				</div>
			</Card.Content>
		</Card.Root>

		{#if settings}
			<Card.Root class="flex flex-col">
				<Card.Header icon={DockerBrandIcon}>
					<div class="flex flex-col space-y-1.5">
						<Card.Title>
							<h2>{m.environments_docker_settings_title()}</h2>
						</Card.Title>
						<Card.Description>{m.environments_config_description()}</Card.Description>
					</div>
				</Card.Header>
				<Card.Content class="space-y-4 p-4">
					<div class="grid grid-cols-2 gap-4">
						<div>
							<Label class="text-muted-foreground text-xs font-medium">{m.docker_enable_polling_label()}</Label>
							<div class="mt-1">
								<StatusBadge
									text={settings.pollingEnabled ? m.common_enabled() : m.common_disabled()}
									variant={settings.pollingEnabled ? 'green' : 'gray'}
								/>
							</div>
						</div>
						{#if settings.pollingEnabled}
							<div>
								<Label class="text-muted-foreground text-xs font-medium">{m.docker_polling_interval_label()}</Label>
								<div class="mt-1 text-sm">{settings.pollingInterval} min</div>
							</div>
						{/if}
						<div>
							<Label class="text-muted-foreground text-xs font-medium">{m.docker_auto_update_label()}</Label>
							<div class="mt-1">
								<StatusBadge
									text={settings.autoUpdate ? m.common_enabled() : m.common_disabled()}
									variant={settings.autoUpdate ? 'green' : 'gray'}
								/>
							</div>
						</div>
						{#if settings.autoUpdate}
							<div>
								<Label class="text-muted-foreground text-xs font-medium">{m.docker_auto_update_interval_label()}</Label>
								<div class="mt-1 text-sm">{settings.autoUpdateInterval} min</div>
							</div>
						{/if}
						<div>
							<Label class="text-muted-foreground text-xs font-medium">{m.docker_prune_action_label()}</Label>
							<div class="mt-1 text-sm capitalize">{settings.dockerPruneMode || 'dangling'}</div>
						</div>
						<div>
							<Label class="text-muted-foreground text-xs font-medium">{m.docker_default_shell_label()}</Label>
							<div class="mt-1 font-mono text-sm">{settings.defaultShell || '/bin/sh'}</div>
						</div>
					</div>
				</Card.Content>
			</Card.Root>
		{/if}

		<Card.Root class="flex flex-col">
			<Card.Header icon={ConnectionIcon}>
				<div class="flex flex-col space-y-1.5">
					<Card.Title>
						<h2>{m.environments_connection_title()}</h2>
					</Card.Title>
					<Card.Description>{m.environments_connection_description()}</Card.Description>
				</div>
			</Card.Header>
			<Card.Content class="space-y-4 p-4">
				<div>
					<Label for="api-url" class="text-sm font-medium">{m.environments_api_url()}</Label>
					{#if environment.id === '0'}
						<ArcaneTooltip.Root>
							<ArcaneTooltip.Trigger class="w-full">
								<Input
									id="api-url"
									type="url"
									bind:value={formApiUrl}
									class="mt-1.5 w-full font-mono"
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
							class="mt-1.5 w-full font-mono"
							placeholder={m.environments_api_url_placeholder()}
							required
						/>
					{/if}
					<p class="text-muted-foreground mt-1.5 text-xs">{m.environments_api_url_help()}</p>
				</div>

				<Button onclick={testConnection} disabled={isTestingConnection} class="w-full">
					{#if isTestingConnection}
						<Spinner />
						{m.environments_testing_connection()}
					{:else}
						<TestIcon class="mr-2 size-4" />
						{m.environments_test_connection()}
					{/if}
				</Button>
			</Card.Content>
		</Card.Root>

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
							<Button variant="outline" onclick={() => (regeneratedApiKey = null)} class="w-full">
								{m.common_dismiss()}
							</Button>
						</div>
					{:else}
						<div class="rounded-lg border border-amber-500/30 bg-amber-500/10 p-4 text-sm text-amber-900 dark:text-amber-200">
							<p class="font-medium">{m.environments_regenerate_warning_title()}</p>
							<p class="mt-1">{m.environments_regenerate_warning_message()}</p>
						</div>
						<Button
							variant="destructive"
							onclick={() => (showRegenerateDialog = true)}
							disabled={isRegeneratingKey}
							class="w-full"
						>
							{#if isRegeneratingKey}
								<Spinner class="size-4" />
							{:else}
								<ResetIcon class="size-4" />
							{/if}
							{m.environments_regenerate_api_key()}
						</Button>
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

	<AlertDialog.Root bind:open={showSwitchDialog}>
		<AlertDialog.Content>
			<AlertDialog.Header>
				<AlertDialog.Title>{m.environments_switch_to_edit_title()}</AlertDialog.Title>
				<AlertDialog.Description>
					{m.environments_switch_to_edit_message()}
				</AlertDialog.Description>
			</AlertDialog.Header>
			<AlertDialog.Footer>
				<AlertDialog.Cancel>{m.common_cancel()}</AlertDialog.Cancel>
				<AlertDialog.Action onclick={confirmSwitchAndEdit}>
					{m.environments_switch_and_edit()}
				</AlertDialog.Action>
			</AlertDialog.Footer>
		</AlertDialog.Content>
	</AlertDialog.Root>
</div>
