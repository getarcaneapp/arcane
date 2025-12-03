<script lang="ts">
	import ActivityIcon from '@lucide/svelte/icons/activity';
	import { toast } from 'svelte-sonner';
	import { tryCatch } from '$lib/utils/try-catch';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import type { Event } from '$lib/types/event.type';
	import EventTable from './event-table.svelte';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { m } from '$lib/paraglide/messages';
	import { eventService } from '$lib/services/event-service';
	import { untrack } from 'svelte';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index.js';
	import { simpleRefresh } from '$lib/utils/refresh.util';

	let { data } = $props();

	let events = $state(untrack(() => data.events));
	let selectedIds = $state<string[]>([]);
	let requestOptions = $state(untrack(() => data.eventRequestOptions));
	let isLoading = $state({ refreshing: false, deleting: false });

	const infoEvents = $derived(events?.data?.filter((e: Event) => e.severity === 'info').length || 0);
	const warningEvents = $derived(events?.data?.filter((e: Event) => e.severity === 'warning').length || 0);
	const errorEvents = $derived(events?.data?.filter((e: Event) => e.severity === 'error').length || 0);
	const successEvents = $derived(events?.data?.filter((e: Event) => e.severity === 'success').length || 0);
	const totalEvents = $derived(events?.pagination?.totalItems || 0);

	async function refresh() {
		await simpleRefresh(
			() => eventService.getEvents(requestOptions),
			(data) => (events = data),
			m.common_refresh_failed({ resource: m.events_title() }),
			(v) => (isLoading.refreshing = v)
		);
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
					isLoading.deleting = true;
					let successCount = 0;
					let failureCount = 0;

					for (const eventId of selectedIds) {
						const result = await tryCatch(eventService.delete(eventId));
						handleApiResultWithCallbacks({
							result,
							message: m.events_delete_item_failed({ id: eventId }),
							setLoadingState: () => {},
							onSuccess: () => { successCount++; }
						});
						if (result.error) failureCount++;
					}

					isLoading.deleting = false;
					if (successCount > 0) {
						toast.success(m.common_bulk_delete_success({ count: successCount, resource: m.events_title() }));
						await refresh();
					}
					if (failureCount > 0) {
						toast.error(m.common_bulk_delete_failed({ count: failureCount, resource: m.events_title() }));
					}
					selectedIds = [];
				}
			}
		});
	}

	const actionButtons: ActionButton[] = $derived([
		...(selectedIds.length > 0
			? [{ id: 'remove-selected', action: 'remove' as const, label: m.events_remove_selected(), onclick: handleDeleteSelected, loading: isLoading.deleting, disabled: isLoading.deleting }]
			: []),
		{ id: 'refresh', action: 'restart' as const, label: m.common_refresh(), onclick: refresh, loading: isLoading.refreshing, disabled: isLoading.refreshing }
	]);

	const statCards: StatCardConfig[] = $derived([
		{ title: m.events_total(), value: totalEvents, subtitle: m.events_total_subtitle(), icon: ActivityIcon, class: 'border-l-4 border-l-primary' },
		{ title: m.events_info(), value: infoEvents, subtitle: m.events_info_subtitle(), icon: ActivityIcon, iconColor: 'text-blue-500', bgColor: 'bg-blue-500/10', class: 'border-l-4 border-l-blue-500' },
		{ title: m.events_success(), value: successEvents, subtitle: m.events_success_subtitle(), icon: ActivityIcon, iconColor: 'text-green-500', bgColor: 'bg-green-500/10', class: 'border-l-4 border-l-green-500' },
		{ title: m.events_warning(), value: warningEvents, subtitle: m.events_warning_subtitle(), icon: ActivityIcon, iconColor: 'text-yellow-500', bgColor: 'bg-yellow-500/10', class: 'border-l-4 border-l-yellow-500' },
		{ title: m.events_error(), value: errorEvents, subtitle: m.events_error_subtitle(), icon: ActivityIcon, iconColor: 'text-red-500', bgColor: 'bg-red-500/10', class: 'border-l-4 border-l-red-500' }
	]);
</script>

<ResourcePageLayout title={m.events_title()} subtitle={m.events_subtitle()} {actionButtons} {statCards} statCardsColumns={5} containerClass="flex h-full flex-col space-y-6">
	{#snippet mainContent()}
		<div class="flex-1 overflow-hidden">
			<EventTable bind:events bind:selectedIds bind:requestOptions />
		</div>
	{/snippet}
</ResourcePageLayout>
