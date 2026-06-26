<script lang="ts">
	import { onMount } from 'svelte';
	import { openConfirmDialog } from './confirm-dialog';
	import { goto, invalidateAll } from '$app/navigation';
	import { toast } from 'svelte-sonner';
	import { tryCatch } from '$lib/utils/api';
	import { handleApiResultWithCallbacks } from '$lib/utils/api';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import DeploySplitButton from '$lib/components/deploy-split-button/deploy-split-button.svelte';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { m } from '$lib/paraglide/messages';
	import settingsStore from '$lib/stores/config-store';
	import { deployOptionsStore } from '$lib/stores/deploy-options.store.svelte';
	import { containerService, type ContainerDetailsResponse } from '$lib/services/container-service';
	import { projectService, type DeployProjectOptions } from '$lib/services/project-service';
	import type { Project } from '$lib/types/swarm';
	import { EllipsisIcon } from '$lib/icons';
	import { createMutation } from '@tanstack/svelte-query';
	import { hasPermission } from '$lib/utils/auth';
	import { isDepotBuildAvailable } from '$lib/utils/build-provider';
	import { environmentStore } from '$lib/stores/environment.store.svelte';

	type TargetType = 'container' | 'project';
	type LoadingStates = {
		start?: boolean;
		stop?: boolean;
		restart?: boolean;
		pull?: boolean;
		deploy?: boolean;
		redeploy?: boolean;
		build?: boolean;
		remove?: boolean;
		validating?: boolean;
		refresh?: boolean;
	};

	let {
		id,
		type = 'container',
		itemState = 'stopped',
		desktopVariant = 'labels',
		loading = $bindable<LoadingStates>({}),
		onActionComplete = $bindable<(status?: string) => void>(() => {}),
		startLoading = $bindable(false),
		stopLoading = $bindable(false),
		restartLoading = $bindable(false),
		removeLoading = $bindable(false),
		redeployLoading = $bindable(false),
		refreshLoading = $bindable(false),
		hasBuildDirective = false,
		disableRedeploy = false,
		onRefresh
	}: {
		id: string;
		name?: string;
		type?: TargetType;
		itemState?: string;
		desktopVariant?: 'labels' | 'adaptive';
		loading?: LoadingStates;
		onActionComplete?: (status?: string) => void;
		startLoading?: boolean;
		stopLoading?: boolean;
		restartLoading?: boolean;
		removeLoading?: boolean;
		redeployLoading?: boolean;
		refreshLoading?: boolean;
		hasBuildDirective?: boolean;
		disableRedeploy?: boolean;
		onRefresh?: () => void | Promise<void>;
	} = $props();

	let isLoading = $state<LoadingStates>({
		start: false,
		stop: false,
		restart: false,
		remove: false,
		pull: false,
		build: false,
		redeploy: false,
		validating: false,
		refresh: false
	});

	function setLoading<K extends keyof LoadingStates>(key: K, value: boolean) {
		isLoading[key] = value;
		loading = { ...loading, [key]: value };

		if (key === 'start') startLoading = value;
		if (key === 'stop') stopLoading = value;
		if (key === 'restart') restartLoading = value;
		if (key === 'remove') removeLoading = value;
		if (key === 'redeploy') redeployLoading = value;
		if (key === 'refresh') refreshLoading = value;
	}

	function handleDeployPullPolicyChange(value: string) {
		if (value === 'missing' || value === 'always' || value === 'never') {
			deployOptionsStore.setPullPolicy(value);
		}
	}

	const uiLoading = $derived({
		start: !!(isLoading.start || loading?.start || startLoading),
		stop: !!(isLoading.stop || loading?.stop || stopLoading),
		restart: !!(isLoading.restart || loading?.restart || restartLoading),
		remove: !!(isLoading.remove || loading?.remove || removeLoading),
		pull: !!(isLoading.pull || loading?.pull),
		build: !!(isLoading.build || loading?.build),
		redeploy: !!(isLoading.redeploy || loading?.redeploy || redeployLoading),
		validating: !!(isLoading.validating || loading?.validating),
		refresh: !!(isLoading.refresh || loading?.refresh || refreshLoading)
	});

	const startMutation = createMutation(() => ({
		mutationKey: ['action', 'start', type, id],
		mutationFn: () =>
			tryCatch(
				type === 'container'
					? containerService.startContainer(id)
					: projectService.deployProject(id, deployOptionsStore.getRequestOptions())
			),
		onMutate: () => setLoading('start', true),
		onSettled: () => setLoading('start', false)
	}));

	const stopMutation = createMutation(() => ({
		mutationKey: ['action', 'stop', type, id],
		mutationFn: () => tryCatch(type === 'container' ? containerService.stopContainer(id) : projectService.downProject(id)),
		onMutate: () => setLoading('stop', true),
		onSettled: () => setLoading('stop', false)
	}));

	const restartMutation = createMutation(() => ({
		mutationKey: ['action', 'restart', type, id],
		mutationFn: () => tryCatch(type === 'container' ? containerService.restartContainer(id) : projectService.restartProject(id)),
		onMutate: () => setLoading('restart', true),
		onSettled: () => setLoading('restart', false)
	}));

	const redeployMutation = createMutation(() => ({
		mutationKey: ['action', 'redeploy', type, id],
		mutationFn: () =>
			tryCatch(
				(type === 'container' ? containerService.redeployContainer(id) : projectService.redeployProject(id)) as Promise<
					ContainerDetailsResponse | Project
				>
			),
		onMutate: () => setLoading('redeploy', true),
		onSettled: () => setLoading('redeploy', false)
	}));

	const removeMutation = createMutation(() => ({
		mutationKey: ['action', 'remove', type, id],
		mutationFn: ({ removeVolumes }: { removeVolumes: boolean }) =>
			tryCatch(
				type === 'container'
					? containerService.deleteContainer(id, { volumes: removeVolumes })
					: projectService.destroyProject(id, removeVolumes)
			),
		onMutate: () => setLoading('remove', true),
		onSettled: () => setLoading('remove', false)
	}));

	const refreshMutation = createMutation(() => ({
		mutationKey: ['action', 'refresh', id],
		mutationFn: () => tryCatch(Promise.resolve(onRefresh?.())),
		onMutate: () => setLoading('refresh', true),
		onSettled: () => setLoading('refresh', false)
	}));

	const isRunning = $derived(itemState === 'running' || (type === 'project' && itemState === 'partially running'));
	const projectHasBuildDirective = $derived(type === 'project' && hasBuildDirective);

	// Per-action RBAC gating. Each button hides if the caller lacks the
	// corresponding permission on the currently-selected environment. Project
	// pull / build / redeploy all share the `projects:deploy` permission since
	// they're stages of the deploy flow.
	const currentEnvId = $derived(environmentStore.selected?.id);
	const canStart = $derived(
		type === 'container' ? hasPermission('containers:start', currentEnvId) : hasPermission('projects:deploy', currentEnvId)
	);
	const canStop = $derived(
		type === 'container' ? hasPermission('containers:stop', currentEnvId) : hasPermission('projects:down', currentEnvId)
	);
	const canRestart = $derived(
		type === 'container' ? hasPermission('containers:restart', currentEnvId) : hasPermission('projects:restart', currentEnvId)
	);
	const canRedeploy = $derived(
		type === 'container' ? hasPermission('containers:redeploy', currentEnvId) : hasPermission('projects:deploy', currentEnvId)
	);
	const canRemove = $derived(
		type === 'container' ? hasPermission('containers:delete', currentEnvId) : hasPermission('projects:delete', currentEnvId)
	);
	const canPull = $derived(type === 'project' && hasPermission('projects:deploy', currentEnvId));
	const canBuild = $derived(type === 'project' && hasPermission('projects:deploy', currentEnvId));
	const deployButtonLabel = $derived(projectHasBuildDirective ? m.compose_build_and_deploy() : m.common_up());
	const depotAvailable = $derived(isDepotBuildAvailable($settingsStore));
	const projectBuildProvider = $derived.by<'local' | 'depot'>(() => {
		const configuredProvider = ($settingsStore?.buildProvider as 'local' | 'depot') ?? 'local';
		if (configuredProvider === 'depot' && !depotAvailable) {
			return 'local';
		}
		return configuredProvider;
	});

	// Tailwind xl breakpoint is 1280px. We use this to avoid mounting two desktop variants at once
	// (which would duplicate portaled popovers when the same `open` state is bound twice).
	let isXlUp = $state(true);
	let isLgUp = $state(true);
	const adaptiveIconOnly = $derived(!isXlUp);

	onMount(() => {
		const mqlXl = window.matchMedia('(min-width: 1280px)');
		const mqlLg = window.matchMedia('(min-width: 1024px)');

		const update = () => {
			isXlUp = mqlXl.matches;
			isLgUp = mqlLg.matches;
		};

		update();

		mqlXl.addEventListener('change', update);
		mqlLg.addEventListener('change', update);
		return () => {
			mqlXl.removeEventListener('change', update);
			mqlLg.removeEventListener('change', update);
		};
	});

	async function handleRefresh() {
		if (!onRefresh) return;
		await refreshMutation.mutateAsync();
	}

	function confirmAction(action: string) {
		if (action === 'remove') {
			openConfirmDialog({
				title: type === 'project' ? m.compose_destroy() : m.common_confirm_removal_title(),
				message:
					type === 'project'
						? m.common_confirm_destroy_message({ type: m.project() })
						: m.common_confirm_removal_message({ type: m.container() }),
				confirm: {
					label: type === 'project' ? m.compose_destroy() : m.common_remove(),
					destructive: true,
					action: async (checkboxStates) => {
						const removeVolumes = checkboxStates['removeVolumes'] === true;

						const result = await removeMutation.mutateAsync({ removeVolumes });
						handleApiResultWithCallbacks({
							result,
							message: m.common_action_failed_with_type({
								action: type === 'project' ? m.compose_destroy() : m.common_remove(),
								type: type
							}),
							onSuccess: async () => {
								await invalidateAll();
								goto(type === 'project' ? '/projects' : '/containers');
							}
						});
					}
				},
				checkboxes: [
					{
						id: 'removeVolumes',
						label: m.confirm_remove_volumes_warning(),
						initialState: false
					}
				]
			});
		} else if (action === 'redeploy') {
			openConfirmDialog({
				title: type === 'container' ? m.container_confirm_redeploy_title() : m.common_confirm_redeploy_title(),
				message: type === 'container' ? m.container_confirm_redeploy_message() : m.common_confirm_redeploy_message(),
				confirm: {
					label: m.common_redeploy(),
					action: async () => {
						const result = await redeployMutation.mutateAsync();
						handleApiResultWithCallbacks({
							result,
							message: m.common_action_failed_with_type({ action: m.common_redeploy(), type }),
							onSuccess: async (data) => {
								const containerData = data as ContainerDetailsResponse;
								if (type === 'container' && containerData?.data?.id) {
									goto(`/containers/${containerData.data.id}`);
								} else if (type === 'container') {
									goto('/containers');
								} else {
									onActionComplete('running');
								}
							}
						});
					}
				}
			});
		}
	}

	async function handleStart() {
		const result = await startMutation.mutateAsync();
		await handleApiResultWithCallbacks({
			result,
			message: m.common_action_failed_with_type({ action: m.common_start(), type }),
			onSuccess: async () => {
				itemState = 'running';
				onActionComplete('running');
			}
		});
	}

	async function handleDeploy(options?: DeployProjectOptions) {
		setLoading('start', true);

		try {
			await projectService.deployProject(id, () => {}, options ?? deployOptionsStore.getRequestOptions());
			await invalidateAll();
			itemState = 'running';
			onActionComplete('running');
		} catch (e: any) {
			const message = e?.message || m.common_action_failed_with_type({ action: m.common_start(), type });
			toast.error(message);
		} finally {
			setLoading('start', false);
		}
	}

	async function handleStop() {
		const result = await stopMutation.mutateAsync();
		await handleApiResultWithCallbacks({
			result,
			message: m.common_action_failed_with_type({ action: m.common_stop(), type }),
			onSuccess: async () => {
				itemState = 'stopped';
				onActionComplete('stopped');
			}
		});
	}

	async function handleRestart() {
		const result = await restartMutation.mutateAsync();
		await handleApiResultWithCallbacks({
			result,
			message: m.common_action_failed_with_type({ action: m.common_restart(), type }),
			onSuccess: async () => {
				itemState = 'running';
				onActionComplete('running');
			}
		});
	}

	async function handleProjectPull() {
		setLoading('pull', true);

		try {
			projectService
				.pullProjectImages(id, () => {})
				.then(async () => {
					await invalidateAll();
					onActionComplete(itemState);
				})
				.catch((error: any) => {
					const message = error?.message || m.images_pull_failed();
					toast.error(message);
				});
		} finally {
			setLoading('pull', false);
		}
	}

	async function handleProjectBuild() {
		setLoading('build', true);

		try {
			const buildProvider = projectBuildProvider;
			projectService
				.buildProjectImages(
					id,
					{
						provider: buildProvider,
						push: buildProvider === 'depot',
						load: buildProvider !== 'depot'
					},
					() => {}
				)
				.then(async () => {
					await invalidateAll();
				})
				.catch((error: any) => {
					const message = error?.message || m.build_failed();
					toast.error(message);
				});
		} finally {
			setLoading('build', false);
		}
	}
