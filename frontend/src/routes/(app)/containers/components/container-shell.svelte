<script lang="ts">
	import { page } from '$app/state';
	import { PersistedState } from 'runed';

	import TerminalControls from '#lib/components/terminal/terminal-controls.svelte';
	import Terminal from '#lib/components/terminal/terminal.svelte';
	import * as Card from '#lib/components/ui/card/index.js';
	import { TerminalIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import settingsStore from '#lib/stores/config-store.svelte.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import { COMPOSE_PROJECT_LABEL, COMPOSE_SERVICE_LABEL } from '#lib/utils/docker.js';

	let {
		containerId,
		labels,
		name
	}: {
		containerId: string | undefined;
		labels: Record<string, string> | null | undefined;
		name: string | undefined;
	} = $props();

	const savedShells = new PersistedState<Record<string, string>>('arcane_terminal_shells', {});

	// Keyed by compose service (or container name) so the choice survives container recreation.
	const shellKey = $derived.by(() => {
		const envId = environmentStore.selected?.id ?? '0';
		const project = labels?.[COMPOSE_PROJECT_LABEL];
		const service = labels?.[COMPOSE_SERVICE_LABEL];
		if (project && service) return `${envId}:${project}/${service}`;
		if (name) return `${envId}:${name}`;
		return undefined;
	});

	let isConnected = $state(false);
	let selectedShell = $derived((shellKey && savedShells.current[shellKey]) || settingsStore.current?.defaultShell || '/bin/sh');
	let reconnectKey = $state(0);
	const websocketUrl = $derived.by(() => {
		if (!containerId || !selectedShell) return '';
		let protocol = 'ws:';
		if (page.url.protocol === 'https:') protocol = 'wss:';
		return `${protocol}//${page.url.host}/api/environments/${environmentStore.selected?.id ?? '0'}/ws/containers/${containerId}/terminal?shell=${encodeURIComponent(selectedShell)}`;
	});

	function handleShellChange(shell: string) {
		selectedShell = shell;
		if (shellKey) savedShells.current = { ...savedShells.current, [shellKey]: shell };
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
			<TerminalControls bind:selectedShell onShellChange={handleShellChange} onReconnect={handleReconnect} />
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
