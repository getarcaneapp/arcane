<script lang="ts">
	import * as Empty from '$lib/components/ui/empty/index.js';
	import { m } from '$lib/paraglide/messages';
	import { ErrorNotFoundIcon } from '$lib/icons';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { goto } from '$app/navigation';
	import EnvironmentSwitcherDialog from '$lib/components/dialogs/environment-switcher-dialog.svelte';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import settingsStore from '$lib/stores/config-store';

	let {
		message,
		status,
		title = m.error_generic(),
		showButton = true,
		actionHref = '/dashboard',
		actionLabel = m.error_go_to_dashboard()
	}: {
		message: string;
		status?: number;
		title?: string;
		showButton?: boolean;
		actionHref?: string;
		actionLabel?: string;
	} = $props();

	let envSwitcherOpen = $state(false);

	// Check if this is a connection error
	const isConnectionError = $derived.by(() => {
		const lowerMessage = message.toLowerCase();
		return (
			lowerMessage.includes('connection') ||
			lowerMessage.includes('proxy') ||
			lowerMessage.includes('reset') ||
			lowerMessage.includes('timeout') ||
			lowerMessage.includes('network') ||
			lowerMessage.includes('tcp') ||
			lowerMessage.includes('dial') ||
			lowerMessage.includes('lookup') ||
			lowerMessage.includes('host') ||
			lowerMessage.includes('refused') ||
			lowerMessage.includes('internal error') ||
			!status ||
			status === 500 ||
			status === 502 ||
			status === 503 ||
			status === 504
		);
	});

	const connectionErrorTitle = $derived.by(() => {
		if (environmentStore.selected?.id === '0') {
			return m.error_connection_local_docker();
		} else {
			return m.error_connection_remote_environment({ name: environmentStore.selected?.name || 'Unknown' });
		}
	});

	const connectionErrorMessage = $derived.by(() => {
		if (environmentStore.selected?.id === '0') {
			const host = $settingsStore ? $settingsStore.dockerHost : 'unix:///var/run/docker.sock';
			return m.error_connection_local_docker_desc({
				host: host || 'unix:///var/run/docker.sock'
			});
		} else {
			return m.error_connection_remote_environment_desc({
				name: environmentStore.selected?.name || 'Unknown',
				url: environmentStore.selected?.apiUrl || 'Unknown URL'
			});
		}
	});
</script>

<div class="grid h-full min-h-screen place-items-center px-6">
	<Empty.Root>
		<Empty.Header>
			<Empty.Media variant="icon">
				<ErrorNotFoundIcon class="text-destructive size-20" aria-hidden="true" />
			</Empty.Media>
			<Empty.Title>{isConnectionError ? connectionErrorTitle : title}</Empty.Title>
			<Empty.Description>{isConnectionError ? connectionErrorMessage : message} - {status}</Empty.Description>
		</Empty.Header>
		<Empty.Content>
			<div class="flex flex-col gap-3">
				{#if status !== 404}
					<ArcaneButton
						action="base"
						tone="outline"
						customLabel={m.sidebar_select_environment()}
						onclick={() => (envSwitcherOpen = true)}
					/>
				{/if}
				{#if showButton}
					<ArcaneButton action="base" customLabel={actionLabel} onclick={() => goto(actionHref)} />
				{/if}
			</div>
		</Empty.Content>
	</Empty.Root>
</div>

<EnvironmentSwitcherDialog bind:open={envSwitcherOpen} />
