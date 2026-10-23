<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import { CopyButton } from '#lib/components/ui/copy-button/index.js';
	import * as InputGroup from '#lib/components/ui/input-group/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { ResponsiveDialog } from '#lib/components/ui/responsive-dialog/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import { Textarea } from '#lib/components/ui/textarea/index.js';
	import { EyeOffIcon, EyeOnIcon, SettingsIcon, UsersIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { swarmService } from '#lib/services/swarm-service.js';
	import userStore from '#lib/stores/user-store.svelte.js';
	import type { SwarmInitRequest, SwarmJoinRequest, SwarmUpdateRequest } from '#lib/types/swarm.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	let { environmentId, onChanged }: { environmentId: string; onChanged?: () => void | Promise<void> } = $props();

	const canManageSwarm = $derived(hasPermission('swarm:nodes', environmentId));

	const swarmInfoQuery = createQuery(() => {
		const requestedEnvironmentId = environmentId;
		userStore.current;
		return {
			queryKey: queryKeys.swarm.info(requestedEnvironmentId),
			queryFn: () => swarmService.getSwarmInfo(requestedEnvironmentId),
			enabled: hasPermission('swarm:read', requestedEnvironmentId),
			retry: false
		};
	});
	const joinTokensQuery = createQuery(() => {
		const requestedEnvironmentId = environmentId;
		userStore.current;
		return {
			queryKey: queryKeys.swarm.joinTokens(requestedEnvironmentId),
			queryFn: () => swarmService.getSwarmJoinTokens(requestedEnvironmentId),
			enabled: hasPermission('swarm:unlock', requestedEnvironmentId),
			retry: false
		};
	});
	const unlockKeyQuery = createQuery(() => {
		const requestedEnvironmentId = environmentId;
		userStore.current;
		return {
			queryKey: queryKeys.swarm.unlockKey(requestedEnvironmentId),
			queryFn: () => swarmService.getSwarmUnlockKey(requestedEnvironmentId),
			enabled: hasPermission('swarm:unlock', requestedEnvironmentId),
			retry: false
		};
	});
	const swarmInfo = $derived(swarmInfoQuery.data ?? null);
	const joinTokens = $derived(joinTokensQuery.data ?? null);
	const unlockKey = $derived(unlockKeyQuery.data?.unlockKey ?? '');
	let showManagerToken = $state(false);
	let showWorkerToken = $state(false);
	let securityDialogOpen = $state(false);
	const isSwarmInitialized = $derived(!!swarmInfo?.id);

	let initForm = $state({
		listenAddr: '',
		advertiseAddr: '',
		spec: '{}',
		autoLockManagers: false,
		forceNewCluster: false
	});

	let joinForm = $state({
		remoteAddrs: '',
		joinToken: '',
		listenAddr: '',
		advertiseAddr: ''
	});

	let leaveForce = $state(false);
	let unlockInput = $state('');
	let specDraft = $state<string | null>(null);
	const updateSpec = $derived(specDraft ?? JSON.stringify(swarmInfo?.spec ?? {}, null, 2));
	let updateForm = $state({
		version: '',
		rotateWorkerToken: false,
		rotateManagerToken: false,
		rotateManagerUnlockKey: false
	});

	let isLoading = $state({
		refresh: false,
		init: false,
		join: false,
		leave: false,
		unlock: false,
		rotateTokens: false,
		updateSpec: false
	});

	function parseObjectJSON(raw: string, label: string): Record<string, unknown> | null {
		try {
			const parsed = JSON.parse(raw || '{}');
			if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
				toast.error(m.swarm_cluster_json_object_error({ label }));
				return null;
			}
			return parsed as Record<string, unknown>;
		} catch {
			toast.error(m.swarm_cluster_json_invalid_error({ label }));
			return null;
		}
	}

	function splitCsv(input: string): string[] {
		return input
			.split(',')
			.map((value) => value.trim())
			.filter(Boolean);
	}

	function loadCurrentSpec() {
		const currentSpec = swarmInfo?.spec ?? {};
		specDraft = JSON.stringify(currentSpec, null, 2);
	}

	async function refresh() {
		isLoading.refresh = true;
		showManagerToken = false;
		showWorkerToken = false;
		try {
			const requests: Promise<unknown>[] = [];
			if (hasPermission('swarm:read', environmentId)) requests.push(swarmInfoQuery.refetch());
			if (hasPermission('swarm:unlock', environmentId)) requests.push(joinTokensQuery.refetch(), unlockKeyQuery.refetch());
			await Promise.all(requests);
			await onChanged?.();
		} finally {
			isLoading.refresh = false;
		}
	}

	async function handleInit() {
		const spec = parseObjectJSON(initForm.spec, m.swarm_cluster_init_spec_label());
		if (!spec) return;

		const request: SwarmInitRequest = { spec };
		if (initForm.listenAddr.trim()) request.listenAddr = initForm.listenAddr.trim();
		if (initForm.advertiseAddr.trim()) request.advertiseAddr = initForm.advertiseAddr.trim();
		request.autoLockManagers = initForm.autoLockManagers;
		request.forceNewCluster = initForm.forceNewCluster;

		isLoading.init = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(swarmService.initSwarm(request, environmentId)),
			message: m.swarm_cluster_init_failed(),
			setLoadingState: (value) => (isLoading.init = value),
			onSuccess: async () => {
				toast.success(m.swarm_cluster_init_success());
				await refresh();
			}
		});
	}

	async function handleJoin() {
		const remoteAddrs = splitCsv(joinForm.remoteAddrs);
		if (remoteAddrs.length === 0 || !joinForm.joinToken.trim()) {
			toast.error(m.swarm_cluster_join_required_error());
			return;
		}

		const request: SwarmJoinRequest = {
			remoteAddrs,
			joinToken: joinForm.joinToken.trim()
		};
		if (joinForm.listenAddr.trim()) request.listenAddr = joinForm.listenAddr.trim();
		if (joinForm.advertiseAddr.trim()) request.advertiseAddr = joinForm.advertiseAddr.trim();

		isLoading.join = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(swarmService.joinSwarm(request, environmentId)),
			message: m.swarm_cluster_join_failed(),
			setLoadingState: (value) => (isLoading.join = value),
			onSuccess: async () => {
				toast.success(m.swarm_cluster_join_success());
				await refresh();
			}
		});
	}

	async function handleLeave() {
		isLoading.leave = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(swarmService.leaveSwarm({ force: leaveForce }, environmentId)),
			message: m.swarm_cluster_leave_failed(),
			setLoadingState: (value) => (isLoading.leave = value),
			onSuccess: async () => {
				toast.success(m.swarm_cluster_leave_success());
				securityDialogOpen = false;
				await refresh();
			}
		});
	}

	async function handleUnlock() {
		if (!unlockInput.trim()) {
			toast.error(m.swarm_cluster_unlock_key_required());
			return;
		}

		isLoading.unlock = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(swarmService.unlockSwarm({ key: unlockInput.trim() }, environmentId)),
			message: m.swarm_cluster_unlock_failed(),
			setLoadingState: (value) => (isLoading.unlock = value),
			onSuccess: async () => {
				toast.success(m.swarm_cluster_unlock_success());
				unlockInput = '';
				securityDialogOpen = false;
				await refresh();
			}
		});
	}

	async function handleRotateTokens() {
		isLoading.rotateTokens = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(
				swarmService.rotateSwarmJoinTokens(
					{
						rotateManagerToken: true,
						rotateWorkerToken: true
					},
					environmentId
				)
			),
			message: m.swarm_cluster_rotate_tokens_failed(),
			setLoadingState: (value) => (isLoading.rotateTokens = value),
			onSuccess: async () => {
				toast.success(m.swarm_cluster_rotate_tokens_success());
				await refresh();
			}
		});
	}

	async function handleUpdateSpec() {
		const spec = parseObjectJSON(updateSpec, m.swarm_cluster_spec_label());
		if (!spec) return;

		const parsedVersion = Number.parseInt(updateForm.version, 10);
		const request: SwarmUpdateRequest = {
			spec,
			rotateWorkerToken: updateForm.rotateWorkerToken,
			rotateManagerToken: updateForm.rotateManagerToken,
			rotateManagerUnlockKey: updateForm.rotateManagerUnlockKey
		};

		if (Number.isFinite(parsedVersion) && parsedVersion > 0) {
			request.version = parsedVersion;
		}

		isLoading.updateSpec = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(swarmService.updateSwarmSpec(request, environmentId)),
			message: m.swarm_cluster_update_spec_failed(),
			setLoadingState: (value) => (isLoading.updateSpec = value),
			onSuccess: async () => {
				toast.success(m.swarm_cluster_update_spec_success());
				await refresh();
			}
		});
	}
