<script lang="ts">
	import { onMount } from 'svelte';
	import { openConfirmDialog } from './confirm-dialog';
	import { goto, invalidateAll } from '$app/navigation';
	import { toast } from 'svelte-sonner';
	import { tryCatch } from '$lib/utils/try-catch';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import ProgressPopover from '$lib/components/progress-popover.svelte';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import * as ContextMenu from '$lib/components/ui/context-menu/index.js';
	import { m } from '$lib/paraglide/messages';
	import { containerService } from '$lib/services/container-service';
	import { projectService } from '$lib/services/project-service';
	import settingsStore from '$lib/stores/config-store';
	import { settingsService } from '$lib/services/settings-service';
	import type { ProjectUpOptions } from '$lib/types/project.type';
	import { isDownloadingLine, calculateOverallProgress, areAllLayersComplete } from '$lib/utils/pull-progress';
	import { EllipsisIcon, DownloadIcon } from '$lib/icons';
	import { createMutation } from '@tanstack/svelte-query';

	type TargetType = 'container' | 'project';
	type LoadingStates = {
		start?: boolean;
		stop?: boolean;
		restart?: boolean;
		pull?: boolean;
		deploy?: boolean;
		redeploy?: boolean;
		remove?: boolean;
		validating?: boolean;
		refresh?: boolean;
	};

	type ProjectUpPreferences = {
		pullPolicy: NonNullable<ProjectUpOptions['pullPolicy']>;
		forceRecreate: boolean;
	};

	let {
		id,
		name,
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
		runningCount,
		serviceCount,
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
		runningCount?: number | string;
		serviceCount?: number | string;
		onRefresh?: () => void | Promise<void>;
	} = $props();

	let isLoading = $state<LoadingStates>({
		start: false,
		stop: false,
		restart: false,
		remove: false,
		pull: false,
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

	const uiLoading = $derived({
		start: !!(isLoading.start || loading?.start || startLoading),
		stop: !!(isLoading.stop || loading?.stop || stopLoading),
		restart: !!(isLoading.restart || loading?.restart || restartLoading),
		remove: !!(isLoading.remove || loading?.remove || removeLoading),
		pulling: !!(isLoading.pull || loading?.pull),
		redeploy: !!(isLoading.redeploy || loading?.redeploy || redeployLoading),
		validating: !!(isLoading.validating || loading?.validating),
		refresh: !!(isLoading.refresh || loading?.refresh || refreshLoading)
	});

	const startMutation = createMutation(() => ({
		mutationKey: ['action', 'start', type, id],
		mutationFn: () => tryCatch(type === 'container' ? containerService.startContainer(id) : projectService.deployProject(id)),
		onMutate: () => setLoading('start', true),
		onSettled: () => {
			if (!deployPulling) {
				setLoading('start', false);
			}
		}
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
		mutationKey: ['action', 'redeploy', id],
		mutationFn: () => tryCatch(projectService.redeployProject(id)),
		onMutate: () => setLoading('redeploy', true),
		onSettled: () => setLoading('redeploy', false)
	}));

	const removeMutation = createMutation(() => ({
		mutationKey: ['action', 'remove', type, id],
		mutationFn: ({ removeFiles, removeVolumes }: { removeFiles: boolean; removeVolumes: boolean }) =>
			tryCatch(
				type === 'container'
					? containerService.deleteContainer(id, { volumes: removeVolumes })
					: projectService.destroyProject(id, removeVolumes, removeFiles)
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

	let pullPopoverOpen = $state(false);
	let deployPullPopoverOpen = $state(false);
	let projectPulling = $state(false); // only for Project Pull button/popover
	let deployPulling = $state(false); // only for Deploy popover
	let pullProgress = $state(0);
	let pullStatusText = $state('');
	let pullError = $state('');
	let layerProgress = $state<Record<string, { current: number; total: number; status: string }>>({});
	let deployServiceProgress = $state<Record<string, { phase: string; health?: string; state?: string; status?: string }>>({});
	let deployLastNonWaitingStatus = $state('');
	let upContextMenuOpen = $state(false);
	let upContextPullLatest = $state(false);
	let upContextForceRecreate = $state(false);

	const projectRunningCount = $derived(type === 'project' ? Number(runningCount ?? 0) || 0 : 0);
	const projectServiceCount = $derived(type === 'project' ? Number(serviceCount ?? 0) || 0 : 0);
	const isContainerRunning = $derived(type === 'container' && itemState === 'running');
	const showContainerStart = $derived(type === 'container' && !isContainerRunning);
	const showContainerRunningActions = $derived(type === 'container' && isContainerRunning);
	const showProjectUp = $derived(type === 'project');
	const showProjectDown = $derived(type === 'project' && projectServiceCount > 0);
	const showProjectRestart = $derived(type === 'project' && projectRunningCount > 0);

	const containerStartTooltip = $derived(`${m.common_start()} ${m.container()}.`);
	const containerStopTooltip = $derived(`${m.common_stop()} ${m.container()}.`);
	const containerRestartTooltip = $derived(`${m.common_restart()} ${m.container()}.`);
	const containerRemoveTooltip = $derived(m.common_confirm_removal_message({ type: m.container() }));

	const projectUpPreferences = $derived.by(
		(): ProjectUpPreferences => ({
			pullPolicy: $settingsStore?.projectUpDefaultPullPolicy === 'always' ? 'always' : 'missing',
			forceRecreate: $settingsStore?.projectUpDefaultForceRecreate === true
		})
	);

	const defaultUpOptions = $derived.by(
		(): ProjectUpOptions => ({
			pullPolicy: projectUpPreferences.pullPolicy,
			forceRecreate: projectUpPreferences.forceRecreate
		})
	);

	async function updateProjectUpPreferences(next: Partial<ProjectUpPreferences>) {
		const merged: ProjectUpPreferences = {
			...projectUpPreferences,
			...next
		};

		const updated = await settingsService.updateSettings({
			projectUpDefaultPullPolicy: merged.pullPolicy,
			projectUpDefaultForceRecreate: merged.forceRecreate
		});

		settingsStore.set(updated);
	}

	function syncUpContextOptions() {
		upContextPullLatest = defaultUpOptions.pullPolicy === 'always';
		upContextForceRecreate = defaultUpOptions.forceRecreate === true;
	}

	function getContextUpOptions(): ProjectUpOptions {
		return {
			pullPolicy: upContextPullLatest ? 'always' : 'missing',
			forceRecreate: upContextForceRecreate
		};
	}

	function toggleContextCheckbox(event: Event, option: 'pullLatest' | 'forceRecreate') {
		event.preventDefault();
		if (option === 'pullLatest') {
			upContextPullLatest = !upContextPullLatest;
			return;
		}

		upContextForceRecreate = !upContextForceRecreate;
	}

	async function handleDeployWithContextOptions() {
		await handleDeploy(getContextUpOptions());
	}

	async function handleSaveContextUpDefaults() {
		try {
			await updateProjectUpPreferences(getContextUpOptions());
			toast.success(m.common_update_success({ resource: m.resource_environment() }));
		} catch {
			toast.error(m.common_save_failed());
		}
	}

	const projectUpTooltip = $derived.by(() => {
		return `Right-click for one-time Up options.`;
	});
	const projectDownTooltip = $derived(`${m.common_down()} ${m.project()}.`);
	const projectRestartTooltip = $derived(`${m.common_restart()} ${m.project()}.`);
	const projectRedeployTooltip = $derived(m.common_confirm_redeploy_message());
	const projectPullTooltip = $derived(m.images_pull_description());
	const projectRefreshTooltip = $derived(`${m.common_refresh()} ${m.project()}.`);
	const projectDestroyTooltip = $derived(m.common_confirm_destroy_message({ type: m.project() }));

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

		if ('addEventListener' in mqlXl) {
			mqlXl.addEventListener('change', update);
			mqlLg.addEventListener('change', update);
			return () => {
				mqlXl.removeEventListener('change', update);
				mqlLg.removeEventListener('change', update);
			};
		}

		// @ts-expect-error legacy MediaQueryList API
		mqlXl.addListener(update);
		mqlLg.addListener(update);
		return () => {
			// @ts-expect-error legacy MediaQueryList API
			mqlXl.removeListener(update);
			mqlLg.removeListener(update);
		};
	});

	function resetPullState() {
		pullProgress = 0;
		pullStatusText = '';
		pullError = '';
		layerProgress = {};
		deployServiceProgress = {};
		deployLastNonWaitingStatus = '';
	}

	function deriveDeployStatusText(): string {
		const entries = Object.entries(deployServiceProgress);
		if (entries.length === 0) return m.progress_deploy_starting();

		const waiting = entries.filter(([_, v]) => v.phase === 'service_waiting_healthy').sort(([a], [b]) => a.localeCompare(b));
		if (waiting.length > 0) {
			const [service, v] = waiting[0];
			const health = (v.health ?? '').trim();
			return health
				? m.progress_deploy_waiting_for_service_with_health({ service, health })
				: m.progress_deploy_waiting_for_service({ service });
		}

		const stateIssues = entries
			.filter(([_, v]) => v.phase === 'service_state' && (v.state ?? '').toLowerCase() !== 'running')
			.sort(([a], [b]) => a.localeCompare(b));
		if (stateIssues.length > 0) {
			const [service, v] = stateIssues[0];
			return m.progress_deploy_service_state({ service, state: String(v.state ?? '') });
		}

		return deployLastNonWaitingStatus || m.progress_deploy_starting();
	}

	function updatePullProgress() {
		pullProgress = calculateOverallProgress(layerProgress);
	}

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
						const removeFiles = checkboxStates['removeFiles'] === true;
						const removeVolumes = checkboxStates['removeVolumes'] === true;

						const result = await removeMutation.mutateAsync({ removeFiles, removeVolumes });
						handleApiResultWithCallbacks({
							result,
							message: m.common_action_failed_with_type({
								action: type === 'project' ? m.compose_destroy() : m.common_remove(),
								type: type
							}),
							onSuccess: async () => {
								toast.success(
									type === 'project'
										? m.common_destroyed_success({ type: m.project() })
										: m.common_removed_success({ type: m.container() })
								);
								await invalidateAll();
								goto(type === 'project' ? '/projects' : '/containers');
							}
						});
					}
				},
				checkboxes: [
					{ id: 'removeFiles', label: m.confirm_remove_project_files(), initialState: false },
					{
						id: 'removeVolumes',
						label: m.confirm_remove_volumes_warning(),
						initialState: false
					}
				]
			});
		} else if (action === 'redeploy') {
			openConfirmDialog({
				title: m.common_confirm_redeploy_title(),
				message: m.common_confirm_redeploy_message(),
				confirm: {
					label: m.common_redeploy(),
					action: async () => {
						const result = await redeployMutation.mutateAsync();
						handleApiResultWithCallbacks({
							result,
							message: m.common_action_failed_with_type({ action: m.common_redeploy(), type }),
							onSuccess: async () => {
								toast.success(m.common_redeploy_success({ type: name || type }));
								onActionComplete('running');
							}
						});
					}
				}
			});
		}
	}

	async function handleProjectUpClick() {
		await handleDeploy(defaultUpOptions);
	}

	async function handleStart() {
		const result = await startMutation.mutateAsync();
		await handleApiResultWithCallbacks({
			result,
			message: m.common_action_failed_with_type({ action: m.common_start(), type }),
			onSuccess: async () => {
				itemState = 'running';
				toast.success(m.common_started_success({ type: name || type }));
				onActionComplete('running');
			}
		});
	}

	async function handleDeploy(options: ProjectUpOptions = { pullPolicy: 'missing', forceRecreate: false }) {
		resetPullState();
		setLoading('start', true);
		let openedPopover = false;
		let hadError = false;
		let deployPhaseStarted = false;

		// Always open the popover for deploy so we can show health-wait status even
		// when there is nothing to pull.
		deployPullPopoverOpen = true;
		deployPulling = true;
		pullStatusText = m.progress_deploy_starting();
		openedPopover = true;

		try {
			await projectService.deployProject(id, options, (streamData) => {
				if (!streamData) return;

				if (streamData.error) {
					const errMsg =
						typeof streamData.error === 'string' ? streamData.error : streamData.error.message || m.progress_deploy_failed();
					pullError = errMsg;
					pullStatusText = m.progress_deploy_failed_with_error({ error: errMsg });
					hadError = true;
					deployPulling = false;
					return;
				}

				if (streamData.type === 'deploy') {
					if (!deployPhaseStarted) {
						deployPhaseStarted = true;
						pullProgress = 0;
						layerProgress = {};
						pullError = '';
						deployServiceProgress = {};
						deployLastNonWaitingStatus = '';
					}

					deployPulling = true;
					switch (streamData.phase) {
						case 'begin':
							pullStatusText = m.progress_deploy_starting();
							break;
						case 'service_waiting_healthy': {
							const service = String(streamData.service ?? '').trim();
							if (service) {
								deployServiceProgress[service] = {
									phase: 'service_waiting_healthy',
									health: String(streamData.health ?? '')
								};
								pullStatusText = deriveDeployStatusText();
							}
							break;
						}
						case 'service_healthy': {
							const service = String(streamData.service ?? '').trim();
							if (service) {
								deployServiceProgress[service] = {
									phase: 'service_healthy',
									health: String(streamData.health ?? ''),
									state: String(streamData.state ?? ''),
									status: String(streamData.status ?? '')
								};
								deployLastNonWaitingStatus = m.progress_deploy_service_healthy({ service });
								pullStatusText = deriveDeployStatusText();
							}
							break;
						}
						case 'service_state': {
							const service = String(streamData.service ?? '').trim();
							if (service) {
								deployServiceProgress[service] = {
									phase: 'service_state',
									state: String(streamData.state ?? ''),
									health: String(streamData.health ?? ''),
									status: String(streamData.status ?? '')
								};
								deployLastNonWaitingStatus = m.progress_deploy_service_state({
									service,
									state: String(streamData.state ?? '')
								});
								pullStatusText = deriveDeployStatusText();
							}
							break;
						}
						case 'service_status': {
							const service = String(streamData.service ?? '').trim();
							if (service) {
								deployServiceProgress[service] = {
									phase: 'service_status',
									status: String(streamData.status ?? ''),
									state: String(streamData.state ?? ''),
									health: String(streamData.health ?? '')
								};
								deployLastNonWaitingStatus = m.progress_deploy_service_status({
									service,
									status: String(streamData.status ?? '')
								});
								pullStatusText = deriveDeployStatusText();
							}
							break;
						}
						case 'complete':
							pullStatusText = m.progress_deploy_completed();
							deployPulling = false;
							pullProgress = 100;
							break;
						default:
							break;
					}
					return;
				}

				if (isDownloadingLine(streamData)) {
					pullStatusText = m.images_pull_initiating();
				}

				if (streamData.status) pullStatusText = String(streamData.status);

				if (streamData.id) {
					const currentLayer = layerProgress[streamData.id] || { current: 0, total: 0, status: '' };
					currentLayer.status = streamData.status || currentLayer.status;
					if (streamData.progressDetail) {
						const { current, total } = streamData.progressDetail;
						if (typeof current === 'number') currentLayer.current = current;
						if (typeof total === 'number') currentLayer.total = total;
					}
					layerProgress[streamData.id] = currentLayer;
					updatePullProgress();
				}
			});

			if (hadError) throw new Error(pullError || m.progress_deploy_failed());

			// Deployment finished successfully.
			pullProgress = 100;
			deployPulling = false;
			pullStatusText = m.progress_deploy_completed();
			await invalidateAll();

			setTimeout(() => {
				deployPullPopoverOpen = false;
				deployPulling = false;
				resetPullState();
			}, 1500);

			// Deploy already completed successfully
			itemState = 'running';
			toast.success(m.common_started_success({ type: name || type }));
			onActionComplete('running');
		} catch (e: any) {
			const message = e?.message || m.common_action_failed_with_type({ action: m.common_start(), type });
			if (openedPopover) {
				pullError = message;
				pullStatusText = m.images_pull_failed_with_error({ error: message });
				deployPulling = false;
			}
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
				toast.success(m.common_stopped_success({ type: name || type }));
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
				toast.success(m.common_restarted_success({ type: name || type }));
				onActionComplete('running');
			}
		});
	}

	async function handleProjectPull() {
		resetPullState();
		projectPulling = true;
		pullPopoverOpen = true;
		pullStatusText = m.images_pull_initiating();

		let wasSuccessful = false;

		try {
			await projectService.pullProjectImages(id, (data) => {
				if (!data) return;

				if (data.error) {
					const errMsg = typeof data.error === 'string' ? data.error : data.error.message || m.images_pull_stream_error();
					pullError = errMsg;
					pullStatusText = m.images_pull_failed_with_error({ error: errMsg });
					return;
				}

				if (data.status) pullStatusText = data.status;

				if (data.id) {
					const currentLayer = layerProgress[data.id] || { current: 0, total: 0, status: '' };
					currentLayer.status = data.status || currentLayer.status;

					if (data.progressDetail) {
						const { current, total } = data.progressDetail;
						if (typeof current === 'number') currentLayer.current = current;
						if (typeof total === 'number') currentLayer.total = total;
					}
					layerProgress[data.id] = currentLayer;
				}

				updatePullProgress();
			});

			// Stream finished
			updatePullProgress();
			if (!pullError && pullProgress < 100 && areAllLayersComplete(layerProgress)) {
				pullProgress = 100;
			}

			if (pullError) throw new Error(pullError);

			wasSuccessful = true;
			pullProgress = 100;
			pullStatusText = m.images_pulled_success();
			toast.success(m.images_pulled_success());
			await invalidateAll();

			setTimeout(() => {
				pullPopoverOpen = false;
				projectPulling = false;
				resetPullState();
			}, 2000);
		} catch (error: any) {
			const message = error?.message || m.images_pull_failed();
			pullError = message;
			pullStatusText = m.images_pull_failed_with_error({ error: message });
			toast.error(message);
		} finally {
			if (!wasSuccessful) {
				projectPulling = false;
			}
		}
	}
