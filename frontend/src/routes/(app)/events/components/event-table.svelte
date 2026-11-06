<script lang="ts">
	import { useQueryClient } from '@tanstack/svelte-query';
	import type { Snippet } from 'svelte';
	import { toast } from 'svelte-sonner';

	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import RelativeTimeCell from '#lib/components/arcane-table/cells/relative-time-cell.svelte';
	import type { ColumnSpec, MobileFieldVisibility } from '#lib/components/arcane-table/index.js';
	import { UniversalMobileCard } from '#lib/components/arcane-table/index.js';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import EventDetailPanel from '#lib/components/events/event-detail-panel.svelte';
	import {
		eventSeverityIconVariant,
		eventSeverityLabel,
		eventSeverityVariant,
		eventTypeFilters,
		eventTypeIcon,
		eventTypeLabel
	} from '#lib/components/events/events-labels.js';
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import { EmptyState } from '#lib/components/states/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { TrashIcon, NotificationsIcon, TagIcon, EnvironmentsIcon, UserIcon, EventsIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { eventService } from '#lib/services/event-service.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import type { Paginated } from '#lib/types/base.js';
	import type { Event } from '#lib/types/event.js';
	import { GLOBAL_SCOPE } from '#lib/types/role.js';
	import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { formatRelativeTime } from '#lib/utils/formatting.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	let {
		events,
		selectedIds = $bindable(),
		requestOptions = $bindable()
	}: {
		events: Paginated<Event>;
		selectedIds: string[];
		requestOptions: SearchPaginationSortRequest;
	} = $props();

	const queryClient = useQueryClient();
	const isLoading = $state({ removing: false });

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
					await handleApiResultWithCallbacks({
						result: await tryCatch(eventService.delete(eventId)),
						message: m.events_delete_failed({ title: safeTitle }),
						setLoadingState: (value) => (isLoading.removing = value),
						onSuccess: async () => {
							toast.success(m.events_delete_success({ title: safeTitle }));
							await queryClient.invalidateQueries({ queryKey: queryKeys.events.all });
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
			id: 'environment',
			title: m.resource_environment_cap(),
			width: 'min',
			cell: EnvironmentCell
		},
		{
			accessorKey: 'title',
			title: m.events_col_event(),
			cell: SummaryCell
		},
		{
			accessorKey: 'timestamp',
			title: m.events_col_time(),
			sortable: true,
			width: 'min',
			cellComponent: RelativeTimeCell
		},
		{
			accessorKey: 'severity',
			title: m.events_col_severity(),
			sortable: true,
			width: 'min',
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
			title: m.resource(),
			cell: ResourceCell
		},
		{
			accessorKey: 'username',
			title: m.common_user(),
			sortable: true,
			width: 'min',
			cell: UserCell
		}
	] satisfies ColumnSpec<Event>[];

	const mobileFields = [
		{ id: 'timestamp', label: m.events_col_time(), defaultVisible: true },
		{ id: 'severity', label: m.events_col_severity(), defaultVisible: true },
		{ id: 'type', label: m.common_type(), defaultVisible: true },
		{ id: 'environment', label: m.resource_environment_cap(), defaultVisible: true },
		{ id: 'resource', label: m.resource(), defaultVisible: true },
		{ id: 'username', label: m.common_user(), defaultVisible: true }
	];

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
</script>

{#snippet SummaryCell({ item }: { item: Event })}
	<p class="line-clamp-1 max-w-event-summary min-w-48 text-sm font-medium break-words whitespace-normal" title={item.title}>
		{item.title}
	</p>
{/snippet}

{#snippet SeverityCell({ value }: { value: unknown })}
	{@const severity = String(value ?? 'info')}
	<Badge variant={eventSeverityVariant(severity)} minWidth="20">{eventSeverityLabel(severity)}</Badge>
{/snippet}

{#snippet TypeCell({ value }: { value: unknown })}
	{@const type = String(value ?? '')}
	{@const TypeIcon = eventTypeIcon(type)}
	<div class="flex min-w-0 items-center gap-2">
		<TypeIcon class="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
		<span class="text-sm whitespace-nowrap" title={type}>{eventTypeLabel(type)}</span>
	</div>
{/snippet}

{#snippet ResourceCell({ item }: { item: Event })}
	{#if item.resourceName || item.resourceType}
		<div class="min-w-0">
			<p class="truncate text-sm" title={item.resourceName}>{item.resourceName ?? '—'}</p>
			{#if item.resourceType}
				<p class="text-xs text-muted-foreground capitalize">{item.resourceType}</p>
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
		<span class="text-sm text-muted-foreground italic">{m.system()}</span>
	{:else}
		<span class="text-sm">{String(value ?? '—')}</span>
	{/if}
{/snippet}

{#snippet ExpandedEvent({ item }: { item: Event })}
	<EventDetailPanel event={item} />
{/snippet}

{#snippet EventMobileCardSnippet({
	item,
	mobileFieldVisibility,
	expander
}: {
	item: Event;
	mobileFieldVisibility: MobileFieldVisibility;
	expander?: Snippet;
})}
	<UniversalMobileCard
		{item}
		{expander}
		icon={(item: Event) => ({
			component: NotificationsIcon,
			variant: eventSeverityIconVariant(item.severity)
		})}
		title={(item: Event) => item.title}
		subtitle={(item: Event) => ((mobileFieldVisibility['timestamp'] ?? true) ? formatRelativeTime(item.timestamp) : null)}
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
				label: m.resource(),
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
				label: m.resource_environment_cap(),
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
	<IfPermitted perm="events:delete" envId={GLOBAL_SCOPE}>
		<RowActionsMenu name={item.title}>
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
	selectionDisabled={!hasPermission('events:delete', GLOBAL_SCOPE)}
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={EventMobileCardSnippet}
	expandedRowContent={ExpandedEvent}
>
	{#snippet emptyState()}
		<EmptyState icon={EventsIcon} title={m.no_events_yet()} description={m.events_appear_as_they_happen()} />
	{/snippet}
</ArcaneTable>
