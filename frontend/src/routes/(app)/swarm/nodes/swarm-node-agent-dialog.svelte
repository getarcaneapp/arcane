<script lang="ts">
	import * as Alert from '$lib/components/ui/alert';
	import { ArcaneButton } from '$lib/components/arcane-button';
	import AgentCommandBlock from '$lib/components/agent-command-block.svelte';
	import * as ResponsiveDialog from '$lib/components/ui/responsive-dialog';
	import { Spinner } from '$lib/components/ui/spinner';
	import { Badge } from '$lib/components/ui/badge';
	import { AlertTriangleIcon, EdgeConnectionIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import type { SwarmNodeAgentDeployment, SwarmNodeSummary } from '$lib/types/swarm';
	import { getSwarmNodeAgentLabel, getSwarmNodeAgentVariant } from './agent-status';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { hasAnyPermission } from '$lib/utils/auth';
	import { goto } from '$app/navigation';

	type SwarmNodeAgentDialogProps = {
		open: boolean;
		node: SwarmNodeSummary | null;
		deployment: SwarmNodeAgentDeployment | null;
		errorMessage?: string;
		isLoading?: boolean;
		onRefresh?: () => void | Promise<void>;
		onRegenerate?: () => void | Promise<void>;
		onProvision?: () => void | Promise<void>;
		onAttach?: (environmentId: string) => void | Promise<void>;
		onDetach?: () => void | Promise<void>;
		onRemoveDeployment?: () => void | Promise<void>;
	};

	let {
		open = $bindable(false),
		node = null,
		deployment = null,
		errorMessage = '',
		isLoading = false,
		onRefresh,
		onRegenerate,
		onProvision,
		onAttach,
		onDetach,
		onRemoveDeployment
	}: SwarmNodeAgentDialogProps = $props();

	const agentStatus = $derived(deployment?.agent ?? node?.agent ?? { state: 'none' as const });
	const agentStatusLabel = $derived(getSwarmNodeAgentLabel(agentStatus.state));
	const isReady = $derived(!!deployment && !isLoading);
	const isVisibleEnvironment = $derived(agentStatus.bindingKind === 'environment' && !!agentStatus.environmentId);
	const isDedicated = $derived(agentStatus.bindingKind === 'dedicated');
	const canCreateEnvironment = $derived(!agentStatus.bindingKind && !agentStatus.candidates?.length);
	const canShowDeployment = $derived(isVisibleEnvironment || isDedicated);
	const bindingLabel = $derived.by(() => {
		switch (agentStatus.bindingKind) {
			case 'local':
				return m.swarm_node_agent_binding_local();
			case 'environment':
				return m.swarm_node_agent_binding_environment();
			case 'dedicated':
				return m.swarm_node_agent_binding_dedicated();
			default:
				return m.swarm_node_agent_binding_none();
		}
	});

	async function navigateToResource(path: string, permissions: string[]) {
		const environmentId = agentStatus.environmentId;
		if (!environmentId || !hasAnyPermission(permissions, environmentId)) return;
		const environment = environmentStore.available.find((candidate) => candidate.id === environmentId);
		if (!environment) return;
		await environmentStore.setEnvironment(environment);
		await goto(path);
		open = false;
	}
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

		<div class="grid gap-3 sm:grid-cols-3">
			<div class="rounded-lg border bg-muted/40 p-4">
				<div class="text-xs font-medium tracking-wide text-muted-foreground uppercase">
					{m.swarm_node_agent_binding_kind()}
				</div>
				<div class="mt-2 text-sm font-medium">{bindingLabel}</div>
			</div>

			<div class="rounded-lg border bg-muted/40 p-4">
				<div class="text-xs font-medium tracking-wide text-muted-foreground uppercase">{m.common_status()}</div>
				<div class="mt-2 flex items-center gap-2">
					<Badge variant={getSwarmNodeAgentVariant(agentStatus.state)} minWidth="20">{agentStatusLabel}</Badge>
				</div>
			</div>

			<div class="rounded-lg border bg-muted/40 p-4">
				<div class="text-xs font-medium tracking-wide text-muted-foreground uppercase">
					{m.swarm_node_agent_environment_id()}
				</div>
				<div class="mt-2 font-mono text-sm break-all">
					{agentStatus.environmentName ?? deployment?.environmentId ?? agentStatus.environmentId ?? m.common_na()}
				</div>
			</div>
		</div>

		{#if agentStatus.candidates?.length}
			<div class="space-y-3 rounded-lg border p-4">
				<div>
					<div class="font-medium">{m.swarm_node_agent_candidates_title()}</div>
					<div class="text-sm text-muted-foreground">{m.swarm_node_agent_candidates_description()}</div>
				</div>
				<div class="space-y-2">
					{#each agentStatus.candidates as candidate (candidate.environmentId)}
						<div class="flex items-center justify-between gap-3 rounded-md border bg-muted/30 px-3 py-2">
							<div>
								<div class="text-sm font-medium">{candidate.environmentName}</div>
								<div class="text-xs text-muted-foreground">{candidate.environmentType}</div>
							</div>
							<ArcaneButton
								action="save"
								size="sm"
								customLabel={m.swarm_node_agent_attach_action()}
								onclick={() => onAttach?.(candidate.environmentId)}
								disabled={isLoading}
							/>
						</div>
					{/each}
				</div>
			</div>
		{/if}

		{#if isVisibleEnvironment && agentStatus.environmentId}
			<div class="space-y-3 rounded-lg border p-4">
				<div>
					<div class="font-medium">{m.swarm_node_agent_resources_title()}</div>
					<div class="text-sm text-muted-foreground">{m.swarm_node_agent_resources_description()}</div>
				</div>
				<div class="flex flex-wrap gap-2">
					{#if hasAnyPermission(['containers:list', 'containers:read'], agentStatus.environmentId)}
						<ArcaneButton
							action="base"
							tone="outline"
							customLabel={m.containers()}
							onclick={() => navigateToResource('/containers', ['containers:list', 'containers:read'])}
						/>
					{/if}
					{#if hasAnyPermission(['images:list', 'images:read'], agentStatus.environmentId)}
						<ArcaneButton
							action="base"
							tone="outline"
							customLabel={m.images()}
							onclick={() => navigateToResource('/images', ['images:list', 'images:read'])}
						/>
					{/if}
					{#if hasAnyPermission(['volumes:list', 'volumes:read'], agentStatus.environmentId)}
						<ArcaneButton
							action="base"
							tone="outline"
							customLabel={m.resource_volumes_cap()}
							onclick={() => navigateToResource('/volumes', ['volumes:list', 'volumes:read'])}
						/>
					{/if}
					{#if hasAnyPermission(['networks:list', 'networks:read'], agentStatus.environmentId)}
						<ArcaneButton
							action="base"
							tone="outline"
							customLabel={m.resource_networks_cap()}
							onclick={() => navigateToResource('/networks', ['networks:list', 'networks:read'])}
						/>
					{/if}
				</div>
			</div>
		{/if}

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
				<AgentCommandBlock
					dockerRunLabel={m.docker_run_command()}
					dockerRun={deployment.dockerRun}
					dockerComposeLabel={m.common_docker_compose()}
					dockerCompose={deployment.dockerCompose}
					preClass="pr-12"
				/>
			</div>
		{:else if canCreateEnvironment}
			<div class="rounded-lg border border-dashed p-4">
				<div class="font-medium">{m.swarm_node_agent_create_environment_title()}</div>
				<div class="mt-1 text-sm text-muted-foreground">{m.swarm_node_agent_create_environment_description()}</div>
				<ArcaneButton
					class="mt-3"
					action="create"
					customLabel={m.swarm_node_agent_create_environment_action()}
					onclick={onProvision}
					disabled={isLoading}
				/>
			</div>
		{:else if canShowDeployment}
			<div class="rounded-lg border border-dashed p-4">
				<div class="font-medium">
					{isDedicated ? m.swarm_node_agent_legacy_deployment_title() : m.swarm_node_agent_deployment_title()}
				</div>
				<div class="mt-1 text-sm text-muted-foreground">
					{isDedicated ? m.swarm_node_agent_legacy_deployment_description() : m.swarm_node_agent_deployment_description()}
				</div>
				<ArcaneButton
					class="mt-3"
					action="inspect"
					customLabel={m.swarm_node_agent_show_deployment()}
					onclick={onProvision}
					disabled={isLoading}
				/>
			</div>
		{/if}
	</div>

	{#snippet footer()}
		<div class="flex w-full flex-col gap-2 sm:flex-row sm:justify-end">
			{#if deployment}
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
			{/if}
			{#if isVisibleEnvironment}
				<ArcaneButton action="remove" customLabel={m.swarm_node_agent_detach_action()} onclick={onDetach} disabled={isLoading} />
			{/if}
			{#if isDedicated}
				<ArcaneButton
					action="remove"
					customLabel={m.swarm_node_agent_remove_deployment_action()}
					onclick={onRemoveDeployment}
					disabled={isLoading}
				/>
			{/if}
			<ArcaneButton action="base" customLabel={m.common_done()} onclick={() => (open = false)} />
		</div>
	{/snippet}
</ResponsiveDialog.Root>
