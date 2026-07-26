<script lang="ts">
	import { page } from '$app/state';
	import { createQuery } from '@tanstack/svelte-query';
	import { ResourcePageLayout } from '#lib/layouts/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Alert from '#lib/components/ui/alert';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import Terminal from '#lib/components/terminal/terminal.svelte';
	import TerminalControls from '#lib/components/terminal/terminal-controls.svelte';
	import SearchableSelect from '#lib/components/form/searchable-select.svelte';
	import { m } from '#lib/paraglide/messages';
	import { environmentStore } from '#lib/stores/environment.store.svelte';
	import { containerService } from '#lib/services/container-service';
	import { settingsService } from '#lib/services/settings-service';
	import { queryKeys } from '#lib/query/query-keys';
	import { hasPermission } from '#lib/utils/auth';
	import { AlertIcon, BoxIcon, MonitorIcon } from '#lib/icons';
	import settingsStore from '#lib/stores/config-store';

	const envId = $derived(environmentStore.selected?.id || '0');

	let target = $state<'container' | 'host'>('container');
	let selectedContainerId = $state('');
	let selectedShell = $state($settingsStore.defaultShell || '/bin/sh');
	let reconnectKey = $state(0);
	let isConnected = $state(false);

	// A container-detail Shell tab can deep-link here with the container
	// preselected instead of duplicating the terminal UI on that page.
	const initialTargetParam = page.url.searchParams.get('target');
	if (initialTargetParam?.startsWith('container:')) {
		target = 'container';
		selectedContainerId = initialTargetParam.slice('container:'.length);
	} else if (initialTargetParam === 'host') {
		target = 'host';
	}

	const containerListOptions = { pagination: { page: 1, limit: 200 } };

	const containersQuery = createQuery(() => {
		const queryEnvId = envId;
		return {
			queryKey: queryKeys.containers.list(queryEnvId, containerListOptions),
			queryFn: () => containerService.getContainersForEnvironment(queryEnvId, containerListOptions)
		};
	});

	const settingsQuery = createQuery(() => {
		const queryEnvId = envId;
		return {
			queryKey: queryKeys.settings.byEnvironment(queryEnvId),
			queryFn: () => settingsService.getSettingsForEnvironmentMerged(queryEnvId)
		};
	});

	const runningContainers = $derived((containersQuery.data?.data ?? []).filter((c) => c.state === 'running'));
	const containerItems = $derived(
		runningContainers.map((c) => ({
			value: c.id,
			label: c.names[0]?.replace(/^\//, '') || c.id.slice(0, 12)
		}))
	);

	const hostTerminalEnabled = $derived(settingsQuery.data?.hostTerminalEnabled ?? false);
	const canUseHostTerminal = $derived(hasPermission('system:host-terminal', envId) && hostTerminalEnabled);
	const hasHostTerminalPermissionOnly = $derived(hasPermission('system:host-terminal', envId) && !hostTerminalEnabled);

	// Once containers load, default the picker to the first running one if
	// nothing was preselected via ?target= and nothing has been picked yet.
	$effect(() => {
		const first = containerItems[0];
		if (!selectedContainerId && first) {
			selectedContainerId = first.value;
		}
	});

	function websocketUrlFor(kind: 'container' | 'host', containerId: string, shell: string): string {
		const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
		const host = window.location.host;
		const base = `${protocol}//${host}/api/environments/${envId}/ws`;
		if (kind === 'host') {
			return `${base}/system/terminal?shell=${encodeURIComponent(shell)}`;
		}
		return `${base}/containers/${containerId}/terminal?shell=${encodeURIComponent(shell)}`;
	}

	const websocketUrl = $derived(
		target === 'host'
			? canUseHostTerminal
				? websocketUrlFor('host', '', selectedShell)
				: ''
			: selectedContainerId
				? websocketUrlFor('container', selectedContainerId, selectedShell)
				: ''
	);

	function handleShellChange(shell: string) {
		selectedShell = shell;
	}

	function handleReconnect() {
		reconnectKey += 1;
		isConnected = false;
	}
</script>

<ResourcePageLayout title={m.terminal_title()} subtitle={m.terminal_subtitle()}>
	{#snippet mainContent()}
		<Card.Root>
			<Card.Header icon={target === 'host' ? MonitorIcon : BoxIcon}>
				<div class="flex flex-1 flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
					<div class="flex flex-col gap-3">
						<Tabs.Root
							value={target}
							onValueChange={(value) => {
								if (value === 'container' || value === 'host') {
									target = value;
									handleReconnect();
								}
							}}
						>
							<Tabs.List>
								<Tabs.Trigger value="container">
									<BoxIcon class="mr-1.5 size-4" />
									{m.terminal_target_containers()}
								</Tabs.Trigger>
								<Tabs.Trigger value="host" disabled={!canUseHostTerminal}>
									<MonitorIcon class="mr-1.5 size-4" />
									{m.terminal_target_host()}
								</Tabs.Trigger>
							</Tabs.List>
						</Tabs.Root>

						{#if target === 'container'}
							<SearchableSelect
								items={containerItems}
								bind:value={selectedContainerId}
								selectText={m.terminal_target_containers()}
								onSelect={(value) => {
									selectedContainerId = value;
									handleReconnect();
								}}
							/>
						{:else if !canUseHostTerminal}
							<p class="text-xs text-muted-foreground">
								{hasHostTerminalPermissionOnly ? m.terminal_host_disabled_hint() : m.terminal_host_no_permission()}
							</p>
						{/if}

						{#if isConnected}
							<div class="flex items-center gap-2">
								<div class="size-2 animate-pulse rounded-full bg-green-500"></div>
								<span class="text-xs font-semibold text-green-600 sm:text-sm">{m.common_live()}</span>
							</div>
						{/if}
					</div>
					<TerminalControls bind:selectedShell onShellChange={handleShellChange} onReconnect={handleReconnect} />
				</div>
			</Card.Header>
			<Card.Content class="overflow-hidden p-2">
				{#if target === 'host'}
					<div class="mb-2">
						<Alert.Root variant="destructive" class="py-2 [&>svg]:top-2">
							<AlertIcon class="size-4" />
							<Alert.Description class="text-xs">
								{m.terminal_host_root_warning()}
							</Alert.Description>
						</Alert.Root>
					</div>
				{/if}
				<div class="h-full overflow-hidden rounded-lg border">
					{#if websocketUrl}
						{#key `${target}-${selectedContainerId}-${reconnectKey}`}
							<Terminal
								{websocketUrl}
								height="calc(100vh - 380px)"
								protocol="v1"
								onConnected={() => (isConnected = true)}
								onDisconnected={() => (isConnected = false)}
							/>
						{/key}
					{/if}
				</div>
			</Card.Content>
		</Card.Root>
	{/snippet}
</ResourcePageLayout>
