<script lang="ts">
	import * as Alert from '$lib/components/ui/alert';
	import { ArcaneButton } from '$lib/components/arcane-button';
	import { CopyButton } from '$lib/components/ui/copy-button';
	import * as ResponsiveDialog from '$lib/components/ui/responsive-dialog';
	import { Spinner } from '$lib/components/ui/spinner';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { AlertTriangleIcon, EdgeConnectionIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import type { SwarmNodeAgentDeployment, SwarmNodeSummary } from '$lib/types/swarm.type';
	import { getSwarmNodeAgentLabel, getSwarmNodeAgentVariant } from './agent-status';

	type SwarmNodeAgentDialogProps = {
		open: boolean;
		node: SwarmNodeSummary | null;
		deployment: SwarmNodeAgentDeployment | null;
		errorMessage?: string;
		isLoading?: boolean;
		onRefresh?: () => void | Promise<void>;
		onRegenerate?: () => void | Promise<void>;
	};

	let {
		open = $bindable(false),
		node = null,
		deployment = null,
		errorMessage = '',
		isLoading = false,
		onRefresh,
		onRegenerate
	}: SwarmNodeAgentDialogProps = $props();

	const agentStatus = $derived(deployment?.agent ?? node?.agent ?? { state: 'none' as const });
	const agentStatusLabel = $derived(getSwarmNodeAgentLabel(agentStatus.state));
	const isReady = $derived(!!deployment && !isLoading);
</script>

<ResponsiveDialog.Root
	bind:open
	variant="sheet"
	title={node ? m.swarm_node_agent_dialog_title({ name: node.hostname }) : m.swarm_node_agent_deploy()}
	description={m.swarm_node_agent_dialog_description()}
	contentClass="sm:max-w-3xl"
>
	<div class="space-y-5 px-6 py-6">
		<Alert.Root class="border-primary/20 bg-primary/5">
			<EdgeConnectionIcon class="size-4" />
			<Alert.Title>{m.swarm_node_agent_dialog_blurb_title()}</Alert.Title>
			<Alert.Description>{m.swarm_node_agent_dialog_blurb_description()}</Alert.Description>
		</Alert.Root>

		<div class="grid gap-3 sm:grid-cols-2">
			<div class="bg-muted/40 rounded-lg border p-4">
				<div class="text-muted-foreground text-xs font-medium tracking-wide uppercase">{m.common_status()}</div>
				<div class="mt-2 flex items-center gap-2">
					<StatusBadge text={agentStatusLabel} variant={getSwarmNodeAgentVariant(agentStatus.state)} />
				</div>
			</div>

			<div class="bg-muted/40 rounded-lg border p-4">
				<div class="text-muted-foreground text-xs font-medium tracking-wide uppercase">
					{m.swarm_node_agent_environment_id()}
				</div>
				<div class="mt-2 font-mono text-sm break-all">{deployment?.environmentId ?? agentStatus.environmentId ?? '—'}</div>
			</div>
		</div>

		{#if errorMessage}
			<Alert.Root variant="destructive">
				<AlertTriangleIcon class="size-4" />
				<Alert.Title>{m.common_action_failed()}</Alert.Title>
				<Alert.Description>{errorMessage}</Alert.Description>
			</Alert.Root>
		{/if}

		{#if isLoading && !deployment}
			<div class="flex items-center justify-center py-10">
				<Spinner class="size-6" />
			</div>
		{:else if isReady && deployment}
			<div class="space-y-4">
				<div class="space-y-2">
					<div class="text-sm font-medium">{m.environments_docker_run_command()}</div>
					<div class="relative">
						<pre class="bg-muted overflow-x-auto rounded-md p-3 pr-12 text-xs"><code>{deployment.dockerRun}</code></pre>
						<div class="absolute top-2 right-2">
							<CopyButton text={deployment.dockerRun} size="icon" class="size-7" />
						</div>
					</div>
				</div>

				<div class="space-y-2">
					<div class="text-sm font-medium">{m.environments_docker_compose()}</div>
					<div class="relative">
						<pre class="bg-muted overflow-x-auto rounded-md p-3 pr-12 text-xs"><code>{deployment.dockerCompose}</code></pre>
						<div class="absolute top-2 right-2">
							<CopyButton text={deployment.dockerCompose} size="icon" class="size-7" />
						</div>
					</div>
				</div>
			</div>
		{/if}
	</div>

	{#snippet footer()}
		<div class="flex w-full flex-col gap-2 sm:flex-row sm:justify-end">
			<ArcaneButton
				action="base"
				tone="outline"
				customLabel={m.environments_regenerate_api_key()}
				onclick={onRegenerate}
				loading={isLoading}
				disabled={!node || isLoading}
			/>
			<ArcaneButton
				action="base"
				customLabel={m.swarm_node_agent_refresh_status()}
				onclick={onRefresh}
				loading={isLoading}
				disabled={!node || isLoading}
			/>
			<ArcaneButton action="base" customLabel={m.common_done()} onclick={() => (open = false)} />
		</div>
	{/snippet}
</ResponsiveDialog.Root>