</script>

<div class="space-y-6">
	<div class="flex justify-end">
		<ArcaneButton
			action="refresh"
			size="sm"
			customLabel={m.common_refresh()}
			onclick={refresh}
			loading={isLoading.refresh}
			disabled={isLoading.refresh}
		/>
	</div>
	<div class="space-y-6 pb-6">
		{#if isSwarmInitialized}
			<div class="grid gap-6 xl:grid-cols-2">
				<Card.Root>
					<Card.Header>
						<Card.Title>{m.swarm_cluster_status_title()}</Card.Title>
						<Card.Description>{m.swarm_cluster_status_subtitle()}</Card.Description>
						<Card.Action>
							<ArcaneButton
								action="inspect"
								size="sm"
								customLabel={m.common_actions()}
								onclick={() => (securityDialogOpen = true)}
								disabled={!canManageSwarm}
							/>
						</Card.Action>
					</Card.Header>
					<Card.Content>
						<div class="divide-y divide-border/60 text-sm">
							<div class="grid gap-1 py-3 sm:grid-cols-aside-35 sm:gap-4">
								<span class="text-muted-foreground">{m.swarm_cluster_id_label()}</span>
								<span class="font-mono break-all sm:text-right">{swarmInfo?.id ?? m.swarm_cluster_not_initialized()}</span>
							</div>
							<div class="grid gap-1 py-3 sm:grid-cols-aside-35 sm:gap-4">
								<span class="text-muted-foreground">{m.common_created()}</span>
								<span class="sm:text-right">{swarmInfo?.createdAt ?? m.common_na()}</span>
							</div>
							<div class="grid gap-1 pt-3 sm:grid-cols-aside-35 sm:gap-4">
								<span class="text-muted-foreground">{m.common_updated()}</span>
								<span class="sm:text-right">{swarmInfo?.updatedAt ?? m.common_na()}</span>
							</div>
						</div>
					</Card.Content>
				</Card.Root>

				<Card.Root>
					<Card.Header>
						<Card.Title>{m.swarm_cluster_join_tokens_title()}</Card.Title>
						<Card.Description>{m.swarm_cluster_join_tokens_subtitle()}</Card.Description>
						<Card.Action>
							<ArcaneButton
								action="restart"
								size="sm"
								customLabel={m.swarm_cluster_rotate_tokens()}
								onclick={handleRotateTokens}
								disabled={!canManageSwarm || !isSwarmInitialized || isLoading.rotateTokens}
								loading={isLoading.rotateTokens}
							/>
						</Card.Action>
					</Card.Header>
					<Card.Content>
						<div class="flex flex-col gap-4">
							<div class="space-y-2">
								<div class="text-xs font-medium text-muted-foreground">{m.manager()}</div>
								<div class="flex items-center gap-2">
									<InputGroup.Root class="flex-1">
										<InputGroup.Input
											type={showManagerToken ? 'text' : 'password'}
											value={joinTokens?.manager ?? ''}
											readonly
											mono
											size="sm"
										/>
										<InputGroup.Addon align="inline-end">
											<InputGroup.Button
												type="button"
												size="icon-xs"
												onclick={() => (showManagerToken = !showManagerToken)}
												disabled={!joinTokens?.manager}
												aria-label={showManagerToken ? m.common_hide() : m.common_show()}
											>
												{#if showManagerToken}
													<EyeOffIcon />
												{:else}
													<EyeOnIcon />
												{/if}
											</InputGroup.Button>
										</InputGroup.Addon>
									</InputGroup.Root>
									{#if joinTokens?.manager}
										<CopyButton text={joinTokens.manager} />
									{/if}
								</div>
							</div>
							<div class="space-y-2">
								<div class="text-xs font-medium text-muted-foreground">{m.worker()}</div>
								<div class="flex items-center gap-2">
									<InputGroup.Root class="flex-1">
										<InputGroup.Input
											type={showWorkerToken ? 'text' : 'password'}
											value={joinTokens?.worker ?? ''}
											readonly
											mono
											size="sm"
										/>
										<InputGroup.Addon align="inline-end">
											<InputGroup.Button
												type="button"
												size="icon-xs"
												onclick={() => (showWorkerToken = !showWorkerToken)}
												disabled={!joinTokens?.worker}
												aria-label={showWorkerToken ? m.common_hide() : m.common_show()}
											>
												{#if showWorkerToken}
													<EyeOffIcon />
												{:else}
													<EyeOnIcon />
												{/if}
											</InputGroup.Button>
										</InputGroup.Addon>
									</InputGroup.Root>
									{#if joinTokens?.worker}
										<CopyButton text={joinTokens.worker} />
									{/if}
								</div>
							</div>
						</div>
					</Card.Content>
				</Card.Root>
			</div>

			<Card.Root>
				<Card.Header>
					<Card.Title>{m.swarm_cluster_update_spec_title()}</Card.Title>
					<Card.Description>{m.swarm_cluster_update_spec_subtitle()}</Card.Description>
					<Card.Action>
						<div class="flex flex-wrap items-center justify-end gap-4">
							<label class="flex items-center gap-2 text-sm whitespace-nowrap">
								<Switch id="rotate-worker-token" bind:checked={updateForm.rotateWorkerToken} />
								<span>{m.swarm_cluster_rotate_worker_token()}</span>
							</label>
							<label class="flex items-center gap-2 text-sm whitespace-nowrap">
								<Switch id="rotate-manager-token" bind:checked={updateForm.rotateManagerToken} />
								<span>{m.swarm_cluster_rotate_manager_token()}</span>
							</label>
							<label class="flex items-center gap-2 text-sm whitespace-nowrap">
								<Switch id="rotate-unlock-key" bind:checked={updateForm.rotateManagerUnlockKey} />
								<span>{m.swarm_cluster_rotate_unlock_key()}</span>
							</label>
							<ArcaneButton
								action="inspect"
								size="sm"
								customLabel={m.swarm_cluster_load_current_spec()}
								onclick={loadCurrentSpec}
							/>
						</div>
					</Card.Action>
				</Card.Header>
				<Card.Content>
					<div class="flex flex-col gap-4">
						<Input placeholder={m.swarm_cluster_version_placeholder()} bind:value={updateForm.version} class="max-w-sm" />
						<Textarea
							rows={22}
							placeholder={m.swarm_cluster_spec_placeholder()}
							bind:value={() => updateSpec, (value) => (specDraft = value)}
							mono
							size="sm"
							class="min-h-136"
						/>
						<div class="flex justify-end border-t pt-4">
							<ArcaneButton
								action="save"
								customLabel={m.swarm_cluster_update_spec_action()}
								onclick={handleUpdateSpec}
								disabled={!canManageSwarm || isLoading.updateSpec}
								loading={isLoading.updateSpec}
							/>
						</div>
					</div>
				</Card.Content>
			</Card.Root>
		{:else}
			<div class="mx-auto w-full max-w-5xl space-y-5 pt-2">
				<div class="rounded-xl border border-border/60 bg-muted/20 px-5 py-4 sm:px-6">
					<p class="font-medium">{m.swarm_cluster_setup_title()}</p>
					<p class="mt-1 max-w-3xl text-sm leading-relaxed text-muted-foreground">
						{m.swarm_cluster_setup_description()}
					</p>
				</div>

				<div class="grid items-stretch gap-5 lg:grid-cols-2">
					<Card.Root class="flex h-full flex-col" variant="outlined">
						<Card.Header icon={SettingsIcon} iconVariant="primary">
							<div class="min-w-0">
								<Card.Title>{m.swarm_cluster_initialize_title()}</Card.Title>
								<Card.Description class="mt-1">{m.swarm_cluster_initialize_description()}</Card.Description>
							</div>
						</Card.Header>
						<Card.Content class="flex-1">
							<div class="flex flex-1 flex-col gap-5">
								<p class="text-sm leading-relaxed text-muted-foreground">{m.swarm_cluster_initialize_hint()}</p>

								<details class="rounded-lg border border-border/60 bg-muted/15">
									<summary class="cursor-pointer px-4 py-3 text-sm font-medium select-none">
										{m.swarm_cluster_advanced_settings()}
									</summary>
									<div class="space-y-4 border-t px-4 py-4">
										<div class="grid gap-3 sm:grid-cols-2">
											<Input placeholder={m.swarm_cluster_listen_addr_placeholder()} bind:value={initForm.listenAddr} />
											<Input placeholder={m.swarm_cluster_advertise_addr_placeholder()} bind:value={initForm.advertiseAddr} />
										</div>
										<Textarea
											rows={5}
											placeholder={m.swarm_cluster_spec_placeholder()}
											bind:value={initForm.spec}
											mono
											size="sm"
										/>
										<div class="grid gap-3 sm:grid-cols-2">
											<label class="flex items-center gap-2 text-sm">
												<input type="checkbox" bind:checked={initForm.autoLockManagers} />
												{m.swarm_cluster_auto_lock_managers()}
											</label>
											<label class="flex items-center gap-2 text-sm">
												<input type="checkbox" bind:checked={initForm.forceNewCluster} />
												{m.swarm_cluster_force_new_cluster()}
											</label>
										</div>
									</div>
								</details>

								<div class="mt-auto border-t pt-5">
									<ArcaneButton
										action="create"
										customLabel={m.swarm_cluster_initialize_action()}
										onclick={handleInit}
										disabled={!canManageSwarm || isLoading.init}
										loading={isLoading.init}
									/>
								</div>
							</div>
						</Card.Content>
					</Card.Root>

					<Card.Root class="flex h-full flex-col" variant="outlined">
						<Card.Header icon={UsersIcon} iconVariant="emerald">
							<div class="min-w-0">
								<Card.Title>{m.swarm_cluster_join_title()}</Card.Title>
								<Card.Description class="mt-1">{m.swarm_cluster_join_description()}</Card.Description>
							</div>
						</Card.Header>
						<Card.Content class="flex-1">
							<div class="flex flex-1 flex-col gap-4">
								<label class="space-y-2">
									<span class="text-sm font-medium">{m.swarm_cluster_manager_addresses_label()}</span>
									<Input placeholder={m.swarm_cluster_join_remote_addrs_placeholder()} bind:value={joinForm.remoteAddrs} />
								</label>
								<label class="space-y-2">
									<span class="text-sm font-medium">{m.swarm_cluster_join_token_placeholder()}</span>
									<Input placeholder={m.swarm_cluster_join_token_placeholder()} bind:value={joinForm.joinToken} mono size="sm" />
								</label>

								<details class="rounded-lg border border-border/60 bg-muted/15">
									<summary class="cursor-pointer px-4 py-3 text-sm font-medium select-none">
										{m.swarm_cluster_advanced_settings()}
									</summary>
									<div class="grid gap-3 border-t px-4 py-4 sm:grid-cols-2">
										<Input placeholder={m.swarm_cluster_listen_addr_placeholder()} bind:value={joinForm.listenAddr} />
										<Input placeholder={m.swarm_cluster_advertise_addr_placeholder()} bind:value={joinForm.advertiseAddr} />
									</div>
								</details>

								<div class="mt-auto border-t pt-5">
									<ArcaneButton
										action="create"
										customLabel={m.swarm_cluster_join_action()}
										onclick={handleJoin}
										disabled={!canManageSwarm || isLoading.join}
										loading={isLoading.join}
									/>
								</div>
							</div>
						</Card.Content>
					</Card.Root>
				</div>
			</div>
		{/if}
	</div>

	<ResponsiveDialog
		bind:open={securityDialogOpen}
		title={m.swarm_cluster_unlock_leave_title()}
		description={m.swarm_cluster_unlock_leave_subtitle()}
		contentClass="sm:max-w-lg"
	>
		{#snippet children()}
			<div class="space-y-6 py-4">
				<div class="space-y-3">
					{#if unlockKey}
						<div class="flex items-center justify-between gap-3 rounded-lg border border-border/60 bg-muted/15 px-3 py-2 text-sm">
							<span class="text-muted-foreground">{m.swarm_cluster_stat_unlock_key()}</span>
							<CopyButton text={unlockKey} />
						</div>
					{/if}
					<Input placeholder={m.swarm_cluster_unlock_key_placeholder()} bind:value={unlockInput} mono size="sm" />
					<div class="flex items-center gap-2">
						<ArcaneButton
							action="save"
							customLabel={m.swarm_cluster_unlock_action()}
							onclick={handleUnlock}
							disabled={!canManageSwarm || isLoading.unlock}
							loading={isLoading.unlock}
						/>
					</div>
				</div>

				<div class="space-y-3 border-t pt-4">
					<label class="flex items-center gap-2 text-sm">
						<input type="checkbox" bind:checked={leaveForce} />
						{m.swarm_cluster_force_leave()}
					</label>
					<ArcaneButton
						action="remove"
						customLabel={m.swarm_cluster_leave_action()}
						onclick={handleLeave}
						disabled={!canManageSwarm || isLoading.leave}
						loading={isLoading.leave}
					/>
				</div>
			</div>
		{/snippet}
	</ResponsiveDialog>
</div>
