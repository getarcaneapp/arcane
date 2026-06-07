<script lang="ts">
	import { toast } from 'svelte-sonner';
	import type { Event } from '$lib/types/event.type';
	import EventTable from './event-table.svelte';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { m } from '$lib/paraglide/messages';
	import { eventService } from '$lib/services/event-service';
	import { queryKeys } from '$lib/query/query-keys';
	import { untrack } from 'svelte';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index.js';
	import { createMutation, createQuery } from '@tanstack/svelte-query';
	import { EventsIcon } from '$lib/icons';

	let { data } = $props();

	let events = $state(untrack(() => data.events));
	let selectedIds = $state<string[]>([]);
	let requestOptions = $state(untrack(() => data.eventRequestOptions));

	const eventsQuery = createQuery(() => ({
		queryKey: queryKeys.events.listGlobal(requestOptions),
		queryFn: () => eventService.getEvents(requestOptions),
		initialData: data.events
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
				await eventsQuery.refetch();
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

	const infoEvents = $derived(events?.data?.filter((e: Event) => e.severity === 'info').length || 0);
	const warningEvents = $derived(events?.data?.filter((e: Event) => e.severity === 'warning').length || 0);
	const errorEvents = $derived(events?.data?.filter((e: Event) => e.severity === 'error').length || 0);
	const successEvents = $derived(events?.data?.filter((e: Event) => e.severity === 'success').length || 0);
	const totalEvents = $derived(events?.pagination?.totalItems || 0);
	const isRefreshing = $derived(eventsQuery.isFetching && !eventsQuery.isPending);

	async function refresh() {
		await eventsQuery.refetch();
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

	const actionButtons: ActionButton[] = $derived([
		...(selectedIds.length > 0
			? [
					{
						id: 'remove-selected',
						action: 'remove' as const,
						label: m.events_remove_selected(),
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
			value: totalEvents,
			subtitle: m.events_total_subtitle(),
			icon: EventsIcon
		},
		{
			title: m.events_info(),
			value: infoEvents,
			subtitle: m.events_info_subtitle(),
			icon: EventsIcon,
			iconColor: 'text-blue-500'
		},
		{
			title: m.events_success(),
			value: successEvents,
			subtitle: m.events_success_subtitle(),
			icon: EventsIcon,
			iconColor: 'text-green-500'
		},
		{
			title: m.events_warning(),
			value: warningEvents,
			subtitle: m.events_warning_subtitle(),
			icon: EventsIcon,
			iconColor: 'text-yellow-500'
		},
		{
			title: m.events_error(),
			value: errorEvents,
			subtitle: m.events_error_subtitle(),
			icon: EventsIcon,
			iconColor: 'text-red-500'
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
				await eventsQuery.refetch();
			}}
		/>
	{/snippet}
</ResourcePageLayout>
