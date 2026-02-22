<script lang="ts">
	import * as Card from '$lib/components/ui/card/index.js';
	import Label from '$lib/components/ui/label/label.svelte';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Switch } from '$lib/components/ui/switch/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import * as ArcaneTooltip from '$lib/components/arcane-tooltip';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import { m } from '$lib/paraglide/messages';
	import { EnvironmentsIcon, TestIcon } from '$lib/icons';

	let {
		environment,
		formInputs,
		currentStatus,
		isLoadingVersion,
		remoteVersion,
		versionInformation,
		isTestingConnection,
		testConnection
	} = $props();

	let transportBadge = $derived.by((): { text: string; variant: 'blue' | 'purple' | 'gray' } => {
		if (!environment.isEdge) {
			return { text: 'HTTP', variant: 'gray' };
		}

		if (environment.edgeTransport === 'websocket') {
			return { text: 'WebSocket', variant: 'purple' };
		}

		return { text: 'gRPC', variant: 'blue' };
	});

	let localDisplayVersion = $derived(
		versionInformation?.displayVersion || versionInformation?.currentTag || versionInformation?.currentVersion || 'Unknown'
	);

	let remoteDisplayVersion = $derived(
		remoteVersion?.displayVersion || remoteVersion?.currentTag || remoteVersion?.currentVersion || ''
	);
</script>

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
			<div>
				<Label class="text-muted-foreground text-xs font-medium">{m.common_type()}</Label>
				<div class="mt-1">
					<StatusBadge text={transportBadge.text} variant={transportBadge.variant} />
				</div>
			</div>
			<div class="col-span-2 border-t pt-4">
				<Label class="text-muted-foreground text-xs font-medium">{m.version_info_version()}</Label>
				<div class="mt-1 flex items-center gap-2">
					{#if environment.id === '0'}
						<span class="font-mono text-sm">{localDisplayVersion}</span>
						{#if versionInformation?.updateAvailable}
							<Badge variant="secondary" class="bg-amber-500/10 text-amber-600 hover:bg-amber-500/20 dark:text-amber-400">
								{m.sidebar_update_available()}: {versionInformation.newestVersion}
							</Badge>
						{/if}
					{:else if isLoadingVersion}
						<Spinner />
						<span class="text-muted-foreground text-sm">{m.common_action_checking()}</span>
					{:else if remoteVersion}
						<span class="font-mono text-sm">{remoteDisplayVersion}</span>
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
