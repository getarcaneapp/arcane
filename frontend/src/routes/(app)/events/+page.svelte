<script lang="ts">
	import EventTable from './event-table.svelte';
	import { m } from '#lib/paraglide/messages';
	import { eventService } from '#lib/services/event-service';
	import { queryKeys } from '#lib/query/query-keys';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '#lib/layouts/index.js';
	import { createQuery, keepPreviousData } from '@tanstack/svelte-query';
	import { AlertIcon, CheckIcon, CloseIcon, EventsIcon, InfoIcon } from '#lib/icons';
	import { hasPermission } from '#lib/utils/auth';
	import { bulkConfirmAndRun } from '#lib/utils/bulk-actions';
	import { onMount } from 'svelte';
	import { clientStream } from '#lib/stores/client-stream.svelte';
	import { STREAM_CHANNEL_EVENTS } from '#lib/services/stream-service';

	let { data } = $props();

	let events = $derived(data.events);
	let selectedIds = $state<string[]>([]);
	let requestOptions = $derived(data.eventRequestOptions);
	let isDeleting = $state(false);

	const eventsQuery = createQuery(() => ({
		queryKey: queryKeys.events.listGlobal(requestOptions),
		queryFn: () => eventService.getEvents(requestOptions),
		placeholderData: keepPreviousData,
		initialData: data.events
	}));

	const statsQuery = createQuery(() => ({
		queryKey: queryKeys.events.statsGlobal(),
		queryFn: () => eventService.getEventStats(),
		initialData: data.eventStats
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

	onMount(() => {
		let disposed = false;
		let refreshing = false;
		let refreshPending = false;

		async function refreshFromStream() {
			if (disposed) return;
			refreshPending = true;
			if (refreshing) return;
			refreshing = true;
			let alreadyFetching = eventsQuery.isFetching || statsQuery.isFetching;
			try {
				while (refreshPending && !disposed) {
					refreshPending = false;
					// An existing request may predate the notification; follow it with a fresh request.
					await Promise.all([eventsQuery.refetch({ cancelRefetch: false }), statsQuery.refetch({ cancelRefetch: false })]);
					refreshPending ||= alreadyFetching;
					alreadyFetching = false;
				}
			} finally {
				refreshing = false;
			}
		}

		const unsubscribe = clientStream.subscribe(STREAM_CHANNEL_EVENTS, {
			onConnected: () => void refreshFromStream(),
			onEvent: (event) => {
				if (typeof event === 'object' && event !== null && 'type' in event && event.type === 'changed') {
					void refreshFromStream();
				}
			}
		});

		return () => {
			disposed = true;
			unsubscribe();
		};
	});

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

	const canManageEvents = $derived(hasPermission('events:delete'));

	const actionButtons: ActionButton[] = $derived([
		...(selectedIds.length > 0 && canManageEvents
			? [
					{
						id: 'remove-selected',
						action: 'remove' as const,
						label: m.common_remove_selected(),
						onclick: handleDeleteSelected,
						loading: isDeleting,
						disabled: isDeleting
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
