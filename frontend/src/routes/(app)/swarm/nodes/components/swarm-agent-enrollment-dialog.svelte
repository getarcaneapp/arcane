<script lang="ts">
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';

	import AgentCommandBlock from '#lib/components/agent-command-block.svelte';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import EnvironmentDestinationSelect from '#lib/components/form/environment-destination-select.svelte';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { ResponsiveDialog } from '#lib/components/ui/responsive-dialog/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { AgentConnectionIcon, AlertTriangleIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { swarmService } from '#lib/services/swarm-service.js';
	import type { EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
	import type { NodeAgentEnrollment, NodeAgentEnrollmentToken } from '#lib/types/swarm.js';
	import { extractApiErrorMessage, handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	let { open = $bindable(false), environments }: { open: boolean; environments: EnvironmentRef[] } = $props();

	const queryClient = useQueryClient();
	let selectedEnvironmentId = $state<string>();
	let generated = $state<{ environmentId: string; result: NodeAgentEnrollmentToken } | null>(null);
	let isMutating = $state(false);
	let opening = 0;

	const environmentId = $derived(environments.find((env) => env.id === selectedEnvironmentId)?.id ?? environments[0]?.id ?? '');
	const enrollmentQuery = createQuery(() => {
		const requestedEnvironmentId = environmentId;
		return {
			queryKey: queryKeys.swarm.agentEnrollment(requestedEnvironmentId),
			queryFn: () => swarmService.getNodeAgentEnrollment(requestedEnvironmentId),
			enabled: open && requestedEnvironmentId !== '',
			retry: false
		};
	});
	const enabled = $derived(enrollmentQuery.data?.enabled);
	const shownStack = $derived(generated?.environmentId === environmentId ? generated.result : null);

	function handleOpenChange(isOpen: boolean) {
		if (!isOpen) {
			opening++;
			generated = null;
		}
	}

	async function generateToken() {
		const targetEnvironmentId = environmentId;
		const requestOpening = opening;
		isMutating = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(swarmService.rotateNodeAgentEnrollment(targetEnvironmentId)),
			message: m.common_action_failed(),
			setLoadingState: (value) => (isMutating = value),
			onSuccess: (result) => {
				queryClient.setQueryData<NodeAgentEnrollment>(queryKeys.swarm.agentEnrollment(targetEnvironmentId), {
					enabled: true
				});
				if (requestOpening === opening) {
					generated = { environmentId: targetEnvironmentId, result };
					toast.success(m.enrollment_token_generated());
				}
			}
		});
	}

	function rotateToken() {
		if (!enabled) {
			void generateToken();
			return;
		}
		openConfirmDialog({
			title: m.rotate_enrollment_token_title(),
			message: m.rotate_enrollment_token_description(),
			confirm: { label: m.rotate_token(), destructive: true, action: generateToken }
		});
	}

	function revokeToken() {
		const targetEnvironmentId = environmentId;
		openConfirmDialog({
			title: m.revoke_enrollment_token_title(),
			message: m.revoke_enrollment_token_description(),
			confirm: {
				label: m.revoke_token(),
				destructive: true,
				action: async () => {
					isMutating = true;
					await handleApiResultWithCallbacks({
						result: await tryCatch(swarmService.revokeNodeAgentEnrollment(targetEnvironmentId)),
						message: m.common_action_failed(),
						setLoadingState: (value) => (isMutating = value),
						onSuccess: () => {
							queryClient.setQueryData<NodeAgentEnrollment>(queryKeys.swarm.agentEnrollment(targetEnvironmentId), {
								enabled: false
							});
							if (generated?.environmentId === targetEnvironmentId) generated = null;
							toast.success(m.enrollment_token_revoked());
						}
					});
				}
			}
		});
	}
</script>

<ResponsiveDialog
	bind:open
	onOpenChange={handleOpenChange}
	variant="sheet"
	title={m.deploy_agents_to_every_node()}
	description={m.agent_enrollment_description()}
	contentClass="sm:max-w-3xl"
>
	{#snippet children()}
		<div class="space-y-5 px-6 py-6">
			<EnvironmentDestinationSelect bind:value={selectedEnvironmentId} {environments} variant="field" />

			{#if enrollmentQuery.isError}
				<Alert.Root variant="destructive">
					<AlertTriangleIcon class="size-4" />
					<Alert.Title>{m.common_action_failed()}</Alert.Title>
					<Alert.Description>{extractApiErrorMessage(enrollmentQuery.error)}</Alert.Description>
				</Alert.Root>
			{:else if enabled === undefined}
				<div class="flex items-center justify-center py-10">
					<Spinner class="size-6" />
				</div>
			{:else}
				<Alert.Root variant={enabled ? 'primary-subtle' : 'warning-subtle'}>
					<AgentConnectionIcon class="size-4" />
					<Alert.Title>{enabled ? m.enrollment_is_on() : m.enrollment_is_off()}</Alert.Title>
					<Alert.Description>{enabled ? m.enrollment_on_description() : m.enrollment_off_description()}</Alert.Description>
				</Alert.Root>
			{/if}

			{#if shownStack}
				<Alert.Root variant="warning-subtle">
					<AlertTriangleIcon class="size-4" />
					<Alert.Title>{m.copy_the_stack_now()}</Alert.Title>
					<Alert.Description>{m.stack_token_shown_once()}</Alert.Description>
				</Alert.Root>
				<AgentCommandBlock
					dockerRunLabel={m.stack_file_save_as()}
					dockerRun={shownStack.stack}
					dockerComposeLabel={m.deploy_from_swarm_manager()}
					dockerCompose={shownStack.deployCommand}
					preClass="pr-12"
				/>
			{/if}
		</div>
	{/snippet}

	{#snippet footer()}
		<div class="flex w-full flex-col gap-2 sm:flex-row sm:justify-end">
			{#if enabled}
				<ArcaneButton action="remove" customLabel={m.revoke_token()} onclick={revokeToken} disabled={isMutating} />
			{/if}
			<ArcaneButton
				action="create"
				customLabel={enabled ? m.rotate_token() : m.generate_token()}
				onclick={rotateToken}
				loading={isMutating}
				disabled={isMutating || enabled === undefined}
			/>
		</div>
	{/snippet}
</ResponsiveDialog>
