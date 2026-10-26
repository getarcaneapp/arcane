<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import * as Empty from '#lib/components/ui/empty/index.js';
	import { useEnvironmentSettings } from '#lib/hooks/use-environment-settings.svelte.js';
	import { ErrorNotFoundIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { environmentStore, LOCAL_DOCKER_ENVIRONMENT_ID } from '#lib/stores/environment.store.svelte.js';

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

	const environment = $derived(environmentStore.available.find((env) => env.id === page.url.searchParams.get('env')));
	const localSettings = useEnvironmentSettings(() => LOCAL_DOCKER_ENVIRONMENT_ID);

	// Check if this is a connection error
	const isConnectionError = $derived.by(() => {
		const lowerMessage = message.toLowerCase();
		const connectionTerms = [
			'cannot connect to the docker daemon',
			'failed to connect to docker',
			'error during connect:',
			'agent is not connected',
			'no active tunnel for environment',
			'tunnel connection closed',
			'proxy request failed'
		];
		return connectionTerms.some((term) => lowerMessage.includes(term));
	});

	const connectionErrorTitle = $derived.by(() => {
		if (environment?.id === LOCAL_DOCKER_ENVIRONMENT_ID) {
			return m.error_connection_local_docker();
		}
		return m.error_connection_remote_environment({ name: environment?.name || 'Unknown' });
	});

	const connectionErrorMessage = $derived.by(() => {
		if (environment?.id === LOCAL_DOCKER_ENVIRONMENT_ID) {
			return m.error_connection_local_docker_desc({
				host: localSettings.current?.values.dockerHost || 'unix:///var/run/docker.sock'
			});
		}
		return m.error_connection_remote_environment_desc({
			name: environment?.name || 'Unknown',
			url: environment?.apiUrl || 'Unknown URL'
		});
	});

	const showConnectionError = $derived(isConnectionError && !!environment);
</script>

<div class="grid h-full min-h-screen place-items-center px-6">
	<Empty.Root>
		<Empty.Header>
			<Empty.Media variant="icon">
				<ErrorNotFoundIcon class="size-20 text-destructive" aria-hidden="true" />
			</Empty.Media>
			<Empty.Title role="heading" aria-level={1}>{showConnectionError ? connectionErrorTitle : title}</Empty.Title>
			<Empty.Description>{showConnectionError ? connectionErrorMessage : message} - {status}</Empty.Description>
		</Empty.Header>
		<Empty.Content>
			<div class="flex flex-col gap-3">
				{#if showButton}
					<ArcaneButton action="base" customLabel={actionLabel} onclick={() => goto(actionHref)} />
				{/if}
			</div>
		</Empty.Content>
	</Empty.Root>
</div>