</script>

{#snippet RedeployActionButton(size: 'default' | 'icon' = 'default', showLabel = true)}
	{#if canRedeploy}
		{#if disableRedeploy}
			<span class="inline-flex" title={m.common_redeploy_disabled_arcane_self()}>
				<ArcaneButton action="redeploy" {size} {showLabel} disabled />
			</span>
		{:else}
			<ArcaneButton action="redeploy" {size} {showLabel} onclick={() => confirmAction('redeploy')} loading={uiLoading.redeploy} />
		{/if}
	{/if}
{/snippet}

{#snippet RedeployMenuItem()}
	{#if canRedeploy}
		{#if disableRedeploy}
			<DropdownMenu.Item disabled title={m.common_redeploy_disabled_arcane_self()}>
				{m.common_redeploy()}
			</DropdownMenu.Item>
		{:else}
			<DropdownMenu.Item onclick={() => confirmAction('redeploy')} disabled={uiLoading.redeploy}>
				{m.common_redeploy()}
			</DropdownMenu.Item>
		{/if}
	{/if}
{/snippet}

{#if desktopVariant === 'adaptive'}
	<div>
		<!-- On xl+ show labels; below xl use icon-only to avoid overflow in constrained headers (sidebar layouts) -->
		{#if isLgUp}
			<div class="flex items-center gap-2">
				{#if !isRunning && canStart}
					{#if type === 'container'}
						<ArcaneButton
							action="start"
							size={adaptiveIconOnly ? 'icon' : 'default'}
							showLabel={!adaptiveIconOnly}
							onclick={() => handleStart()}
							loading={uiLoading.start}
						/>
					{:else}
						<DeploySplitButton
							size={adaptiveIconOnly ? 'icon' : 'default'}
							showLabel={!adaptiveIconOnly}
							customLabel={deployButtonLabel}
							onDeploy={() => handleDeploy()}
							loading={uiLoading.start}
						/>
					{/if}
				{/if}

				{#if isRunning}
					{#if canStop}
						<ArcaneButton
							action="stop"
							size={adaptiveIconOnly ? 'icon' : 'default'}
							showLabel={!adaptiveIconOnly}
							customLabel={type === 'project' ? m.common_down() : undefined}
							onclick={() => handleStop()}
							loading={uiLoading.stop}
						/>
					{/if}
					{#if canRestart}
						<ArcaneButton
							action="restart"
							size={adaptiveIconOnly ? 'icon' : 'default'}
							showLabel={!adaptiveIconOnly}
							onclick={() => handleRestart()}
							loading={uiLoading.restart}
						/>
					{/if}
				{/if}

				{#if type === 'container'}
					{@render RedeployActionButton(adaptiveIconOnly ? 'icon' : 'default', !adaptiveIconOnly)}
					{#if canRemove}
						<ArcaneButton
							action="remove"
							size={adaptiveIconOnly ? 'icon' : 'default'}
							showLabel={!adaptiveIconOnly}
							onclick={() => confirmAction('remove')}
							loading={uiLoading.remove}
						/>
					{/if}
				{:else}
					{@render RedeployActionButton(adaptiveIconOnly ? 'icon' : 'default', !adaptiveIconOnly)}

					{#if type === 'project'}
						{#if projectHasBuildDirective && canBuild}
							<ArcaneButton
								action="build"
								size={adaptiveIconOnly ? 'icon' : 'default'}
								showLabel={!adaptiveIconOnly}
								onclick={() => handleProjectBuild()}
								loading={uiLoading.build}
							/>
						{/if}

						{#if canPull}
							<ArcaneButton
								action="pull"
								size={adaptiveIconOnly ? 'icon' : 'default'}
								showLabel={!adaptiveIconOnly}
								onclick={() => handleProjectPull()}
								loading={uiLoading.pull}
							/>
						{/if}
					{/if}

					{#if onRefresh}
						<ArcaneButton
							action="refresh"
							size={adaptiveIconOnly ? 'icon' : 'default'}
							showLabel={!adaptiveIconOnly}
							onclick={() => handleRefresh()}
							loading={uiLoading.refresh}
						/>
					{/if}

					{#if canRemove}
						<ArcaneButton
							customLabel={type === 'project' ? m.compose_destroy() : m.common_remove()}
							action="remove"
							size={adaptiveIconOnly ? 'icon' : 'default'}
							showLabel={!adaptiveIconOnly}
							onclick={() => confirmAction('remove')}
							loading={uiLoading.remove}
						/>
					{/if}
				{/if}
			</div>
		{:else}
			<div class="flex items-center">
				<DropdownMenu.Root>
					<DropdownMenu.Trigger class="bg-background/70 inline-flex size-9 items-center justify-center rounded-lg border">
						<span class="sr-only">{m.common_open_menu()}</span>
						<EllipsisIcon />
					</DropdownMenu.Trigger>

					<DropdownMenu.Content
						align="end"
						class="bg-popover/20 z-[var(--arcane-z-surface)] min-w-[180px] rounded-xl border p-1 shadow-lg backdrop-blur-md"
					>
						<DropdownMenu.Group>
							{#if !isRunning && canStart}
								{#if type === 'container'}
									<DropdownMenu.Item onclick={handleStart} disabled={uiLoading.start}>
										{m.common_start()}
									</DropdownMenu.Item>
								{:else}
									<DropdownMenu.Item onclick={() => handleDeploy()} disabled={uiLoading.start}>
										{deployButtonLabel}
									</DropdownMenu.Item>
									{#if type === 'project'}
										<DropdownMenu.Separator />
										<DropdownMenu.Label>{m.settings_default_deploy_pull_policy()}</DropdownMenu.Label>
										<DropdownMenu.RadioGroup value={deployOptionsStore.pullPolicy} onValueChange={handleDeployPullPolicyChange}>
											<DropdownMenu.RadioItem value="missing">Missing</DropdownMenu.RadioItem>
											<DropdownMenu.RadioItem value="always">
												{m.common_always()}
											</DropdownMenu.RadioItem>
											<DropdownMenu.RadioItem value="never">
												{m.common_never()}
											</DropdownMenu.RadioItem>
										</DropdownMenu.RadioGroup>
										<DropdownMenu.Separator />
										<DropdownMenu.CheckboxItem
											checked={deployOptionsStore.forceRecreate}
											onCheckedChange={(checked) => deployOptionsStore.setForceRecreate(checked === true)}
										>
											{m.deploy_force_recreate()}
										</DropdownMenu.CheckboxItem>
									{/if}
								{/if}
							{:else if isRunning}
								{#if canStop}
									<DropdownMenu.Item onclick={handleStop} disabled={uiLoading.stop}>
										{type === 'project' ? m.common_down() : m.common_stop()}
									</DropdownMenu.Item>
								{/if}
								{#if canRestart}
									<DropdownMenu.Item onclick={handleRestart} disabled={uiLoading.restart}>
										{m.common_restart()}
									</DropdownMenu.Item>
								{/if}
							{/if}

							{#if type === 'container'}
								{@render RedeployMenuItem()}
								{#if canRemove}
									<DropdownMenu.Item onclick={() => confirmAction('remove')} disabled={uiLoading.remove}>
										{m.common_remove()}
									</DropdownMenu.Item>
								{/if}
							{:else}
								{@render RedeployMenuItem()}

								{#if type === 'project'}
									{#if projectHasBuildDirective && canBuild}
										<DropdownMenu.Item onclick={handleProjectBuild} disabled={uiLoading.build}>
											{m.build()}
										</DropdownMenu.Item>
									{/if}
									{#if canPull}
										<DropdownMenu.Item onclick={handleProjectPull} disabled={uiLoading.pull}>
											{m.images_pull()}
										</DropdownMenu.Item>
									{/if}
								{/if}

								{#if onRefresh}
									<DropdownMenu.Item onclick={handleRefresh} disabled={uiLoading.refresh}>
										{m.common_refresh()}
									</DropdownMenu.Item>
								{/if}

								{#if canRemove}
									<DropdownMenu.Item onclick={() => confirmAction('remove')} disabled={uiLoading.remove}>
										{type === 'project' ? m.compose_destroy() : m.common_remove()}
									</DropdownMenu.Item>
								{/if}
							{/if}
						</DropdownMenu.Group>
					</DropdownMenu.Content>
				</DropdownMenu.Root>
			</div>
		{/if}
	</div>
{:else}
	<div>
		<div class="hidden items-center gap-2 lg:flex">
			{#if !isRunning && canStart}
				{#if type === 'container'}
					<ArcaneButton action="start" onclick={() => handleStart()} loading={uiLoading.start} />
				{:else}
					<DeploySplitButton customLabel={deployButtonLabel} onDeploy={() => handleDeploy()} loading={uiLoading.start} />
				{/if}
			{/if}

			{#if isRunning}
				{#if canStop}
					<ArcaneButton
						action="stop"
						customLabel={type === 'project' ? m.common_down() : undefined}
						onclick={() => handleStop()}
						loading={uiLoading.stop}
					/>
				{/if}
				{#if canRestart}
					<ArcaneButton action="restart" onclick={() => handleRestart()} loading={uiLoading.restart} />
				{/if}
			{/if}

			{#if type === 'container'}
				{@render RedeployActionButton()}
				{#if canRemove}
					<ArcaneButton action="remove" onclick={() => confirmAction('remove')} loading={uiLoading.remove} />
				{/if}
			{:else}
				{@render RedeployActionButton()}

				{#if type === 'project'}
					{#if projectHasBuildDirective && canBuild}
						<ArcaneButton action="build" onclick={() => handleProjectBuild()} loading={uiLoading.build} />
					{/if}

					{#if canPull}
						<ArcaneButton action="pull" onclick={() => handleProjectPull()} loading={uiLoading.pull} />
					{/if}
				{/if}

				{#if onRefresh}
					<ArcaneButton action="refresh" onclick={() => handleRefresh()} loading={uiLoading.refresh} />
				{/if}

				{#if canRemove}
					<ArcaneButton
						customLabel={type === 'project' ? m.compose_destroy() : m.common_remove()}
						action="remove"
						onclick={() => confirmAction('remove')}
						loading={uiLoading.remove}
					/>
				{/if}
			{/if}
		</div>

		<div class="flex items-center lg:hidden">
			<DropdownMenu.Root>
				<DropdownMenu.Trigger class="bg-background/70 inline-flex size-9 items-center justify-center rounded-lg border">
					<span class="sr-only">{m.common_open_menu()}</span>
					<EllipsisIcon />
				</DropdownMenu.Trigger>

				<DropdownMenu.Content
					align="end"
					class="bg-popover/20 z-[var(--arcane-z-surface)] min-w-[180px] rounded-xl border p-1 shadow-lg backdrop-blur-md"
				>
					<DropdownMenu.Group>
						{#if !isRunning && canStart}
							{#if type === 'container'}
								<DropdownMenu.Item onclick={handleStart} disabled={uiLoading.start}>
									{m.common_start()}
								</DropdownMenu.Item>
							{:else}
								<DropdownMenu.Item onclick={() => handleDeploy()} disabled={uiLoading.start}>
									{deployButtonLabel}
								</DropdownMenu.Item>
								{#if type === 'project'}
									<DropdownMenu.Separator />
									<DropdownMenu.Label>{m.settings_default_deploy_pull_policy()}</DropdownMenu.Label>
									<DropdownMenu.RadioGroup value={deployOptionsStore.pullPolicy} onValueChange={handleDeployPullPolicyChange}>
										<DropdownMenu.RadioItem value="missing">Missing</DropdownMenu.RadioItem>
										<DropdownMenu.RadioItem value="always">
											{m.common_always()}
										</DropdownMenu.RadioItem>
										<DropdownMenu.RadioItem value="never">
											{m.common_never()}
										</DropdownMenu.RadioItem>
									</DropdownMenu.RadioGroup>
									<DropdownMenu.Separator />
									<DropdownMenu.CheckboxItem
										checked={deployOptionsStore.forceRecreate}
										onCheckedChange={(checked) => deployOptionsStore.setForceRecreate(checked === true)}
									>
										{m.deploy_force_recreate()}
									</DropdownMenu.CheckboxItem>
								{/if}
							{/if}
						{:else if isRunning}
							{#if canStop}
								<DropdownMenu.Item onclick={handleStop} disabled={uiLoading.stop}>
									{type === 'project' ? m.common_down() : m.common_stop()}
								</DropdownMenu.Item>
							{/if}
							{#if canRestart}
								<DropdownMenu.Item onclick={handleRestart} disabled={uiLoading.restart}>
									{m.common_restart()}
								</DropdownMenu.Item>
							{/if}
						{/if}

						{#if type === 'container'}
							{@render RedeployMenuItem()}
							{#if canRemove}
								<DropdownMenu.Item onclick={() => confirmAction('remove')} disabled={uiLoading.remove}>
									{m.common_remove()}
								</DropdownMenu.Item>
							{/if}
						{:else}
							{@render RedeployMenuItem()}

							{#if type === 'project'}
								{#if projectHasBuildDirective && canBuild}
									<DropdownMenu.Item onclick={handleProjectBuild} disabled={uiLoading.build}>
										{m.build()}
									</DropdownMenu.Item>
								{/if}
								{#if canPull}
									<DropdownMenu.Item onclick={handleProjectPull} disabled={uiLoading.pull}>
										{m.images_pull()}
									</DropdownMenu.Item>
								{/if}
							{/if}

							{#if onRefresh}
								<DropdownMenu.Item onclick={handleRefresh} disabled={uiLoading.refresh}>
									{m.common_refresh()}
								</DropdownMenu.Item>
							{/if}

							{#if canRemove}
								<DropdownMenu.Item onclick={() => confirmAction('remove')} disabled={uiLoading.remove}>
									{type === 'project' ? m.compose_destroy() : m.common_remove()}
								</DropdownMenu.Item>
							{/if}
						{/if}
					</DropdownMenu.Group>
				</DropdownMenu.Content>
			</DropdownMenu.Root>
		</div>
	</div>
{/if}
