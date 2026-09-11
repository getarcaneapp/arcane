<script lang="ts">
	import { tryCatch } from '#lib/utils/try-catch.js';
	import { createQuery } from '@tanstack/svelte-query';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import userStore from '#lib/stores/user-store.svelte.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { onDestroy } from 'svelte';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { ResponsiveDialog } from '#lib/components/ui/responsive-dialog/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { swarmService } from '#lib/services/swarm-service.js';
	import type { SwarmJoinCandidate, SwarmJoinEnvironmentResult, SwarmJoinEnvironmentTarget } from '#lib/types/swarm.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';

	type Props = {
		open: boolean;
		managerEnvironmentId?: string;
		targetEnvironmentId?: string;
		onComplete?: () => void | Promise<void>;
	};

	let { open = $bindable(false), managerEnvironmentId, targetEnvironmentId, onComplete }: Props = $props();
	const requestedManagerId = $derived(managerEnvironmentId ?? environmentStore.selected?.id);
	const candidatesQuery = createQuery(() => {
		const environmentId = requestedManagerId;
		return {
			queryKey: queryKeys.swarm.joinCandidates(environmentId ?? null, userStore.current),
			queryFn: async () => {
				await environmentStore.ready;
				return swarmService.getSwarmJoinCandidates(environmentId!);
			},
			enabled: open && !!environmentId && hasPermission('swarm:join', environmentId)
		};
	});
	const candidates = $derived(
		(candidatesQuery.data ?? []).filter((candidate) => !targetEnvironmentId || candidate.environmentId === targetEnvironmentId)
	);
	let targetDraft = $state<Record<string, SwarmJoinEnvironmentTarget> | null>(null);
	const targets = $derived.by(() => {
		if (targetDraft) return targetDraft;
		if (!targetEnvironmentId || !candidates[0]) return {};
		const candidate = candidates[0];
		return {
			[candidate.environmentId]: {
				environmentId: candidate.environmentId,
				role: 'worker' as const,
				availability: 'active' as const
			}
		};
	});
	let results = $state<SwarmJoinEnvironmentResult[]>([]);
	let mutationError = $state('');
	const errorMessage = $derived.by(() => {
		if (mutationError) return mutationError;
		if (candidatesQuery.error) return extractApiErrorMessage(candidatesQuery.error);
		return '';
	});
	let isJoining = $state(false);
	const isLoading = $derived(candidatesQuery.isFetching || isJoining);
	let destroyed = false;
	onDestroy(() => {
		destroyed = true;
	});

	function toggleCandidate(candidate: SwarmJoinCandidate, selected: boolean) {
		if (selected) {
			targetDraft = {
				...targets,
				[candidate.environmentId]: {
					environmentId: candidate.environmentId,
					role: 'worker',
					availability: 'active'
				}
			};
			return;
		}
		const next = { ...targets };
		delete next[candidate.environmentId];
		targetDraft = next;
	}

	function updateTarget(environmentId: string, update: Partial<SwarmJoinEnvironmentTarget>) {
		const current = targets[environmentId];
		if (!current) return;
		targetDraft = { ...targets, [environmentId]: { ...current, ...update } };
	}

	async function joinSelected() {
		const environmentId = requestedManagerId;
		if (!environmentId) return;
		const selectedTargets = Object.values(targets);
		if (selectedTargets.length === 0) {
			mutationError = m.swarm_easy_join_targets_required();
			return;
		}
		isJoining = true;
		mutationError = '';
		results = [];
		try {
			const operationResult = await tryCatch(
				(async () => {
					const response = await swarmService.joinEnvironments({ remoteAddrs: [], targets: selectedTargets }, environmentId);
					if (destroyed || environmentId !== requestedManagerId) return;
					results = response.results;
					await onComplete?.();
				})()
			);
			if (operationResult.error !== null && !destroyed && environmentId === requestedManagerId) {
				const error = operationResult.error;

				mutationError = extractApiErrorMessage(error);
			}
		} finally {
			isJoining = false;
		}
	}

	function resultLabel(result: SwarmJoinEnvironmentResult): string {
		switch (result.state) {
			case 'joined':
				return m.swarm_easy_join_result_joined();
			case 'already_member':
				return m.swarm_easy_join_result_already_member();
			case 'joined_unverified':
				return m.swarm_easy_join_result_joined_unverified();
			case 'failed':
			default:
				return m.common_failed();
		}
	}
</script>

<ResponsiveDialog
	bind:open
	title={m.swarm_easy_join_title()}
	description={m.swarm_easy_join_description()}
	contentClass="sm:max-w-3xl"
