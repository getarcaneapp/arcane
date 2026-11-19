<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { createMutation, useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';

	import ActionButtonGroup from '#lib/components/action-button-group/action-button-group.svelte';
	import type { ActionButton } from '#lib/components/action-button-group/types.js';
	import DeployOptionsMenuItems from '#lib/components/deploy-split-button/deploy-options-menu-items.svelte';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { useEnvironmentSettings } from '#lib/hooks/use-environment-settings.svelte.js';
	import { TerminalIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { containerService } from '#lib/services/container-service.js';
	import { projectService } from '#lib/services/project-service.js';
	import { deployOptionsStore } from '#lib/stores/deploy-options.store.svelte.js';
	import { operationWatchStore } from '#lib/stores/operation-watch.store.svelte.js';
	import type { MessageResponse } from '#lib/types/base.js';
	import type { ContainerDetails } from '#lib/types/container.js';
	import type { Project, ProjectDeployOptions } from '#lib/types/project.js';
	import { activityToastOptions, activityIdFromStreamFrame, extractActivityId } from '#lib/utils/activity-toast.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { isDepotBuildAvailable } from '#lib/utils/build-provider.js';
	import { tryCatch } from '#lib/utils/try-catch.js';
	import { attachProjectLogsToWatch } from '#lib/utils/watch-logs.js';

	import { openConfirmDialog } from './confirm-dialog';

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
		environmentId,
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
		archived = false,
		disabled = false,
		disabledReason,
		onRefresh,
		extraActions = [],
		hasUnsavedChanges = false,
		onSaveChanges
	}: {
		id: string;
		environmentId: string;
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
		archived?: boolean;
		disabled?: boolean;
		disabledReason?: string;
		onRefresh?: () => void | Promise<void>;
		extraActions?: ActionButton[];
		hasUnsavedChanges?: boolean;
		onSaveChanges?: () => Promise<boolean>;
	} = $props();

	const environmentSettings = useEnvironmentSettings(() => environmentId);
	const queryClient = useQueryClient();

	const isLoading = $state<LoadingStates>({
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
		mutationFn: () => {
			if (type === 'container') {
				return tryCatch<MessageResponse | Project>(containerService.startContainer(id, environmentId));
			}
			return tryCatch<MessageResponse | Project>(
				projectService.deployProject(id, 'up', deployOptionsStore.takeRequestOptions(), environmentId)
			);
		},
		onMutate: () => setLoading('start', true),
		onSettled: () => setLoading('start', false)
	}));

	const stopMutation = createMutation(() => ({
		mutationKey: ['action', 'stop', type, id],
		mutationFn: (target: ActionTarget = actionTarget()) => {
			if (type === 'container') {
				return tryCatch<MessageResponse | Project>(containerService.stopContainer(target.id, target.environmentId));
			}
			return tryCatch<MessageResponse | Project>(projectService.downProject(target.id, target.environmentId));
		},
		onMutate: () => setLoading('stop', true),
		onSettled: () => setLoading('stop', false)
	}));

	const restartMutation = createMutation(() => ({
		mutationKey: ['action', 'restart', type, id],
		mutationFn: () => {
			if (type === 'container') {
				return tryCatch<MessageResponse | Project>(containerService.restartContainer(id, environmentId));
			}
			return tryCatch<MessageResponse | Project>(projectService.restartProject(id, undefined, environmentId));
		},
		onMutate: () => setLoading('restart', true),
		onSettled: () => setLoading('restart', false)
	}));

	// Set from the redeploy stream's started frame so the success toast can link
	// to the activity.
	let redeployActivityId = $state<string | undefined>(undefined);

	const redeployMutation = createMutation(() => ({
		mutationKey: ['action', 'redeploy', type, id],
		mutationFn: ({ watch = false, target }: { watch?: boolean; target: ActionTarget }) => {
			if (type === 'container') {
				return tryCatch<ContainerDetails | Project>(containerService.redeployContainer(target.id, target.environmentId));
			}
			return tryCatch<ContainerDetails | Project>(
				projectService.deployProject(
					target.id,
					'redeploy',
					(frame) => {
						redeployActivityId = activityIdFromStreamFrame(frame) ?? redeployActivityId;
						if (watch) {
							operationWatchStore.onLine(frame);
						}
					},
					// Taken inside mutationFn so cancelling the confirm dialog doesn't
					// spend the single-shot recreateVolumes opt-in.
					deployOptionsStore.takeRequestOptions(),
					target.environmentId
				)
			);
		},
		onMutate: () => {
			redeployActivityId = undefined;
			setLoading('redeploy', true);
		},
		onSettled: () => setLoading('redeploy', false)
	}));

	const removeMutation = createMutation(() => ({
		mutationKey: ['action', 'remove', type, id],
		mutationFn: ({ removeVolumes, target }: { removeVolumes: boolean; target: ActionTarget }) => {
			if (type === 'container') {
				return tryCatch<MessageResponse | void>(
					containerService.deleteContainer(target.id, { volumes: removeVolumes, environmentId: target.environmentId })
				);
			}
			return tryCatch<MessageResponse | void>(projectService.destroyProject(target.id, removeVolumes, target.environmentId));
		},
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

	// Captured when an action starts: a confirmation or watch can outlive navigation to another resource.
	type ActionTarget = { id: string; environmentId: string };
	const actionTarget = (): ActionTarget => ({ id, environmentId });
	const canStart = $derived(
		type === 'container' ? hasPermission('containers:start', environmentId) : hasPermission('projects:deploy', environmentId)
	);
	const canStop = $derived(
		type === 'container' ? hasPermission('containers:stop', environmentId) : hasPermission('projects:down', environmentId)
	);
	const canRestart = $derived(
		type === 'container' ? hasPermission('containers:restart', environmentId) : hasPermission('projects:restart', environmentId)
	);
	const canRedeploy = $derived(
		type === 'container' ? hasPermission('containers:redeploy', environmentId) : hasPermission('projects:deploy', environmentId)
	);
	const canRemove = $derived(
		type === 'container' ? hasPermission('containers:delete', environmentId) : hasPermission('projects:delete', environmentId)
	);
	const canPull = $derived(type === 'project' && hasPermission('projects:deploy', environmentId));
	const canBuild = $derived(type === 'project' && hasPermission('projects:deploy', environmentId));
	const deployButtonLabel = $derived(projectHasBuildDirective ? m.compose_build_and_deploy() : m.common_up());

	async function handleRefresh() {
		if (!onRefresh) return;
		await refreshMutation.mutateAsync();
	}

	function confirmAction(action: string) {
		if (action === 'remove') {
			const target = actionTarget();
			let message = m.common_confirm_removal_message({ type: m.container() });
			if (type === 'project') {
				message = m.common_confirm_destroy_message({ type: m.project() });
			}
			openConfirmDialog({
				title: type === 'project' ? m.compose_destroy() : m.common_confirm_removal_title(),
				message,
				confirm: {
					label: type === 'project' ? m.compose_destroy() : m.common_remove(),
					destructive: true,
					action: async (checkboxStates) => {
						const removeVolumes = checkboxStates['removeVolumes'] === true;

						const result = await removeMutation.mutateAsync({ removeVolumes, target });
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
								goto(type === 'project' ? resolve('/(app)/projects') : resolve('/(app)/containers'));
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

	function confirmSaveBeforeDeploy(label: string, run: () => Promise<void>) {
		openConfirmDialog({
			title: m.common_unsaved_changes(),
			message: m.projects_save_before_deploy_message(),
			confirm: {
				label,
				action: async () => {
					if (await onSaveChanges?.()) await run();
				}
			}
		});
	}

	function confirmDeploy(watch: boolean) {
		if (hasUnsavedChanges) {
			confirmSaveBeforeDeploy(m.projects_save_and_deploy(), () => handleDeploy(undefined, watch));
			return;
		}
		void handleDeploy(undefined, watch);
	}

	function confirmRedeploy(watch: boolean) {
		const target = actionTarget();
		if (hasUnsavedChanges) {
			confirmSaveBeforeDeploy(m.projects_save_and_redeploy(), () => handleRedeploy(watch, target));
			return;
		}
		openConfirmDialog({
			title: type === 'container' ? m.container_confirm_redeploy_title() : m.common_confirm_redeploy_title(),
			message: type === 'container' ? m.container_confirm_redeploy_message() : m.common_confirm_redeploy_message(),
			confirm: {
				label: m.common_redeploy(),
				action: () => handleRedeploy(watch, target)
			}
		});
	}

	async function handleRedeploy(watch: boolean, target: ActionTarget) {
		const operationStartedAt = Math.floor(Temporal.Now.instant().epochMilliseconds / 1000);
		if (watch) {
			operationWatchStore.start(`${m.common_redeploy()} — ${name ?? id}`);
		}
		const result = await redeployMutation.mutateAsync({ watch, target });
		if (watch && result.error) {
			operationWatchStore.fail(result.error.message || m.common_action_failed_with_type({ action: m.common_redeploy(), type }));
			return;
		}
		if (watch) {
			enterInteractiveWatch(operationStartedAt, target);
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
				const containerData = data as ContainerDetails;
				if (type === 'container' && containerData?.id) {
					goto(
						resolve(`/(app)/containers/[containerId]?env=${encodeURIComponent(target.environmentId)}`, {
							containerId: containerData.id
						})
					);
				} else if (type === 'container') {
					goto(resolve('/(app)/containers'));
				} else {
					onActionComplete('running');
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

	async function handleDeploy(options?: ProjectDeployOptions, watch = false) {
		setLoading('start', true);

		const operationStartedAt = Math.floor(Temporal.Now.instant().epochMilliseconds / 1000);
		const target = actionTarget();
		if (watch) {
			operationWatchStore.start(`${deployButtonLabel} — ${name ?? id}`);
		}

		try {
			const deployed = await tryCatch(
				projectService.deployProject(
					target.id,
					'up',
					watch ? (frame: unknown) => operationWatchStore.onLine(frame) : () => {},
					options ?? deployOptionsStore.takeRequestOptions(),
					target.environmentId
				)
			);
			if (deployed.error !== null) {
				let message: string = m.common_action_failed_with_type({ action: m.common_start(), type });
				if (deployed.error instanceof Error) {
					message = deployed.error.message;
				}
				if (watch) {
					operationWatchStore.fail(message);
				} else {
					toast.error(message);
				}
				return;
			}
			if (watch) {
				enterInteractiveWatch(operationStartedAt, target);
			}
			itemState = 'running';
			onActionComplete('running');
		} finally {
			setLoading('start', false);
		}
	}

	function enterInteractiveWatch(operationStartedAt: number, target: ActionTarget) {
		operationWatchStore.append(`Attaching to ${name ?? id}`);
		const detach = attachProjectLogsToWatch(target.id, operationStartedAt, target.environmentId);
		operationWatchStore.setOnClose(() => {
			detach();
			void handleStop(target);
		});
	}

	async function handleStop(target: ActionTarget = actionTarget()) {
		const result = await stopMutation.mutateAsync(target);
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
			const pulled = await tryCatch(
				projectService.pullProjectImages(
					id,
					watch ? (frame: unknown) => operationWatchStore.onLine(frame) : () => {},
					environmentId
				)
			);
			if (pulled.error !== null) {
				const message = pulled.error instanceof Error ? pulled.error.message : m.images_pull_failed();
				if (watch) {
					operationWatchStore.fail(message);
				} else {
					toast.error(message);
				}
				return;
			}
			onActionComplete(itemState);
		} finally {
			setLoading('pull', false);
		}
	}

	async function handleProjectBuild() {
		setLoading('build', true);

		try {
			const settings = environmentSettings.current;
			const configuredProvider = (settings?.values.buildProvider as 'local' | 'depot') ?? 'local';
			const buildProvider = configuredProvider === 'depot' && isDepotBuildAvailable(settings) ? 'depot' : 'local';
			const built = await tryCatch(
				projectService.buildProjectImages(
					id,
					{
						provider: buildProvider,
						push: buildProvider === 'depot',
						load: buildProvider !== 'depot'
					},
					() => {},
					environmentId
				)
			);
			if (built.error !== null) {
				toast.error(built.error instanceof Error ? built.error.message : m.build_failed());
				return;
			}
			const invalidated = await tryCatch(
				queryClient.invalidateQueries({ queryKey: queryKeys.projects.detail(environmentId, id) })
			);
			if (invalidated.error !== null) {
				toast.error(invalidated.error instanceof Error ? invalidated.error.message : m.build_failed());
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

		if (!archived && !isRunning && canStart) {
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
					onclick: () => confirmDeploy(false),
					menuContent: upMenu
				});
			}
		}

		if (!archived && isRunning && canStop) {
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

		if (!archived && isRunning && canRestart) {
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

		if (!archived && canRedeploy) {
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

		if (!archived && projectHasBuildDirective && canBuild) {
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

		if (!archived && canPull) {
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
	{@render watchItem(() => confirmDeploy(true), disabled)}
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
