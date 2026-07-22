<script lang="ts">
	import { toast } from 'svelte-sonner';
	import EventTable from './event-table.svelte';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { m } from '$lib/paraglide/messages';
	import { eventService } from '$lib/services/event-service';
	import { queryKeys } from '$lib/query/query-keys';
	import { untrack } from 'svelte';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index.js';
	import { createMutation, createQuery } from '@tanstack/svelte-query';
	import { AlertIcon, CheckIcon, CloseIcon, EventsIcon, InfoIcon } from '$lib/icons';
	import { hasPermission } from '$lib/utils/auth';

	let { data } = $props();

	let events = $state(untrack(() => data.events));
	let selectedIds = $state<string[]>([]);
	let requestOptions = $state(untrack(() => data.eventRequestOptions));

	const eventsQuery = createQuery(() => ({
		queryKey: queryKeys.events.listGlobal(requestOptions),
		queryFn: () => eventService.getEvents(requestOptions),
		initialData: data.events
	}));

	const statsQuery = createQuery(() => ({
		queryKey: queryKeys.events.statsGlobal(),
		queryFn: () => eventService.getEventStats(),
		initialData: data.eventStats
	}));

	const deleteSelectedMutation = createMutation(() => ({
		mutationKey: queryKeys.events.deleteSelectedGlobal(),
		mutationFn: async (ids: string[]) => {
			let successCount = 0;
			let failureCount = 0;

			for (const eventId of ids) {
				try {
					await eventService.delete(eventId);
					successCount += 1;
				} catch {
					failureCount += 1;
				}
			}

			return { successCount, failureCount };
		},
		onSuccess: async ({ successCount, failureCount }) => {
			if (successCount > 0) {
				toast.success(m.common_bulk_delete_success({ count: successCount, resource: m.events_title() }));
				await refresh();
			}
			if (failureCount > 0) {
				toast.error(m.common_bulk_delete_failed({ count: failureCount, resource: m.events_title() }));
			}
			selectedIds = [];
		}
	}));

	$effect(() => {
		if (eventsQuery.data) {
			events = eventsQuery.data;
		}
	});

	const counts = $derived(statsQuery.data);
	const isRefreshing = $derived(eventsQuery.isFetching && !eventsQuery.isPending);

	async function refresh() {
		await Promise.all([eventsQuery.refetch(), statsQuery.refetch()]);
	}

	const activeSeverities = $derived.by(() => {
		const value = requestOptions.filters?.['severity'];
		if (Array.isArray(value)) {
			return value.map(String);
		}
		return value ? [String(value)] : [];
	});

	function toggleSeverityFilter(severity: string) {
		const next = activeSeverities.includes(severity)
			? activeSeverities.filter((s) => s !== severity)
			: [...activeSeverities, severity];
		const filters = { ...requestOptions.filters };
		if (next.length) {
			filters['severity'] = next;
		} else {
			delete filters['severity'];
		}
		requestOptions = {
			...requestOptions,
			filters: Object.keys(filters).length ? filters : undefined,
			pagination: { page: 1, limit: requestOptions.pagination?.limit ?? 20 }
		};
	}

	async function handleDeleteSelected() {
		if (selectedIds.length === 0) return;

		openConfirmDialog({
			title: m.events_delete_selected_title({ count: selectedIds.length }),
			message: m.events_delete_selected_message({ count: selectedIds.length }),
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: async () => {
					await deleteSelectedMutation.mutateAsync([...selectedIds]);
				}
			}
		});
	}

	const canManageEvents = $derived(hasPermission('events:delete'));

	const actionButtons: ActionButton[] = $derived([
		...(selectedIds.length > 0 && canManageEvents
			? [
					{
						id: 'remove-selected',
						action: 'remove' as const,
						label: m.common_remove_selected(),
						onclick: handleDeleteSelected,
						loading: deleteSelectedMutation.isPending,
						disabled: deleteSelectedMutation.isPending
					}
				]
			: []),
		{
			id: 'refresh',
			action: 'restart' as const,
			label: m.common_refresh(),
			onclick: refresh,
			loading: isRefreshing,
			disabled: isRefreshing
		}
	]);

	const statCards: StatCardConfig[] = $derived([
		{
			title: m.events_total(),
			value: counts?.total ?? 0,
			icon: EventsIcon
		},
		{
			title: m.info(),
			value: counts?.info ?? 0,
			icon: InfoIcon,
			iconColor: 'text-blue-500',
			onclick: () => toggleSeverityFilter('info'),
			active: activeSeverities.includes('info')
		},
		{
			title: m.common_success(),
			value: counts?.success ?? 0,
			icon: CheckIcon,
			iconColor: 'text-green-500',
			onclick: () => toggleSeverityFilter('success'),
			active: activeSeverities.includes('success')
		},
		{
			title: m.warning(),
			value: counts?.warning ?? 0,
			icon: AlertIcon,
			iconColor: 'text-yellow-500',
			onclick: () => toggleSeverityFilter('warning'),
			active: activeSeverities.includes('warning')
		},
		{
			title: m.common_error(),
			value: counts?.error ?? 0,
			icon: CloseIcon,
			iconColor: 'text-red-500',
			onclick: () => toggleSeverityFilter('error'),
			active: activeSeverities.includes('error')
		}
	]);
</script>

<ResourcePageLayout title={m.events_title()} subtitle={m.events_subtitle()} {actionButtons} {statCards}>
	{#snippet mainContent()}
		<EventTable
			bind:events
			bind:selectedIds
			bind:requestOptions
			onRefreshData={async (options) => {
				requestOptions = options;
				await refresh();
			}}
		/>
	{/snippet}
</ResourcePageLayout>