>
	{#snippet children()}
		<div class="space-y-5 py-4">
			{#if errorMessage}
				<Alert.Root variant="destructive">
					<Alert.Title>{m.common_action_failed()}</Alert.Title>
					<Alert.Description>{errorMessage}</Alert.Description>
				</Alert.Root>
			{/if}

			{#if isLoading && candidates.length === 0}
				<div class="flex justify-center py-10"><Spinner class="size-6" /></div>
			{:else if candidates.length === 0}
				<div class="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">
					{m.swarm_easy_join_no_candidates()}
				</div>
			{:else}
				<div class="space-y-3">
					{#each candidates as candidate (candidate.environmentId)}
						{@const target = targets[candidate.environmentId]}
						<div class="rounded-lg border p-4">
							<label class="flex items-start gap-3">
								<input
									type="checkbox"
									checked={!!target}
									onchange={(event) => toggleCandidate(candidate, event.currentTarget.checked)}
								/>
								<span>
									<span class="block text-sm font-medium">{candidate.environmentName}</span>
									<span class="text-xs text-muted-foreground">{candidate.environmentType} · {candidate.status}</span>
								</span>
							</label>

							{#if target}
								<div class="mt-4 space-y-3 border-t pt-4">
									<div class="grid gap-3 sm:grid-cols-2">
										<label class="space-y-1 text-xs">
											<span class="text-muted-foreground">{m.common_role()}</span>
											<select
												class="h-9 w-full rounded-md border border-input bg-background px-3"
												value={target.role}
												onchange={(event) =>
													updateTarget(candidate.environmentId, {
														role: event.currentTarget.value as 'worker' | 'manager'
													})}
											>
												<option value="worker">{m.worker()}</option>
												<option value="manager">{m.manager()}</option>
											</select>
										</label>
										<label class="space-y-1 text-xs">
											<span class="text-muted-foreground">{m.swarm_availability()}</span>
											<select
												class="h-9 w-full rounded-md border border-input bg-background px-3"
												value={target.availability}
												onchange={(event) =>
													updateTarget(candidate.environmentId, {
														availability: event.currentTarget.value as 'active' | 'pause' | 'drain'
													})}
											>
												<option value="active">{m.common_active()}</option>
												<option value="pause">{m.common_pause()}</option>
												<option value="drain">{m.drain()}</option>
											</select>
										</label>
									</div>
									<div class="grid gap-3 sm:grid-cols-3">
										<Input
											placeholder={m.swarm_cluster_listen_addr_placeholder()}
											value={target.listenAddr ?? ''}
											oninput={(event) => updateTarget(candidate.environmentId, { listenAddr: event.currentTarget.value })}
										/>
										<Input
											placeholder={m.swarm_cluster_advertise_addr_placeholder()}
											value={target.advertiseAddr ?? ''}
											oninput={(event) => updateTarget(candidate.environmentId, { advertiseAddr: event.currentTarget.value })}
										/>
										<Input
											placeholder={m.swarm_easy_join_data_path_placeholder()}
											value={target.dataPathAddr ?? ''}
											oninput={(event) => updateTarget(candidate.environmentId, { dataPathAddr: event.currentTarget.value })}
										/>
									</div>
								</div>
							{/if}
						</div>
					{/each}
				</div>
			{/if}

			{#if results.length > 0}
				<div class="space-y-2 rounded-lg border p-4">
					<div class="font-medium">{m.swarm_easy_join_results_title()}</div>
					{#each results as result (result.environmentId)}
						<div class="flex items-start justify-between gap-4 text-sm">
							<span
								>{candidates.find((candidate) => candidate.environmentId === result.environmentId)?.environmentName ??
									result.environmentId}</span
							>
							<span class={result.state === 'failed' ? 'text-right text-destructive' : 'text-right text-muted-foreground'}>
								{resultLabel(result)}{result.error ? `: ${result.error}` : ''}
							</span>
						</div>
					{/each}
				</div>
			{/if}

			<div class="flex justify-end gap-2 border-t pt-4">
				<ArcaneButton action="base" tone="outline" customLabel={m.common_cancel()} onclick={() => (open = false)} />
				<ArcaneButton
					action="create"
					customLabel={m.swarm_easy_join_action()}
					onclick={joinSelected}
					loading={isLoading}
					disabled={isLoading || Object.keys(targets).length === 0}
				/>
			</div>
		</div>
	{/snippet}
</ResponsiveDialog>
