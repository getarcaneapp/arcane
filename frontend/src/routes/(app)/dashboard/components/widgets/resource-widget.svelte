<script lang="ts">
	import { resolve } from '$app/paths';
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { mode } from 'mode-watcher';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import IconImage from '#lib/components/icon-image.svelte';
	import { Badge, type BadgeVariant } from '#lib/components/ui/badge/index.js';
	import { ContainersIcon, FolderOpenIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { containerDetailQuery } from '#lib/query/containers.js';
	import { projectDetailQuery } from '#lib/query/projects.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { runContainerLifecycleAction } from '#lib/utils/container-actions.js';
	import type { DashboardWidget } from '#lib/utils/dashboard-layout.js';
	import { getStatusVariant, getThemedIconUrl } from '#lib/utils/docker.js';
	import { capitalizeFirstLetter } from '#lib/utils/formatting.js';

	import { getContainerStatusLabel, getStateBadgeVariant } from '../../../containers/container-table.helpers';
	import { runProjectAction } from '../../../projects/projects-table.actions';
	import DashboardPanel from '../dashboard-panel.svelte';
	import WidgetStatus from './widget-status.svelte';
	import { resolveWidgetTarget } from './widget-target';

	type QuickAction = 'start' | 'stop' | 'restart';

	const RUNNING_ACTIONS: QuickAction[] = ['stop', 'restart'];
	const STOPPED_ACTIONS: QuickAction[] = ['start'];
	const ACTION_STATUS: Record<QuickAction, string> = { start: 'starting', stop: 'stopping', restart: 'restarting' };
	const CONTAINER_PERMISSIONS: Record<QuickAction, string> = {
		start: 'containers:start',
		stop: 'containers:stop',
		restart: 'containers:restart'
	};
	const PROJECT_PERMISSIONS: Record<QuickAction, string> = {
		start: 'projects:deploy',
		stop: 'projects:down',
		restart: 'projects:restart'
	};
	const PROJECT_LABELS: Record<QuickAction, () => string> = {
		start: m.common_up,
		stop: m.common_down,
		restart: m.common_restart
	};

	let { widget }: { widget: DashboardWidget } = $props();

	const queryClient = useQueryClient();

	let actionStatus = $state('');

	const config = $derived(widget.config ?? {});
	const isProject = $derived(config.resourceKind === 'projects');
	const environmentId = $derived(config.environmentIds?.[0] ?? '');
	const resourceId = $derived(config.resourceId ?? '');
	const target = $derived(resolveWidgetTarget(config, [isProject ? 'projects:read' : 'containers:read']));
	const permissions = $derived(isProject ? PROJECT_PERMISSIONS : CONTAINER_PERMISSIONS);
	const title = $derived(widget.title || (isProject ? m.project() : m.container()));

	const containerQuery = createQuery(() => ({
		...containerDetailQuery(environmentId, resourceId),
		enabled: target.status === 'ready' && !isProject
	}));

	const projectQuery = createQuery(() => ({
		...projectDetailQuery(environmentId, resourceId),
		enabled: target.status === 'ready' && isProject
	}));

	function refresh() {
		return Promise.all(
			isProject
				? [
						queryClient.invalidateQueries({ queryKey: queryKeys.projects.all }),
						queryClient.invalidateQueries({ queryKey: queryKeys.projects.detail(environmentId, resourceId) })
					]
				: [
						queryClient.invalidateQueries({ queryKey: queryKeys.containers.all }),
						queryClient.invalidateQueries({ queryKey: queryKeys.containers.detail(environmentId, resourceId) })
					]
		);
	}

	function run(action: QuickAction) {
		const options = {
			environmentId,
			setStatus: (status: string) => {
				actionStatus = status;
			},
			onRefresh: refresh
		};
		if (isProject) {
			void runProjectAction({ ...options, action, projectId: resourceId });
			return;
		}
		void runContainerLifecycleAction({ ...options, action, containerId: resourceId });
	}
</script>

{#snippet summary(name: string, iconUrl: string | null, href: string, status: string, variant: BadgeVariant)}
	<div class="flex min-w-0 items-center gap-3">
		<IconImage
			src={iconUrl}
			alt={name}
			fallback={isProject ? FolderOpenIcon : ContainersIcon}
			class="size-5"
			containerClass="size-9 shrink-0"
		/>
		<div class="min-w-0 flex-1 space-y-1">
			<a {href} class="block truncate text-sm font-medium hover:underline" title={name}>{name}</a>
			<Badge {variant} size="sm">{status}</Badge>
		</div>
	</div>
{/snippet}

{#snippet actions(running: boolean, disabled: boolean)}
	<div class="flex flex-wrap gap-2">
		{#each running ? RUNNING_ACTIONS : STOPPED_ACTIONS as action (action)}
			{#if hasPermission(permissions[action], environmentId)}
				<ArcaneButton
					{action}
					size="sm"
					customLabel={isProject ? PROJECT_LABELS[action]() : undefined}
					loading={actionStatus === ACTION_STATUS[action]}
					disabled={disabled || actionStatus !== ''}
					onclick={() => run(action)}
				/>
			{/if}
		{/each}
	</div>
{/snippet}

<DashboardPanel {title}>
	{#if isProject}
		<WidgetStatus {target} data={projectQuery.data} error={projectQuery.error} resource={m.project()}>
			{#snippet children(project)}
				<div class="space-y-4 py-1">
					{@render summary(
						project.name,
						getThemedIconUrl(project, mode.current),
						resolve(`/(app)/projects/[projectId]?env=${encodeURIComponent(environmentId)}`, { projectId: project.id }),
						capitalizeFirstLetter(project.status),
						getStatusVariant(project.status)
					)}
					{@render actions(project.status === 'running', !!project.isArchived)}
				</div>
			{/snippet}
		</WidgetStatus>
	{:else}
		<WidgetStatus {target} data={containerQuery.data} error={containerQuery.error} resource={m.container()}>
			{#snippet children(container)}
				<div class="space-y-4 py-1">
					{@render summary(
						container.name,
						getThemedIconUrl(container, mode.current),
						resolve(`/(app)/containers/[containerId]?env=${encodeURIComponent(environmentId)}`, {
							containerId: container.id
						}),
						getContainerStatusLabel(container.state.status),
						getStateBadgeVariant(container.state.status)
					)}
					{@render actions(container.state.running, false)}
				</div>
			{/snippet}
		</WidgetStatus>
	{/if}
</DashboardPanel>
