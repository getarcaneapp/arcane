<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import * as InputGroup from '$lib/components/ui/input-group/index.js';
	import { ResponsiveDialog } from '$lib/components/ui/responsive-dialog/index.js';
	import { Switch } from '$lib/components/ui/switch/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import { CopyButton } from '$lib/components/ui/copy-button';
	import { useEnvironmentRefresh } from '$lib/hooks/use-environment-refresh.svelte';
	import { EyeOffIcon, EyeOnIcon, LockIcon, SettingsIcon, UsersIcon } from '$lib/icons';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index.js';
	import { m } from '$lib/paraglide/messages';
	import { swarmService } from '$lib/services/swarm-service';
	import userStore from '$lib/stores/user-store';
	import type {
		SwarmInfo,
		SwarmInitRequest,
		SwarmJoinRequest,
		SwarmJoinTokensResponse,
		SwarmUpdateRequest
	} from '$lib/types/swarm.type';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import { fromStore } from 'svelte/store';
	import { toast } from 'svelte-sonner';

	const storeUser = fromStore(userStore);
	const isAdmin = $derived(!!storeUser.current?.roles?.includes('admin'));

	let swarmInfo = $state<SwarmInfo | null>(null);
	let joinTokens = $state<SwarmJoinTokensResponse | null>(null);
	let unlockKey = $state('');
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
	let updateForm = $state({
		version: '',
		spec: '{}',
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
		updateForm.spec = JSON.stringify(currentSpec, null, 2);
	}

	async function refresh() {
		isLoading.refresh = true;
		try {
			const [infoRes, tokensRes, unlockRes] = await Promise.allSettled([
				swarmService.getSwarmInfo(),
				swarmService.getSwarmJoinTokens(),
				swarmService.getSwarmUnlockKey()
			]);

			swarmInfo = infoRes.status === 'fulfilled' ? infoRes.value : null;
			joinTokens = tokensRes.status === 'fulfilled' ? tokensRes.value : null;
			unlockKey = unlockRes.status === 'fulfilled' ? unlockRes.value.unlockKey : '';
			showManagerToken = false;
			showWorkerToken = false;

			if (swarmInfo && updateForm.spec === '{}') {
				loadCurrentSpec();
			}
		} finally {
			isLoading.refresh = false;
		}
	}

	useEnvironmentRefresh(refresh);

	$effect(() => {
		refresh();
	});

	async function handleInit() {
		const spec = parseObjectJSON(initForm.spec, m.swarm_cluster_init_spec_label());
		if (!spec) return;

		const request: SwarmInitRequest = { spec };
		if (initForm.listenAddr.trim()) request.listenAddr = initForm.listenAddr.trim();
		if (initForm.advertiseAddr.trim()) request.advertiseAddr = initForm.advertiseAddr.trim();
		request.autoLockManagers = initForm.autoLockManagers;
		request.forceNewCluster = initForm.forceNewCluster;

		handleApiResultWithCallbacks({
			result: await tryCatch(swarmService.initSwarm(request)),
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

		handleApiResultWithCallbacks({
			result: await tryCatch(swarmService.joinSwarm(request)),
			message: m.swarm_cluster_join_failed(),
			setLoadingState: (value) => (isLoading.join = value),
			onSuccess: async () => {
				toast.success(m.swarm_cluster_join_success());
				await refresh();
			}
		});
	}

	async function handleLeave() {
		handleApiResultWithCallbacks({
			result: await tryCatch(swarmService.leaveSwarm({ force: leaveForce })),
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

		handleApiResultWithCallbacks({
			result: await tryCatch(swarmService.unlockSwarm({ key: unlockInput.trim() })),
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
		handleApiResultWithCallbacks({
			result: await tryCatch(
				swarmService.rotateSwarmJoinTokens({
					rotateManagerToken: true,
					rotateWorkerToken: true
				})
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
		const spec = parseObjectJSON(updateForm.spec, m.swarm_cluster_spec_label());
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

		handleApiResultWithCallbacks({
			result: await tryCatch(swarmService.updateSwarmSpec(request)),
			message: m.swarm_cluster_update_spec_failed(),
			setLoadingState: (value) => (isLoading.updateSpec = value),
			onSuccess: async () => {
				toast.success(m.swarm_cluster_update_spec_success());
				await refresh();
			}
		});
	}

	const actionButtons: ActionButton[] = $derived([
		{
			id: 'refresh',
			action: 'restart',
			label: m.common_refresh(),
			onclick: refresh,
			loading: isLoading.refresh,
			disabled: isLoading.refresh
		}
	]);

	const statCards: StatCardConfig[] = $derived([
		{
			title: m.swarm_cluster_stat_cluster(),
			value: swarmInfo?.id ? swarmInfo.id.slice(0, 12) : m.swarm_cluster_not_initialized(),
			icon: SettingsIcon,
			iconColor: 'text-blue-500'
		},
		{
			title: m.swarm_cluster_stat_tokens(),
			value: joinTokens ? 2 : 0,
			icon: UsersIcon,
			iconColor: 'text-emerald-500'
		},
		{
			title: m.swarm_cluster_stat_unlock_key(),
			value: unlockKey ? m.common_available() : m.common_unavailable(),
			icon: LockIcon,
			iconColor: 'text-amber-500'
		}
	]);
</script>

<ResourcePageLayout
	title={m.swarm_cluster_title()}
	subtitle={m.swarm_cluster_subtitle()}
	icon={SettingsIcon}
	class="pb-6"
	{actionButtons}
	{statCards}
>
	{#snippet mainContent()}
		<div class="space-y-6 pb-6">
			<div class="grid gap-6 xl:grid-cols-[minmax(0,1.05fr)_minmax(0,0.95fr)]">
				<Card.Root class="pt-0">
					<Card.Header>
						<Card.Title>{m.swarm_cluster_status_title()}</Card.Title>
						<Card.Description>{m.swarm_cluster_status_subtitle()}</Card.Description>
						<Card.Action>
							<ArcaneButton
								action="inspect"
								size="sm"
								customLabel={m.common_actions()}
								onclick={() => (securityDialogOpen = true)}
								disabled={!isAdmin}
							/>
						</Card.Action>
					</Card.Header>
					<Card.Content class="pb-6">
						<div class="divide-border/60 divide-y text-sm">
							<div class="grid gap-1 py-3 sm:grid-cols-[140px_minmax(0,1fr)] sm:gap-4">
								<span class="text-muted-foreground">{m.swarm_cluster_id_label()}</span>
								<span class="font-mono break-all sm:text-right">{swarmInfo?.id ?? m.swarm_cluster_not_initialized()}</span>
							</div>
							<div class="grid gap-1 py-3 sm:grid-cols-[140px_minmax(0,1fr)] sm:gap-4">
								<span class="text-muted-foreground">{m.common_created()}</span>
								<span class="sm:text-right">{swarmInfo?.createdAt ?? m.common_na()}</span>
							</div>
							<div class="grid gap-1 pt-3 sm:grid-cols-[140px_minmax(0,1fr)] sm:gap-4">
								<span class="text-muted-foreground">{m.common_updated()}</span>
								<span class="sm:text-right">{swarmInfo?.updatedAt ?? m.common_na()}</span>
							</div>
						</div>
					</Card.Content>
				</Card.Root>

				<Card.Root class="pt-0">
					<Card.Header>
						<Card.Title>{m.swarm_cluster_join_tokens_title()}</Card.Title>
						<Card.Description>{m.swarm_cluster_join_tokens_subtitle()}</Card.Description>
						<Card.Action>
							<ArcaneButton
								action="restart"
								size="sm"
								customLabel={m.swarm_cluster_rotate_tokens()}
								onclick={handleRotateTokens}
								disabled={!isAdmin || !isSwarmInitialized || isLoading.rotateTokens}
								loading={isLoading.rotateTokens}
							/>
						</Card.Action>
					</Card.Header>
					<Card.Content class="space-y-4 pb-6">
						<div class="space-y-2">
							<div class="text-muted-foreground text-xs font-medium">{m.swarm_cluster_manager_token_label()}</div>
							<div class="flex items-center gap-2">
								<InputGroup.Root class="flex-1">
									<InputGroup.Input
										type={showManagerToken ? 'text' : 'password'}
										value={joinTokens?.manager ?? ''}
										readonly
										class="font-mono text-xs"
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
							<div class="text-muted-foreground text-xs font-medium">{m.swarm_cluster_worker_token_label()}</div>
							<div class="flex items-center gap-2">
								<InputGroup.Root class="flex-1">
									<InputGroup.Input
										type={showWorkerToken ? 'text' : 'password'}
										value={joinTokens?.worker ?? ''}
										readonly
										class="font-mono text-xs"
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
					</Card.Content>
				</Card.Root>
			</div>

			{#if !isSwarmInitialized}
				<div class="grid gap-6 xl:grid-cols-2">
					<Card.Root class="pt-0">
						<Card.Header>
							<Card.Title>{m.swarm_cluster_initialize_title()}</Card.Title>
							<Card.Description>{m.swarm_cluster_initialize_subtitle()}</Card.Description>
						</Card.Header>
						<Card.Content class="space-y-4 pb-6">
							<div class="grid gap-3 sm:grid-cols-2">
								<Input placeholder={m.swarm_cluster_listen_addr_placeholder()} bind:value={initForm.listenAddr} />
								<Input placeholder={m.swarm_cluster_advertise_addr_placeholder()} bind:value={initForm.advertiseAddr} />
							</div>
							<Textarea
								rows={8}
								placeholder={m.swarm_cluster_spec_placeholder()}
								bind:value={initForm.spec}
								class="font-mono text-xs"
							/>
							<div class="grid gap-2 sm:grid-cols-2">
								<label class="flex items-center gap-2 text-sm">
									<input type="checkbox" bind:checked={initForm.autoLockManagers} />
									{m.swarm_cluster_auto_lock_managers()}
								</label>
								<label class="flex items-center gap-2 text-sm">
									<input type="checkbox" bind:checked={initForm.forceNewCluster} />
									{m.swarm_cluster_force_new_cluster()}
								</label>
							</div>
							<ArcaneButton
								action="create"
								customLabel={m.swarm_cluster_initialize_action()}
								onclick={handleInit}
								disabled={!isAdmin || isLoading.init}
								loading={isLoading.init}
							/>
						</Card.Content>
					</Card.Root>

					<Card.Root class="pt-0">
						<Card.Header>
							<Card.Title>{m.swarm_cluster_join_title()}</Card.Title>
							<Card.Description>{m.swarm_cluster_join_subtitle()}</Card.Description>
						</Card.Header>
						<Card.Content class="space-y-4 pb-6">
							<Input placeholder={m.swarm_cluster_join_remote_addrs_placeholder()} bind:value={joinForm.remoteAddrs} />
							<Input
								placeholder={m.swarm_cluster_join_token_placeholder()}
								bind:value={joinForm.joinToken}
								class="font-mono text-xs"
							/>
							<div class="grid gap-3 sm:grid-cols-2">
								<Input placeholder={m.swarm_cluster_listen_addr_placeholder()} bind:value={joinForm.listenAddr} />
								<Input placeholder={m.swarm_cluster_advertise_addr_placeholder()} bind:value={joinForm.advertiseAddr} />
							</div>
							<ArcaneButton
								action="create"
								customLabel={m.swarm_cluster_join_action()}
								onclick={handleJoin}
								disabled={!isAdmin || isLoading.join}
								loading={isLoading.join}
							/>
						</Card.Content>
					</Card.Root>
				</div>
			{/if}

			<Card.Root class="pt-0">
				<Card.Header>
					<Card.Title>{m.swarm_cluster_update_spec_title()}</Card.Title>
					<Card.Description>{m.swarm_cluster_update_spec_subtitle()}</Card.Description>
					<Card.Action class="flex flex-wrap items-center justify-end gap-4">
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
					</Card.Action>
				</Card.Header>
				<Card.Content class="space-y-4 pb-6">
					<Input placeholder={m.swarm_cluster_version_placeholder()} bind:value={updateForm.version} class="max-w-sm" />
					<Textarea
						rows={22}
						placeholder={m.swarm_cluster_spec_placeholder()}
						bind:value={updateForm.spec}
						class="min-h-[34rem] font-mono text-xs"
					/>
					<div class="flex justify-end border-t pt-4">
						<ArcaneButton
							action="save"
							customLabel={m.swarm_cluster_update_spec_action()}
							onclick={handleUpdateSpec}
							disabled={!isAdmin || isLoading.updateSpec}
							loading={isLoading.updateSpec}
						/>
					</div>
				</Card.Content>
			</Card.Root>
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
							<div
								class="border-border/60 bg-muted/15 flex items-center justify-between gap-3 rounded-lg border px-3 py-2 text-sm"
							>
								<span class="text-muted-foreground">{m.swarm_cluster_stat_unlock_key()}</span>
								<CopyButton text={unlockKey} />
							</div>
						{/if}
						<Input placeholder={m.swarm_cluster_unlock_key_placeholder()} bind:value={unlockInput} class="font-mono text-xs" />
						<div class="flex items-center gap-2">
							<ArcaneButton
								action="save"
								customLabel={m.swarm_cluster_unlock_action()}
								onclick={handleUnlock}
								disabled={!isAdmin || isLoading.unlock}
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
							disabled={!isAdmin || isLoading.leave}
							loading={isLoading.leave}
						/>
					</div>
				</div>
			{/snippet}
		</ResponsiveDialog>
	{/snippet}
</ResourcePageLayout>
