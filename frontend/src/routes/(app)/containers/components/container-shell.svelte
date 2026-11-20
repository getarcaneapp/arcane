<script lang="ts">
	import { page } from '$app/state';

	import TerminalControls from '#lib/components/terminal/terminal-controls.svelte';
	import Terminal from '#lib/components/terminal/terminal.svelte';
	import * as Card from '#lib/components/ui/card/index.js';
	import { TerminalIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import settingsStore from '#lib/stores/config-store.svelte.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import userStore from '#lib/stores/user-store.svelte.js';
	import { COMPOSE_PROJECT_LABEL, COMPOSE_SERVICE_LABEL } from '#lib/utils/docker.js';
	import { saveUserPreferences } from '#lib/utils/user-preferences.js';

	let {
		containerId,
		environmentId,
		labels,
		name
	}: {
		containerId: string | undefined;
		environmentId: string;
		labels: Record<string, string> | null | undefined;
		name: string | undefined;
	} = $props();

	const maxSavedChoices = 1000;
	const preferences = $derived(userStore.current?.preferences ?? {});

	function withSavedChoice(saved: Record<string, string> | undefined, key: string, value: string) {
		const others = Object.entries(saved ?? {}).filter(([savedKey]) => savedKey !== key);
		if (!value) return Object.fromEntries(others);
		return Object.fromEntries([...others, [key, value]].slice(-maxSavedChoices));
	}

	// Keyed by compose service (or container name) so the choice survives container recreation.
	const shellKey = $derived.by(() => {
		const envId = environmentId;
		const project = labels?.[COMPOSE_PROJECT_LABEL];
		const service = labels?.[COMPOSE_SERVICE_LABEL];
		if (project && service) return `${envId}:${project}/${service}`;
		if (name) return `${envId}:${name}`;
		return undefined;
	});

	let isConnected = $state(false);
	let selectedShell = $derived(
		(shellKey && preferences.terminalShells?.[shellKey]) ||
			preferences.defaultShell ||
			settingsStore.current?.defaultShell ||
			'/bin/sh'
	);
	let selectedUser = $derived((shellKey && preferences.terminalUsers?.[shellKey]) || '');
	let reconnectKey = $state(0);
	const websocketUrl = $derived.by(() => {
		if (!containerId || !selectedShell) return '';
		let protocol = 'ws:';
		if (page.url.protocol === 'https:') protocol = 'wss:';
		const query = new URLSearchParams({ shell: selectedShell });
		if (selectedUser) query.set('user', selectedUser);
		return `${protocol}//${page.url.host}/api/agents/${environmentId}/ws/containers/${containerId}/terminal?${query}`;
	});

	function handleShellChange(shell: string) {
		selectedShell = shell;
		if (shellKey) void saveUserPreferences({ terminalShells: withSavedChoice(preferences.terminalShells, shellKey, shell) });
	}

	function handleUserChange(user: string) {
		selectedUser = user;
		if (shellKey) void saveUserPreferences({ terminalUsers: withSavedChoice(preferences.terminalUsers, shellKey, user) });
	}

	function handleConnected() {
		isConnected = true;
	}

	function handleDisconnected() {
		isConnected = false;
	}

	function handleReconnect() {
		reconnectKey += 1;
		isConnected = false;
	}
</script>

<Card.Root>
	<Card.Header icon={TerminalIcon}>
		<div class="flex flex-1 flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
			<div class="flex flex-col gap-1.5">
				<div class="flex items-center gap-2">
					<Card.Title>
						<h2>
							{m.common_shell()}
						</h2>
					</Card.Title>
					{#if isConnected}
						<div class="flex items-center gap-2">
							<div class="size-2 animate-pulse rounded-full bg-success"></div>
							<span class="text-xs font-semibold text-success sm:text-sm">{m.common_live()}</span>
						</div>
					{/if}
				</div>
				<Card.Description>{m.shell_interactive_access()}</Card.Description>
			</div>
			<TerminalControls
				bind:selectedShell
				bind:selectedUser
				onShellChange={handleShellChange}
				onUserChange={handleUserChange}
				onReconnect={handleReconnect}
			/>
		</div>
	</Card.Header>
	<div class="overflow-hidden p-2">
		<div class="h-full overflow-hidden rounded-lg border">
			{#await environmentStore.ready then}
				{#if websocketUrl}
					{#key reconnectKey}
						<Terminal
							{websocketUrl}
							height="calc(100vh - 320px)"
							onConnected={handleConnected}
							onDisconnected={handleDisconnected}
						/>
					{/key}
				{/if}
			{/await}
		</div>
	</div>
</Card.Root>