</script>

{#if desktopVariant === 'adaptive'}
	<div>
		<!-- On xl+ show labels; below xl use icon-only to avoid overflow in constrained headers (sidebar layouts) -->
		{#if isLgUp}
			<div class="flex items-center gap-2">
				{#if showContainerStart}
					<ArcaneButton
						action="start"
						size={adaptiveIconOnly ? 'icon' : 'default'}
						showLabel={!adaptiveIconOnly}
						tooltipContent={containerStartTooltip}
						onclick={() => handleStart()}
						loading={uiLoading.start}
					/>
				{/if}

				{#if showProjectUp}
					<ProgressPopover
						bind:open={deployPullPopoverOpen}
						bind:progress={pullProgress}
						mode="generic"
						openOnTriggerClick={false}
						title={m.progress_deploying_project()}
						completeTitle={m.progress_deploy_completed()}
						statusText={pullStatusText}
						error={pullError}
						loading={deployPulling}
						icon={DownloadIcon}
						layers={layerProgress}
					>
						<ContextMenu.Root bind:open={upContextMenuOpen}>
							<ContextMenu.Trigger>
								<ArcaneButton
									action="deploy"
									size={adaptiveIconOnly ? 'icon' : 'default'}
									showLabel={!adaptiveIconOnly}
									tooltipContent={projectUpTooltip}
									onclick={handleProjectUpClick}
									oncontextmenu={syncUpContextOptions}
									loading={uiLoading.start}
								/>
							</ContextMenu.Trigger>
							<ContextMenu.Content class="w-64">
								<ContextMenu.Label>{'Up options'}</ContextMenu.Label>
								<ContextMenu.Separator />
								<ContextMenu.CheckboxItem
									onSelect={(event) => toggleContextCheckbox(event, 'pullLatest')}
									checked={upContextPullLatest}
								>
									{'Pull latest images before Up'}
								</ContextMenu.CheckboxItem>
								<ContextMenu.CheckboxItem
									onSelect={(event) => toggleContextCheckbox(event, 'forceRecreate')}
									checked={upContextForceRecreate}
								>
									{'Recreate existing containers'}
								</ContextMenu.CheckboxItem>
								<ContextMenu.Separator />
								<ContextMenu.Item onclick={handleDeployWithContextOptions}>{m.common_up()}</ContextMenu.Item>
								<ContextMenu.Item onclick={handleSaveContextUpDefaults}>Save as environment defaults</ContextMenu.Item>
							</ContextMenu.Content>
						</ContextMenu.Root>
					</ProgressPopover>
				{/if}

				{#if showContainerRunningActions}
					<ArcaneButton
						action="stop"
						size={adaptiveIconOnly ? 'icon' : 'default'}
						showLabel={!adaptiveIconOnly}
						tooltipContent={containerStopTooltip}
						onclick={() => handleStop()}
						loading={uiLoading.stop}
					/>
					<ArcaneButton
						action="restart"
						size={adaptiveIconOnly ? 'icon' : 'default'}
						showLabel={!adaptiveIconOnly}
						tooltipContent={containerRestartTooltip}
						onclick={() => handleRestart()}
						loading={uiLoading.restart}
					/>
				{/if}

				{#if showProjectDown}
					<ArcaneButton
						action="stop"
						size={adaptiveIconOnly ? 'icon' : 'default'}
						showLabel={!adaptiveIconOnly}
						customLabel={m.common_down()}
						tooltipContent={projectDownTooltip}
						onclick={() => handleStop()}
						loading={uiLoading.stop}
					/>
				{/if}

				{#if showProjectRestart}
					<ArcaneButton
						action="restart"
						size={adaptiveIconOnly ? 'icon' : 'default'}
						showLabel={!adaptiveIconOnly}
						tooltipContent={projectRestartTooltip}
						onclick={() => handleRestart()}
						loading={uiLoading.restart}
					/>
				{/if}

				{#if type === 'container'}
					<ArcaneButton
						action="remove"
						size={adaptiveIconOnly ? 'icon' : 'default'}
						showLabel={!adaptiveIconOnly}
						tooltipContent={containerRemoveTooltip}
						onclick={() => confirmAction('remove')}
						loading={uiLoading.remove}
					/>
				{:else}
					<ArcaneButton
						action="redeploy"
						size={adaptiveIconOnly ? 'icon' : 'default'}
						showLabel={!adaptiveIconOnly}
						tooltipContent={projectRedeployTooltip}
						onclick={() => confirmAction('redeploy')}
						loading={uiLoading.redeploy}
					/>

					{#if type === 'project'}
						<ProgressPopover
							bind:open={pullPopoverOpen}
							bind:progress={pullProgress}
							title={m.progress_pulling_images()}
							statusText={pullStatusText}
							error={pullError}
							loading={projectPulling}
							icon={DownloadIcon}
							layers={layerProgress}
						>
							<ArcaneButton
								action="pull"
								size={adaptiveIconOnly ? 'icon' : 'default'}
								showLabel={!adaptiveIconOnly}
								tooltipContent={projectPullTooltip}
								onclick={() => handleProjectPull()}
								loading={projectPulling}
							/>
						</ProgressPopover>
					{/if}

					{#if onRefresh}
						<ArcaneButton
							action="refresh"
							size={adaptiveIconOnly ? 'icon' : 'default'}
							showLabel={!adaptiveIconOnly}
							tooltipContent={projectRefreshTooltip}
							onclick={() => handleRefresh()}
							loading={uiLoading.refresh}
						/>
					{/if}

					<ArcaneButton
						customLabel={type === 'project' ? m.compose_destroy() : m.common_remove()}
						action="remove"
						size={adaptiveIconOnly ? 'icon' : 'default'}
						showLabel={!adaptiveIconOnly}
						tooltipContent={projectDestroyTooltip}
						onclick={() => confirmAction('remove')}
						loading={uiLoading.remove}
					/>
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
						class="bg-popover/20 z-50 min-w-[180px] rounded-xl border p-1 shadow-lg backdrop-blur-md"
					>
						<DropdownMenu.Group>
							{#if showContainerStart}
								<DropdownMenu.Item onclick={handleStart} disabled={uiLoading.start}>
									{m.common_start()}
								</DropdownMenu.Item>
							{/if}

							{#if showContainerRunningActions}
								<DropdownMenu.Item onclick={handleStop} disabled={uiLoading.stop}>
									{m.common_stop()}
								</DropdownMenu.Item>
								<DropdownMenu.Item onclick={handleRestart} disabled={uiLoading.restart}>
									{m.common_restart()}
								</DropdownMenu.Item>
							{/if}

							{#if showProjectUp}
								<DropdownMenu.Item onclick={() => handleProjectUpClick()} disabled={uiLoading.start}>
									{m.common_up()}
								</DropdownMenu.Item>
							{/if}

							{#if showProjectDown}
								<DropdownMenu.Item onclick={handleStop} disabled={uiLoading.stop}>
									{m.common_down()}
								</DropdownMenu.Item>
							{/if}

							{#if showProjectRestart}
								<DropdownMenu.Item onclick={handleRestart} disabled={uiLoading.restart}>
									{m.common_restart()}
								</DropdownMenu.Item>
							{/if}

							{#if type === 'container'}
								<DropdownMenu.Item onclick={() => confirmAction('remove')} disabled={uiLoading.remove}>
									{m.common_remove()}
								</DropdownMenu.Item>
							{:else}
								<DropdownMenu.Item onclick={() => confirmAction('redeploy')} disabled={uiLoading.redeploy}>
									{m.common_redeploy()}
								</DropdownMenu.Item>

								{#if type === 'project'}
									<DropdownMenu.Item onclick={handleProjectPull} disabled={projectPulling || uiLoading.pulling}>
										{m.images_pull()}
									</DropdownMenu.Item>
								{/if}

								{#if onRefresh}
									<DropdownMenu.Item onclick={handleRefresh} disabled={uiLoading.refresh}>
										{m.common_refresh()}
									</DropdownMenu.Item>
								{/if}

								<DropdownMenu.Item onclick={() => confirmAction('remove')} disabled={uiLoading.remove}>
									{type === 'project' ? m.compose_destroy() : m.common_remove()}
								</DropdownMenu.Item>
							{/if}
						</DropdownMenu.Group>
					</DropdownMenu.Content>
				</DropdownMenu.Root>

				{#if type === 'project'}
					<ProgressPopover
						bind:open={deployPullPopoverOpen}
						bind:progress={pullProgress}
						mode="generic"
						title={m.progress_deploying_project()}
						completeTitle={m.progress_deploy_completed()}
						statusText={pullStatusText}
						error={pullError}
						loading={deployPulling}
						icon={DownloadIcon}
						layers={layerProgress}
						triggerClass="hidden"
					>
						<span class="hidden"></span>
					</ProgressPopover>

					<ProgressPopover
						bind:open={pullPopoverOpen}
						bind:progress={pullProgress}
						title={m.progress_pulling_images()}
						statusText={pullStatusText}
						error={pullError}
						loading={projectPulling}
						icon={DownloadIcon}
						layers={layerProgress}
						triggerClass="hidden"
					>
						<span class="hidden"></span>
					</ProgressPopover>
				{/if}
			</div>
		{/if}
	</div>
{:else}
	<div>
		<div class="hidden items-center gap-2 lg:flex">
			{#if showContainerStart}
				<ArcaneButton
					action="start"
					tooltipContent={containerStartTooltip}
					onclick={() => handleStart()}
					loading={uiLoading.start}
				/>
			{/if}

			{#if showProjectUp}
				<ProgressPopover
					bind:open={deployPullPopoverOpen}
					bind:progress={pullProgress}
					openOnTriggerClick={false}
					title={m.progress_deploying_project()}
					completeTitle={m.progress_deploy_completed()}
					statusText={pullStatusText}
					error={pullError}
					loading={deployPulling}
					icon={DownloadIcon}
					layers={layerProgress}
				>
					<ContextMenu.Root bind:open={upContextMenuOpen}>
						<ContextMenu.Trigger>
							<ArcaneButton
								action="deploy"
								tooltipContent={projectUpTooltip}
								onclick={handleProjectUpClick}
								oncontextmenu={syncUpContextOptions}
								loading={uiLoading.start}
							/>
						</ContextMenu.Trigger>
						<ContextMenu.Content class="w-64">
							<ContextMenu.Label>{'Up options'}</ContextMenu.Label>
							<ContextMenu.Separator />
							<ContextMenu.CheckboxItem
								onSelect={(event) => toggleContextCheckbox(event, 'pullLatest')}
								checked={upContextPullLatest}
							>
								{'Pull latest images before Up'}
							</ContextMenu.CheckboxItem>
							<ContextMenu.CheckboxItem
								onSelect={(event) => toggleContextCheckbox(event, 'forceRecreate')}
								checked={upContextForceRecreate}
							>
								{'Recreate existing containers'}
							</ContextMenu.CheckboxItem>
							<ContextMenu.Separator />
							<ContextMenu.Item onclick={handleDeployWithContextOptions}>{m.common_up()}</ContextMenu.Item>
							<ContextMenu.Item onclick={handleSaveContextUpDefaults}>Save as environment defaults</ContextMenu.Item>
						</ContextMenu.Content>
					</ContextMenu.Root>
				</ProgressPopover>
			{/if}

			{#if showContainerRunningActions}
				<ArcaneButton action="stop" tooltipContent={containerStopTooltip} onclick={() => handleStop()} loading={uiLoading.stop} />
				<ArcaneButton
					action="restart"
					tooltipContent={containerRestartTooltip}
					onclick={() => handleRestart()}
					loading={uiLoading.restart}
				/>
			{/if}

			{#if showProjectDown}
				<ArcaneButton
					action="stop"
					customLabel={m.common_down()}
					tooltipContent={projectDownTooltip}
					onclick={() => handleStop()}
					loading={uiLoading.stop}
				/>
			{/if}

			{#if showProjectRestart}
				<ArcaneButton
					action="restart"
					tooltipContent={projectRestartTooltip}
					onclick={() => handleRestart()}
					loading={uiLoading.restart}
				/>
			{/if}

			{#if type === 'container'}
				<ArcaneButton
					action="remove"
					tooltipContent={containerRemoveTooltip}
					onclick={() => confirmAction('remove')}
					loading={uiLoading.remove}
				/>
			{:else}
				<ArcaneButton
					action="redeploy"
					tooltipContent={projectRedeployTooltip}
					onclick={() => confirmAction('redeploy')}
					loading={uiLoading.redeploy}
				/>

				{#if type === 'project'}
					<ProgressPopover
						bind:open={pullPopoverOpen}
						bind:progress={pullProgress}
						title={m.progress_pulling_images()}
						statusText={pullStatusText}
						error={pullError}
						loading={projectPulling}
						icon={DownloadIcon}
						layers={layerProgress}
					>
						<ArcaneButton
							action="pull"
							tooltipContent={projectPullTooltip}
							onclick={() => handleProjectPull()}
							loading={projectPulling}
						/>
					</ProgressPopover>
				{/if}

				{#if onRefresh}
					<ArcaneButton
						action="refresh"
						tooltipContent={projectRefreshTooltip}
						onclick={() => handleRefresh()}
						loading={uiLoading.refresh}
					/>
				{/if}

				<ArcaneButton
					customLabel={type === 'project' ? m.compose_destroy() : m.common_remove()}
					action="remove"
					tooltipContent={projectDestroyTooltip}
					onclick={() => confirmAction('remove')}
					loading={uiLoading.remove}
				/>
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
					class="bg-popover/20 z-50 min-w-[180px] rounded-xl border p-1 shadow-lg backdrop-blur-md"
				>
					<DropdownMenu.Group>
						{#if showContainerStart}
							<DropdownMenu.Item onclick={handleStart} disabled={uiLoading.start}>
								{m.common_start()}
							</DropdownMenu.Item>
						{/if}

						{#if showContainerRunningActions}
							<DropdownMenu.Item onclick={handleStop} disabled={uiLoading.stop}>
								{m.common_stop()}
							</DropdownMenu.Item>
							<DropdownMenu.Item onclick={handleRestart} disabled={uiLoading.restart}>
								{m.common_restart()}
							</DropdownMenu.Item>
						{/if}

						{#if showProjectUp}
							<DropdownMenu.Item onclick={() => handleProjectUpClick()} disabled={uiLoading.start}>
								{m.common_up()}
							</DropdownMenu.Item>
						{/if}

						{#if showProjectDown}
							<DropdownMenu.Item onclick={handleStop} disabled={uiLoading.stop}>
								{m.common_down()}
							</DropdownMenu.Item>
						{/if}

						{#if showProjectRestart}
							<DropdownMenu.Item onclick={handleRestart} disabled={uiLoading.restart}>
								{m.common_restart()}
							</DropdownMenu.Item>
						{/if}

						{#if type === 'container'}
							<DropdownMenu.Item onclick={() => confirmAction('remove')} disabled={uiLoading.remove}>
								{m.common_remove()}
							</DropdownMenu.Item>
						{:else}
							<DropdownMenu.Item onclick={() => confirmAction('redeploy')} disabled={uiLoading.redeploy}>
								{m.common_redeploy()}
							</DropdownMenu.Item>

							{#if type === 'project'}
								<DropdownMenu.Item onclick={handleProjectPull} disabled={projectPulling || uiLoading.pulling}>
									{m.images_pull()}
								</DropdownMenu.Item>
							{/if}

							{#if onRefresh}
								<DropdownMenu.Item onclick={handleRefresh} disabled={uiLoading.refresh}>
									{m.common_refresh()}
								</DropdownMenu.Item>
							{/if}

							<DropdownMenu.Item onclick={() => confirmAction('remove')} disabled={uiLoading.remove}>
								{type === 'project' ? m.compose_destroy() : m.common_remove()}
							</DropdownMenu.Item>
						{/if}
					</DropdownMenu.Group>
				</DropdownMenu.Content>
			</DropdownMenu.Root>
		</div>
	</div>
{/if}
