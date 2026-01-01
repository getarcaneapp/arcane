<script lang="ts">
	import type { NotificationSettings } from '$lib/types/notification.type';
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { EllipsisIcon, EditIcon, TrashIcon, SendEmailIcon } from '$lib/icons';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import { toast } from 'svelte-sonner';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
	import type { ColumnSpec, MobileFieldVisibility } from '$lib/components/arcane-table';
	import { UniversalMobileCard } from '$lib/components/arcane-table';
	import { m } from '$lib/paraglide/messages';
	import { notificationService } from '$lib/services/notification-service';
	import { notificationProviders } from '$lib/types/notification.type';

	let {
		providers = $bindable(),
		selectedIds = $bindable(),
		requestOptions = $bindable(),
		onEdit,
		onTest
	}: {
		providers: Paginated<NotificationSettings>;
		selectedIds: string[];
		requestOptions: SearchPaginationSortRequest;
		onEdit: (provider: NotificationSettings) => void;
		onTest: (provider: NotificationSettings) => void;
	} = $props();

	let isLoading = $state({
		remove: false,
		testing: false
	});

	async function removeProvider(provider: string) {
		openConfirmDialog({
			title: m.common_confirm_removal_title(),
			message: m.common_confirm_removal_message({ type: 'Provider' }),
			confirm: {
				label: m.common_remove(),
				destructive: true,
				action: async () => {
					handleApiResultWithCallbacks({
						result: await tryCatch(notificationService.deleteSettings(provider)),
						message: m.common_action_failed(),
						setLoadingState: (value) => (isLoading.remove = value),
						onSuccess: async () => {
							toast.success(m.general_settings_saved());
							const data = await notificationService.getSettings();
							providers = {
								data: data,
								pagination: {
									currentPage: 1,
									totalPages: 1,
									totalItems: data.length,
									itemsPerPage: 100
								}
							};
						}
					});
				}
			}
		});
	}

	const columns = [
		{ accessorKey: 'name', title: m.common_name(), sortable: true },
		{ accessorKey: 'provider', title: m.common_type(), sortable: true, cell: ProviderCell },
		{ accessorKey: 'enabled', title: m.common_status(), sortable: true, cell: StatusCell }
	] satisfies ColumnSpec<NotificationSettings>[];

	const mobileFields = [
		{ id: 'provider', label: m.common_type(), defaultVisible: true },
		{ id: 'enabled', label: m.common_status(), defaultVisible: true }
	];

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
</script>

{#snippet ProviderCell({ value }: { value: unknown })}
	{notificationProviders[value as keyof typeof notificationProviders]?.() || value}
{/snippet}

{#snippet StatusCell({ item }: { item: NotificationSettings })}
	<StatusBadge variant={item.enabled ? 'emerald' : 'gray'} text={item.enabled ? m.common_enabled() : m.common_disabled()} />
{/snippet}

{#snippet ProviderMobileCardSnippet({
	item,
	mobileFieldVisibility
}: {
	item: NotificationSettings;
	mobileFieldVisibility: Record<string, boolean>;
})}
	<UniversalMobileCard
		{item}
		icon={() => ({
			component: SendEmailIcon,
			variant: item.enabled ? 'emerald' : 'gray'
		})}
		title={(item: NotificationSettings) => item.name}
		subtitle={(item: NotificationSettings) =>
			notificationProviders[item.provider as keyof typeof notificationProviders]?.() || item.provider}
		badges={[
			(item: NotificationSettings) =>
				(mobileFieldVisibility.enabled ?? true)
					? {
							variant: item.enabled ? 'green' : 'gray',
							text: item.enabled ? m.common_enabled() : m.common_disabled()
						}
					: null
		]}
		rowActions={RowActions}
		onclick={() => onEdit(item)}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: NotificationSettings })}
	<DropdownMenu.Root>
		<DropdownMenu.Trigger>
			{#snippet child({ props })}
				<ArcaneButton
					{...props}
					action="base"
					tone="ghost"
					size="icon"
					class="relative size-8 p-0"
					icon={EllipsisIcon}
					showLabel={false}
					customLabel={m.common_open_menu()}
				/>
			{/snippet}
		</DropdownMenu.Trigger>
		<DropdownMenu.Content align="end">
			<DropdownMenu.Group>
				<DropdownMenu.Item onclick={() => onEdit(item)}>
					<EditIcon class="size-4" />
					{m.common_edit()}
				</DropdownMenu.Item>
				<DropdownMenu.Item onclick={() => onTest(item)}>
					<SendEmailIcon class="size-4" />
					{m.notifications_email_test_button()}
				</DropdownMenu.Item>
				<DropdownMenu.Separator />
				<DropdownMenu.Item variant="destructive" onclick={() => removeProvider(item.provider)} disabled={isLoading.remove}>
					{#if isLoading.remove}
						<Spinner class="size-4" />
					{:else}
						<TrashIcon class="size-4" />
					{/if}
					{m.common_remove()}
				</DropdownMenu.Item>
			</DropdownMenu.Group>
		</DropdownMenu.Content>
	</DropdownMenu.Root>
{/snippet}

<ArcaneTable
	persistKey="arcane-notification-providers-table"
	items={providers}
	bind:requestOptions
	bind:selectedIds
	bind:mobileFieldVisibility
	onRefresh={async () => (providers = (await notificationService.getSettings()) as any)}
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={ProviderMobileCardSnippet}
/>
