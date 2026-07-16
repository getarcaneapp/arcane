<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import RowActionsMenu from '$lib/components/arcane-table/row-actions-menu.svelte';
	import { toast } from 'svelte-sonner';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { Badge } from '$lib/components/ui/badge';
	import { tryCatch } from '$lib/utils/api';
	import { handleApiResultWithCallbacks } from '$lib/utils/api';
	import { formatDistanceToNow } from 'date-fns';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/shared';
	import type { Event } from '$lib/types/shared';
	import type { ColumnSpec, MobileFieldVisibility } from '$lib/components/arcane-table';
	import { UniversalMobileCard } from '$lib/components/arcane-table';
	import RelativeTimeCell from '$lib/components/arcane-table/cells/relative-time-cell.svelte';
	import EventDetailPanel from '$lib/components/events/event-detail-panel.svelte';
	import {
		eventSeverityIconVariant,
		eventSeverityLabel,
		eventSeverityVariant,
		eventTypeFilters,
		eventTypeIcon,
		eventTypeLabel
	} from '$lib/components/events/events-labels';
	import { m } from '$lib/paraglide/messages';
	import { eventService } from '$lib/services/event-service';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import IfPermitted from '$lib/components/if-permitted.svelte';
	import { TrashIcon, NotificationsIcon, TagIcon, EnvironmentsIcon, UserIcon } from '$lib/icons';

	let {
		events = $bindable(),
		selectedIds = $bindable(),
		requestOptions = $bindable(),
		onRefreshData
	}: {
		events: Paginated<Event>;
		selectedIds: string[];
		requestOptions: SearchPaginationSortRequest;
		onRefreshData?: (options: SearchPaginationSortRequest) => Promise<void>;
	} = $props();

	let isLoading = $state({ removing: false });

	async function refreshEvents(options: SearchPaginationSortRequest = requestOptions) {
		if (onRefreshData) {
			await onRefreshData(options);
			return;
		}
		events = await eventService.getEvents(options);
	}

	async function handleDeleteEvent(eventId: string, title: string) {
		const safeTitle = title?.trim() || m.common_unknown();
		openConfirmDialog({
			title: m.common_delete_title({ resource: 'event' }),
			message: m.events_delete_confirm_message({ title: safeTitle }),
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: async () => {
					isLoading.removing = true;
					handleApiResultWithCallbacks({
						result: await tryCatch(eventService.delete(eventId)),
						message: m.events_delete_failed({ title: safeTitle }),
						setLoadingState: (value) => (isLoading.removing = value),
						onSuccess: async () => {
							toast.success(m.events_delete_success({ title: safeTitle }));
							await refreshEvents();
						}
					});
				}
			}
		});
	}

	function environmentName(environmentId: string): string {
		return environmentStore.available.find((e) => e.id === environmentId)?.name || environmentId;
	}

	const columns = [
		{
			accessorKey: 'severity',
			title: m.events_col_severity(),
			sortable: true,
			cell: SeverityCell
		},
		{
			accessorKey: 'type',
			title: m.common_type(),
			sortable: true,
			filterOptions: eventTypeFilters,
			cell: TypeCell
		},
		{
			id: 'resource',
			title: m.events_col_resource(),
			cell: ResourceCell
		},
		{
			id: 'environment',
			title: m.events_environment_label(),
			cell: EnvironmentCell
		},
		{
			accessorKey: 'username',
			title: m.common_user(),
			sortable: true,
			cell: UserCell
		},
		{
			accessorKey: 'timestamp',
			title: m.events_col_time(),
			sortable: true,
			cell: TimeCell
		}
	] satisfies ColumnSpec<Event>[];

	const mobileFields = [
		{ id: 'severity', label: m.events_col_severity(), defaultVisible: true },
		{ id: 'type', label: m.common_type(), defaultVisible: true },
		{ id: 'resource', label: m.events_col_resource(), defaultVisible: true },
		{ id: 'environment', label: m.events_environment_label(), defaultVisible: true },
		{ id: 'username', label: m.common_user(), defaultVisible: true },
		{ id: 'timestamp', label: m.events_col_time(), defaultVisible: true }
	];

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
</script>

