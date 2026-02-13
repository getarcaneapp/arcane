<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { toast } from 'svelte-sonner';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { Badge } from '$lib/components/ui/badge';
	import { tryCatch } from '$lib/utils/try-catch';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { formatDistanceToNow } from 'date-fns';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
	import type { Event } from '$lib/types/event.type';
	import type { ColumnSpec, MobileFieldVisibility } from '$lib/components/arcane-table';
	import { UniversalMobileCard } from '$lib/components/arcane-table';
	import EventDetailsDialog from '$lib/components/dialogs/event-details-dialog.svelte';
	import { m } from '$lib/paraglide/messages';
	import { eventService } from '$lib/services/event-service';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { TrashIcon, InfoIcon, NotificationsIcon, TagIcon, EnvironmentsIcon, UserIcon, EllipsisIcon } from '$lib/icons';

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
	let detailsOpen = $state(false);
	let detailsEvent = $state<Event | null>(null);

	async function refreshEvents(options: SearchPaginationSortRequest = requestOptions) {
		if (onRefreshData) {
			await onRefreshData(options);
			return;
		}
		events = await eventService.getEvents(options);
	}

	function getSeverityBadgeVariant(severity: string) {
		switch (severity) {
			case 'success':
				return 'green';
			case 'error':
				return 'red';
			case 'warning':
				return 'amber';
			case 'info':
			default:
				return 'blue';
		}
	}

	function getIconVariant(severity: string): 'emerald' | 'red' | 'amber' | 'blue' {
		const variant = getSeverityBadgeVariant(severity);
		if (variant === 'green') return 'emerald';
		if (variant === 'red') return 'red';
		if (variant === 'amber') return 'amber';
		return 'blue';
	}

	function formatTimestamp(timestamp: string) {
		const date = new Date(timestamp);
		return formatDistanceToNow(date, { addSuffix: true });
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

	function openDetails(e: Event) {
		detailsEvent = e;
		detailsOpen = true;
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
	<StatusBadge text={String(value ?? '')} variant={getSeverityBadgeVariant(String(value ?? 'info'))} />
{/snippet}

{#snippet TypeCell({ value }: { value: unknown })}
	<Badge variant="outline">{String(value ?? '-')}</Badge>
{/snippet}

{#snippet ResourceCell({ item }: { item: Event })}
	{#if item.resourceType}
		<div class="space-y-1">
			<span class="text-sm">{item.resourceType}</span>
			{#if item.resourceName}
				<p class="text-muted-foreground text-xs">{item.resourceName}</p>
			{/if}
		</div>
	{:else}
		-
	{/if}
{/snippet}

{#snippet EnvironmentCell({ item }: { item: Event })}
	{#if item.environmentId}
		<Badge variant="outline"
			>{environmentStore.available.find((e) => e.id === item.environmentId)?.name || item.environmentId}</Badge
		>
	{:else}
		-
	{/if}
{/snippet}

{#snippet UserCell({ value }: { value: unknown })}
	{#if String(value ?? '') === 'System'}
		<span class="text-red-500/50">{String(value ?? '-')}</span>
	{:else}
		{String(value ?? '-')}
	{/if}
{/snippet}

{#snippet TimeCell({ value }: { value: unknown })}
	<span class="text-sm">{formatTimestamp(String(value ?? new Date().toISOString()))}</span>
{/snippet}

{#snippet EventMobileCardSnippet({ item, mobileFieldVisibility }: { item: Event; mobileFieldVisibility: MobileFieldVisibility })}
	<UniversalMobileCard
		{item}
		icon={(item: Event) => ({
			component: NotificationsIcon,
			variant: getIconVariant(item.severity)
		})}
		title={(item: Event) => item.title}
		subtitle={(item: Event) => ((mobileFieldVisibility.timestamp ?? true) ? formatTimestamp(item.timestamp) : null)}
		badges={[
			(item: Event) =>
				(mobileFieldVisibility.severity ?? true)
					? {
							variant: getSeverityBadgeVariant(item.severity),
							text: item.severity
						}
					: null
		]}
		fields={[
			{
				label: m.common_type(),
				getValue: (item: Event) => item.type,
				icon: TagIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility.type ?? true
			},
			{
				label: m.events_col_resource(),
				getValue: (item: Event) => {
					if (!item.resourceType && !item.resourceName) return null;
					const parts = [item.resourceType || '-'];
					if (item.resourceName) parts.push(item.resourceName);
					return parts.join(' - ');
				},
				icon: EnvironmentsIcon,
				iconVariant: 'gray' as const,
				show: (mobileFieldVisibility.resource ?? true) && (!!item.resourceType || !!item.resourceName)
			},
			{
				label: m.events_environment_label(),
				getValue: (item: Event) => {
					if (!item.environmentId) return null;
					return environmentStore.available.find((e) => e.id === item.environmentId)?.name || item.environmentId;
				},
				icon: EnvironmentsIcon,
				iconVariant: 'gray' as const,
				show: (mobileFieldVisibility.environment ?? true) && !!item.environmentId
			},
			{
				label: m.common_user(),
				getValue: (item: Event) => item.username,
				icon: UserIcon,
				iconVariant: 'gray' as const,
				show: (mobileFieldVisibility.username ?? true) && !!item.username
			}
		]}
		rowActions={RowActions}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: Event })}
	<DropdownMenu.Root>
		<DropdownMenu.Trigger>
			{#snippet child({ props })}
				<ArcaneButton {...props} action="base" tone="ghost" size="icon" class="size-8">
					<span class="sr-only">{m.common_open_menu()}</span>
					<EllipsisIcon class="size-4" />
				</ArcaneButton>
			{/snippet}
		</DropdownMenu.Trigger>
		<DropdownMenu.Content align="end">
			<DropdownMenu.Group>
				<DropdownMenu.Item onclick={() => openDetails(item)}>
					<InfoIcon class="size-4" />
					{m.common_view_details()}
				</DropdownMenu.Item>

				<DropdownMenu.Separator />

				<DropdownMenu.Item
					variant="destructive"
					onclick={() => handleDeleteEvent(item.id, item.title)}
					disabled={isLoading.removing}
				>
					<TrashIcon class="size-4" />
					{m.common_delete()}
				</DropdownMenu.Item>
			</DropdownMenu.Group>
		</DropdownMenu.Content>
	</DropdownMenu.Root>
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
/>

<EventDetailsDialog bind:open={detailsOpen} event={detailsEvent} />
