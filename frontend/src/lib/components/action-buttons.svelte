<script lang="ts">
	import { openConfirmDialog } from './confirm-dialog';
	import { goto, refreshAll } from '$app/navigation';
	import { toast } from 'svelte-sonner';
	import { tryCatch } from '#lib/utils/try-catch.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import ActionButtonGroup from '#lib/components/action-button-group/action-button-group.svelte';
	import type { ActionButton } from '#lib/components/action-button-group/types.js';
	import DeployOptionsMenuItems from '#lib/components/deploy-split-button/deploy-options-menu-items.svelte';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import settingsStore from '#lib/stores/config-store.svelte.js';
	import { deployOptionsStore } from '#lib/stores/deploy-options.store.svelte.js';
	import { containerService } from '#lib/services/container-service.js';
	import { projectService } from '#lib/services/project-service.js';
	import type { DeployProjectOptions } from '#lib/types/project-deployment.js';
	import { activityToastOptions, activityIdFromStreamFrame, extractActivityId } from '#lib/utils/activity-toast.js';
	import { operationWatchStore } from '#lib/stores/operation-watch.store.svelte.js';
	import { attachProjectLogsToWatch } from '#lib/utils/watch-logs.js';
	import type { Project } from '#lib/types/swarm.js';
	import type { ContainerDetailsDto } from '#lib/types/docker.js';
	import { TerminalIcon } from '#lib/icons/index.js';
	import { createMutation } from '@tanstack/svelte-query';
	import { hasPermission } from '#lib/utils/auth.js';
	import { isDepotBuildAvailable } from '#lib/utils/build-provider.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import { Temporal } from 'temporal-polyfill';

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
		refresh?: boolean;
	};

	let {
		id,
		name,
		type = 'container',
		itemState = 'stopped',
		onActionComplete = () => {},
		startLoading = $bindable(false),
		stopLoading = $bindable(false),
		restartLoading = $bindable(false),
		removeLoading = $bindable(false),
		redeployLoading = $bindable(false),
		hasBuildDirective = false,
		disableRedeploy = false,
		disabled = false,
		disabledReason,
		onRefresh,
		extraActions = []
	}: {
		id: string;
		name?: string;
		type?: TargetType;
		itemState?: string;
		onActionComplete?: (status?: string) => void;
		startLoading?: boolean;
		stopLoading?: boolean;
		restartLoading?: boolean;
		removeLoading?: boolean;
		redeployLoading?: boolean;
		hasBuildDirective?: boolean;
		disableRedeploy?: boolean;
		disabled?: boolean;
		disabledReason?: string;
		onRefresh?: () => void | Promise<void>;
		extraActions?: ActionButton[];
	} = $props();

	let isLoading = $state<LoadingStates>({
		start: false,
		stop: false,
		restart: false,
		remove: false,
		pull: false,
		build: false,
		redeploy: false,
		refresh: false
	});

	function setLoading<K extends keyof LoadingStates>(key: K, value: boolean) {
		isLoading[key] = value;

		if (key === 'start') startLoading = value;
		if (key === 'stop') stopLoading = value;
		if (key === 'restart') restartLoading = value;
		if (key === 'remove') removeLoading = value;
		if (key === 'redeploy') redeployLoading = value;
	}

	const uiLoading = $derived({
		start: !!(isLoading.start || startLoading),
		stop: !!(isLoading.stop || stopLoading),
		restart: !!(isLoading.restart || restartLoading),
		remove: !!(isLoading.remove || removeLoading),
		pull: !!isLoading.pull,
		build: !!isLoading.build,
		redeploy: !!(isLoading.redeploy || redeployLoading),
		refresh: !!isLoading.refresh
	});

	const startMutation = createMutation(() => ({
		mutationKey: ['action', 'start', type, id],
		mutationFn: () =>
			tryCatch(
				type === 'container'
					? containerService.startContainer(id)
					: projectService.deployProject(id, 'up', deployOptionsStore.takeRequestOptions())
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

	// Set from the redeploy stream's started frame so the success toast can link
	// to the activity.
	let redeployActivityId = $state<string | undefined>(undefined);

	const redeployMutation = createMutation(() => ({
		mutationKey: ['action', 'redeploy', type, id],
		mutationFn: ({ watch = false }: { watch?: boolean } = {}) =>
			tryCatch(
				(type === 'container'
					? containerService.redeployContainer(id)
					: projectService.deployProject(
							id,
							'redeploy',
							(frame) => {
								redeployActivityId = activityIdFromStreamFrame(frame) ?? redeployActivityId;
								if (watch) {
									operationWatchStore.onLine(frame);
								}
							},
							// Taken inside mutationFn so cancelling the confirm dialog doesn't
							// spend the single-shot recreateVolumes opt-in.
							deployOptionsStore.takeRequestOptions()
						)) as Promise<ContainerDetailsDto | Project>
			),
		onMutate: () => {
			redeployActivityId = undefined;
			setLoading('redeploy', true);
		},
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

	const isLifecycleActionPending = $derived(
		!!(uiLoading.start || uiLoading.stop || uiLoading.restart || uiLoading.redeploy || uiLoading.remove)
	);
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
	const depotAvailable = $derived(isDepotBuildAvailable(settingsStore.current));
	const projectBuildProvider = $derived.by<'local' | 'depot'>(() => {
		const configuredProvider = (settingsStore.current?.buildProvider as 'local' | 'depot') ?? 'local';
		if (configuredProvider === 'depot' && !depotAvailable) {
			return 'local';
		}
		return configuredProvider;
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
						await handleApiResultWithCallbacks({
							result,
							message: m.common_action_failed_with_type({
								action: type === 'project' ? m.compose_destroy() : m.common_remove(),
								type: type
							}),
							onSuccess: async (data) => {
								const activityId = extractActivityId(data);
								if (activityId) {
									toast.success(
										type === 'project' ? m.compose_destroy_success() : m.containers_remove_success(),
										activityToastOptions(activityId)
									);
								}
								await refreshAll();
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
			confirmRedeploy(false);
		}
	}

	function confirmRedeploy(watch: boolean) {
		openConfirmDialog({
			title: type === 'container' ? m.container_confirm_redeploy_title() : m.common_confirm_redeploy_title(),
			message: type === 'container' ? m.container_confirm_redeploy_message() : m.common_confirm_redeploy_message(),
			confirm: {
				label: m.common_redeploy(),
				action: async () => {
					const operationStartedAt = Math.floor(Temporal.Now.instant().epochMilliseconds / 1000);
					if (watch) {
						operationWatchStore.start(`${m.common_redeploy()} — ${name ?? id}`);
					}
					const result = await redeployMutation.mutateAsync({ watch });
					if (watch && result.error) {
						operationWatchStore.fail(
							result.error.message || m.common_action_failed_with_type({ action: m.common_redeploy(), type })
						);
						return;
					}
					if (watch) {
						enterInteractiveWatchInternal(operationStartedAt);
					}
					await handleApiResultWithCallbacks({
						result,
						message: m.common_action_failed_with_type({ action: m.common_redeploy(), type }),
						onSuccess: async (data) => {
							const activityId = type === 'container' ? extractActivityId(data) : redeployActivityId;
							if (activityId && !watch) {
								toast.success(
									type === 'project' ? m.compose_redeploy_success() : m.container_redeploy_success(),
									activityToastOptions(activityId)
								);
							}
							const containerData = data as ContainerDetailsDto;
							if (type === 'container' && containerData?.id) {
								goto(`/containers/${containerData.id}`);
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

	async function handleDeploy(options?: DeployProjectOptions, watch = false) {
		setLoading('start', true);

		const operationStartedAt = Math.floor(Temporal.Now.instant().epochMilliseconds / 1000);
		if (watch) {
			operationWatchStore.start(`${deployButtonLabel} — ${name ?? id}`);
		}

		try {
			const operationResult1 = await tryCatch(
				(async () => {
					await projectService.deployProject(
						id,
						'up',
						watch ? (frame: unknown) => operationWatchStore.onLine(frame) : () => {},
						options ?? deployOptionsStore.takeRequestOptions()
					);
					if (watch) {
						enterInteractiveWatchInternal(operationStartedAt);
					}
					await refreshAll();
					itemState = 'running';
					onActionComplete('running');
				})()
			);
			if (operationResult1.error !== null) {
				const error = operationResult1.error;

				const message =
					error instanceof Error ? error.message : m.common_action_failed_with_type({ action: m.common_start(), type });
				if (watch) {
					operationWatchStore.fail(message);
				} else {
					toast.error(message);
				}
			}
		} finally {
			setLoading('start', false);
		}
	}

	// Interactive mode: like a non-detached `docker compose up`, the deploy
	// output is followed by the containers' live logs — everything they wrote
	// since the operation began — and dismissing the dialog is the Ctrl-C:
	// the project is brought down.
	function enterInteractiveWatchInternal(operationStartedAt: number) {
		operationWatchStore.append(`Attaching to ${name ?? id}`);
		const detach = attachProjectLogsToWatch(id, operationStartedAt);
		operationWatchStore.setOnClose(() => {
			detach();
			void handleStop();
		});
	}

	async function handleStop() {
		const result = await stopMutation.mutateAsync();
		await handleApiResultWithCallbacks({
			result,
			message: m.common_action_failed_with_type({ action: m.common_stop(), type }),
			onSuccess: async (data) => {
				const activityId = extractActivityId(data);
				if (activityId) {
					toast.success(
						type === 'project' ? m.compose_down_success() : m.containers_stop_success(),
						activityToastOptions(activityId)
					);
				}
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
			onSuccess: async (data) => {
				const activityId = extractActivityId(data);
				if (activityId) {
					toast.success(
						type === 'project' ? m.compose_restart_success() : m.containers_restart_success(),
						activityToastOptions(activityId)
					);
				}
				itemState = 'running';
				onActionComplete('running');
			}
		});
	}

	async function handleProjectPull(watch = false) {
		setLoading('pull', true);

		if (watch) {
			operationWatchStore.start(`${m.pull()} — ${name ?? id}`);
		}

		try {
			const operationResult2 = await tryCatch(
				(async () => {
					await projectService.pullProjectImages(id, watch ? (frame: unknown) => operationWatchStore.onLine(frame) : () => {});
					await refreshAll();
					onActionComplete(itemState);
				})()
			);
			if (operationResult2.error !== null) {
				const error = operationResult2.error;

				const message = error instanceof Error ? error.message : m.images_pull_failed();
				if (watch) {
					operationWatchStore.fail(message);
				} else {
					toast.error(message);
				}
			}
		} finally {
			setLoading('pull', false);
		}
	}

	async function handleProjectBuild() {
		setLoading('build', true);

		try {
			const operationResult3 = await tryCatch(
				(async () => {
					const buildProvider = projectBuildProvider;
					await projectService.buildProjectImages(
						id,
						{
							provider: buildProvider,
							push: buildProvider === 'depot',
							load: buildProvider !== 'depot'
						},
						() => {}
					);
					await refreshAll();
				})()
			);
			if (operationResult3.error !== null) {
				const error = operationResult3.error;

				const message = error instanceof Error ? error.message : m.build_failed();
				toast.error(message);
			}
		} finally {
			setLoading('build', false);
		}
	}

	const redeployDisabledReason = $derived(disableRedeploy ? m.common_redeploy_disabled_arcane_self() : disabledReason);

	const buttons = $derived.by((): ActionButton[] => {
		// Page-provided actions pause while a lifecycle operation is in flight.
		const list: ActionButton[] = extraActions.map((action) => ({
			...action,
			disabled: action.disabled || isLifecycleActionPending
		}));

		if (!isRunning && canStart) {
			if (type === 'container') {
				list.push({
					id: 'start',
					action: 'start',
					label: m.common_start(),
					placement: 'primary',
					group: 'lifecycle',
					loading: uiLoading.start,
					disabled,
					disabledReason,
					onclick: () => handleStart()
				});
			} else {
				list.push({
					id: 'deploy',
					action: 'deploy',
					label: deployButtonLabel,
					placement: 'primary',
					group: 'lifecycle',
					loading: uiLoading.start,
					disabled,
					disabledReason,
					onclick: () => handleDeploy(),
					menuContent: upMenu
				});
			}
		}

		if (isRunning && canStop) {
			list.push({
				id: 'stop',
				action: 'stop',
				label: type === 'project' ? m.common_down() : m.common_stop(),
				placement: 'primary',
				group: 'lifecycle',
				destructive: true,
				loading: uiLoading.stop,
				disabled,
				disabledReason,
				onclick: () => handleStop()
			});
		}

		if (isRunning && canRestart) {
			list.push({
				id: 'restart',
				action: 'restart',
				label: m.common_restart(),
				placement: 'secondary',
				group: 'lifecycle',
				loading: uiLoading.restart,
				disabled,
				disabledReason,
				onclick: () => handleRestart()
			});
		}

		if (canRedeploy) {
			list.push({
				id: 'redeploy',
				action: 'redeploy',
				label: m.common_redeploy(),
				placement: type === 'project' ? 'secondary' : 'menu',
				group: 'deploy',
				loading: uiLoading.redeploy,
				disabled: disabled || disableRedeploy,
				disabledReason: redeployDisabledReason,
				onclick: () => confirmAction('redeploy'),
				menuContent: type === 'project' ? redeployMenu : undefined
			});
		}

		if (projectHasBuildDirective && canBuild) {
			list.push({
				id: 'build',
				action: 'build',
				label: m.build(),
				group: 'deploy',
				loading: uiLoading.build,
				disabled,
				disabledReason,
				onclick: () => handleProjectBuild()
			});
		}

		if (canPull) {
			list.push({
				id: 'pull',
				action: 'pull',
				label: m.pull(),
				group: 'deploy',
				loading: uiLoading.pull,
				disabled,
				disabledReason,
				onclick: () => handleProjectPull(),
				menuContent: pullMenu
			});
		}

		if (onRefresh) {
			list.push({
				id: 'refresh',
				action: 'refresh',
				placement: 'secondary',
				iconOnly: true,
				label: m.common_refresh(),
				group: 'manage',
				loading: uiLoading.refresh,
				onclick: () => handleRefresh()
			});
		}

		if (canRemove) {
			list.push({
				id: 'remove',
				action: 'remove',
				label: type === 'project' ? m.compose_destroy() : m.common_remove(),
				group: 'danger',
				destructive: true,
				loading: uiLoading.remove,
				onclick: () => confirmAction('remove')
			});
		}

		return list;
	});
</script>

{#snippet watchItem(onWatch: () => void, disabled: boolean, label = m.watch_output())}
	<DropdownMenu.Item onclick={onWatch} {disabled}>
		<TerminalIcon class="size-4" />
		{label}
	</DropdownMenu.Item>
{/snippet}

{#snippet upMenu(disabled: boolean)}
	<DeployOptionsMenuItems />
	<DropdownMenu.Separator />
	{@render watchItem(() => handleDeploy(undefined, true), disabled)}
{/snippet}

{#snippet redeployMenu(disabled: boolean)}
	<DeployOptionsMenuItems />
	<DropdownMenu.Separator />
	{@render watchItem(() => confirmRedeploy(true), disabled)}
{/snippet}

{#snippet pullMenu(disabled: boolean)}
	{@render watchItem(() => handleProjectPull(true), disabled, m.pull_and_watch_output())}
{/snippet}

<ActionButtonGroup {buttons} class="flex-1" />
