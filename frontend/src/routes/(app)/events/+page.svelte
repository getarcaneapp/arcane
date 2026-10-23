<script lang="ts">
	import { createQuery, keepPreviousData } from '@tanstack/svelte-query';
	import { tick } from 'svelte';

	import { AlertIcon, CheckIcon, CloseIcon, EventsIcon, InfoIcon } from '#lib/icons/index.js';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { eventService } from '#lib/services/event-service.js';
	import { GLOBAL_SCOPE } from '#lib/types/auth.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { bulkConfirmAndRun } from '#lib/utils/bulk-actions.js';

	import EventTable from './components/event-table.svelte';

	let { data } = $props();

	let selectedIds = $state<string[]>([]);
	let requestOptions = $derived(data.eventRequestOptions);
	let isDeleting = $state(false);

	// The load already cached the first page under its key; seeding every key with it would hide other pages and filters.
	const eventsQuery = createQuery(() => {
		const options = requestOptions;
		return {
			queryKey: queryKeys.events.listGlobal(options),
			queryFn: () => eventService.getEvents(options),
			placeholderData: keepPreviousData
		};
	});

	const statsQuery = createQuery(() => ({
		queryKey: queryKeys.events.statsGlobal(),
		queryFn: () => eventService.getEventStats(),
		initialData: data.eventStats
	}));

	const events = $derived(eventsQuery.data ?? data.events);
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

	function handleDeleteSelected() {
		if (selectedIds.length === 0) return;
		const ids = [...selectedIds];

		bulkConfirmAndRun({
			ids,
			title: m.events_delete_selected_title({ count: ids.length }),
			message: m.events_delete_selected_message({ count: ids.length }),
			confirmLabel: m.common_delete(),
			destructive: true,
			run: (eventId) => eventService.delete(eventId),
			messages: {
				success: (count) => m.common_bulk_delete_success({ count, resource: m.events_title() }),
				partial: (success, total, failed) => m.common_bulk_delete_partial({ success, total, failed, resource: m.events_title() }),
				failure: () => m.common_bulk_delete_failed({ count: ids.length, resource: m.events_title() })
			},
			setLoading: (loading) => (isDeleting = loading),
			onComplete: async ({ success }) => {
				if (success > 0) await refresh();
			},
			clearSelection: () => (selectedIds = []),
			sequential: true
		});
	}

	const canManageEvents = $derived(hasPermission('events:delete', GLOBAL_SCOPE));

	const actionButtons: ActionButton[] = $derived([
		...(selectedIds.length > 0 && canManageEvents
			? [
					{
						id: 'remove-selected',
						action: 'remove' as const,
						group: 'danger' as const,
						destructive: true,
						label: m.common_remove_selected(),
						onclick: handleDeleteSelected,
						loading: isDeleting,
						disabled: isDeleting
					}
				]
			: []),
		{
			id: 'refresh',
			action: 'refresh' as const,
			placement: 'secondary',
			iconOnly: true,
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
			iconColor: 'text-info',
			onclick: () => toggleSeverityFilter('info'),
			active: activeSeverities.includes('info')
		},
		{
			title: m.common_success(),
			value: counts?.success ?? 0,
			icon: CheckIcon,
			iconColor: 'text-success',
			onclick: () => toggleSeverityFilter('success'),
			active: activeSeverities.includes('success')
		},
		{
			title: m.warning(),
			value: counts?.warning ?? 0,
			icon: AlertIcon,
			iconColor: 'text-warning',
			onclick: () => toggleSeverityFilter('warning'),
			active: activeSeverities.includes('warning')
		},
		{
			title: m.common_error(),
			value: counts?.error ?? 0,
			icon: CloseIcon,
			iconColor: 'text-destructive',
			onclick: () => toggleSeverityFilter('error'),
			active: activeSeverities.includes('error')
		}
	]);
</script>

<ResourcePageLayout title={m.events_title()} subtitle={m.events_subtitle()} {actionButtons} {statCards}>
	{#snippet mainContent()}
		<EventTable
			{events}
			bind:selectedIds
			bind:requestOptions
			onRefreshData={async (options) => {
				requestOptions = options;
				// Let the query switch to the new options first; the refetch then joins its fetch.
				await tick();
				await Promise.all([eventsQuery.refetch({ cancelRefetch: false }), statsQuery.refetch()]);
			}}
		/>
	{/snippet}
</ResourcePageLayout>