{#snippet SeverityCell({ value }: { value: unknown })}
	{@const severity = String(value ?? 'info')}
	<Badge variant={eventSeverityVariant(severity)} minWidth="20">{eventSeverityLabel(severity)}</Badge>
{/snippet}

{#snippet TypeCell({ value }: { value: unknown })}
	{@const type = String(value ?? '')}
	{@const TypeIcon = eventTypeIcon(type)}
	<div class="flex min-w-0 items-center gap-2">
		<TypeIcon class="text-muted-foreground size-4 shrink-0" aria-hidden="true" />
		<span class="truncate text-sm" title={type}>{eventTypeLabel(type)}</span>
	</div>
{/snippet}

{#snippet ResourceCell({ item }: { item: Event })}
	{#if item.resourceName || item.resourceType}
		<div class="min-w-0">
			<p class="truncate text-sm" title={item.resourceName}>{item.resourceName ?? '—'}</p>
			{#if item.resourceType}
				<p class="text-muted-foreground text-xs capitalize">{item.resourceType}</p>
			{/if}
		</div>
	{:else}
		<span class="text-muted-foreground">—</span>
	{/if}
{/snippet}

{#snippet EnvironmentCell({ item }: { item: Event })}
	{#if item.environmentId}
		<Badge variant="gray">{environmentName(item.environmentId)}</Badge>
	{:else}
		<span class="text-muted-foreground">—</span>
	{/if}
{/snippet}

{#snippet UserCell({ value }: { value: unknown })}
	{#if String(value ?? '') === 'System'}
		<span class="text-muted-foreground text-sm italic">System</span>
	{:else}
		<span class="text-sm">{String(value ?? '—')}</span>
	{/if}
{/snippet}

{#snippet TimeCell({ value }: { value: unknown })}
	<RelativeTimeCell {value} />
{/snippet}

{#snippet ExpandedEvent({ item }: { item: Event })}
	<EventDetailPanel event={item} />
{/snippet}

{#snippet EventMobileCardSnippet({ item, mobileFieldVisibility }: { item: Event; mobileFieldVisibility: MobileFieldVisibility })}
	<UniversalMobileCard
		{item}
		icon={(item: Event) => ({
			component: NotificationsIcon,
			variant: eventSeverityIconVariant(item.severity)
		})}
		title={(item: Event) => item.title}
		subtitle={(item: Event) =>
			(mobileFieldVisibility['timestamp'] ?? true) ? formatDistanceToNow(new Date(item.timestamp), { addSuffix: true }) : null}
		badges={[
			(item: Event) =>
				(mobileFieldVisibility['severity'] ?? true)
					? {
							variant: eventSeverityVariant(item.severity),
							text: eventSeverityLabel(item.severity)
						}
					: null
		]}
		fields={[
			{
				label: m.common_type(),
				getValue: (item: Event) => eventTypeLabel(item.type),
				icon: TagIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['type'] ?? true
			},
			{
				label: m.events_col_resource(),
				getValue: (item: Event) => {
					if (!item.resourceType && !item.resourceName) return null;
					const parts = [item.resourceName || '—'];
					if (item.resourceType) parts.push(item.resourceType);
					return parts.join(' · ');
				},
				icon: EnvironmentsIcon,
				iconVariant: 'gray' as const,
				show: (mobileFieldVisibility['resource'] ?? true) && (!!item.resourceType || !!item.resourceName)
			},
			{
				label: m.events_environment_label(),
				getValue: (item: Event) => (item.environmentId ? environmentName(item.environmentId) : null),
				icon: EnvironmentsIcon,
				iconVariant: 'gray' as const,
				show: (mobileFieldVisibility['environment'] ?? true) && !!item.environmentId
			},
			{
				label: m.common_user(),
				getValue: (item: Event) => item.username,
				icon: UserIcon,
				iconVariant: 'gray' as const,
				show: (mobileFieldVisibility['username'] ?? true) && !!item.username
			}
		]}
		rowActions={RowActions}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: Event })}
	<IfPermitted perm="events:delete">
		<RowActionsMenu>
			<DropdownMenu.Item
				variant="destructive"
				onclick={() => handleDeleteEvent(item.id, item.title)}
				disabled={isLoading.removing}
			>
				<TrashIcon class="size-4" />
				{m.common_delete()}
			</DropdownMenu.Item>
		</RowActionsMenu>
	</IfPermitted>
{/snippet}

<ArcaneTable
	persistKey="arcane-events-table"
	items={events}
	bind:requestOptions
	bind:selectedIds
	bind:mobileFieldVisibility
	onRefresh={async (options) => {
		requestOptions = options;
		await refreshEvents(options);
		return events;
	}}
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={EventMobileCardSnippet}
	expandedRowContent={ExpandedEvent}
/>
