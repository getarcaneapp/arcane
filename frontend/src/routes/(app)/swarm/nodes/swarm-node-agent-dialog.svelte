<script lang="ts">
	import * as Alert from '$lib/components/ui/alert';
	import { ArcaneButton } from '$lib/components/arcane-button';
	import * as ResponsiveDialog from '$lib/components/ui/responsive-dialog';
	import { Snippet } from '$lib/components/ui/snippet';
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
	title={node ? m.swarm_node_agent_dialog_title({ name: node.hostname }) : m.swarm_node_agent_deploy()}
	description={m.swarm_node_agent_dialog_description()}
	contentClass="sm:max-w-3xl"
>
	{#snippet children()}
		<div class="space-y-5 py-6">
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
						<Snippet text={deployment.dockerRun} />
					</div>

					<div class="space-y-2">
						<div class="text-sm font-medium">{m.environments_docker_compose()}</div>
						<Snippet text={deployment.dockerCompose} />
					</div>
				</div>
			{/if}
		</div>
	{/snippet}

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
