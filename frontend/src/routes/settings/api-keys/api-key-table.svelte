<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';
	import EllipsisIcon from '@lucide/svelte/icons/ellipsis';
	import EditIcon from '@lucide/svelte/icons/edit';
	import CopyIcon from '@lucide/svelte/icons/copy';
	import { toast } from 'svelte-sonner';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
	import type { ApiKey } from '$lib/types/api-key.type';
	import type { ColumnSpec } from '$lib/components/arcane-table';
	import { UniversalMobileCard } from '$lib/components/arcane-table';
	import { apiKeyService } from '$lib/services/api-key-service';
	import KeyIcon from '@lucide/svelte/icons/key';
	import * as m from '$lib/paraglide/messages.js';

	let {
		apiKeys = $bindable(),
		selectedIds = $bindable(),
		requestOptions = $bindable(),
		onApiKeysChanged,
		onEditApiKey
	}: {
		apiKeys: Paginated<ApiKey>;
		selectedIds: string[];
		requestOptions: SearchPaginationSortRequest;
		onApiKeysChanged: () => Promise<void>;
		onEditApiKey: (apiKey: ApiKey) => void;
	} = $props();

	let isLoading = $state({
		removing: false
	});

	function formatDate(dateString?: string): string {
		if (!dateString) return '-';
		return new Date(dateString).toLocaleString();
	}

	function isExpired(expiresAt?: string): boolean {
		if (!expiresAt) return false;
		return new Date(expiresAt) < new Date();
	}

	function getStatusText(apiKey: ApiKey): string {
		if (isExpired(apiKey.expiresAt)) return m.api_key_status_expired();
		return m.api_key_status_active();
	}

	function getStatusVariant(apiKey: ApiKey): 'red' | 'green' {
		if (isExpired(apiKey.expiresAt)) return 'red';
		return 'green';
	}

	async function copyToClipboard(text: string) {
		try {
			await navigator.clipboard.writeText(text);
			toast.success(m.common_copied());
		} catch {
			toast.error(m.common_copy_failed());
		}
	}

	async function handleDeleteSelected() {
		if (selectedIds.length === 0) return;

		openConfirmDialog({
			title: m.api_key_delete_selected_title({ count: selectedIds.length }),
			message: m.api_key_delete_selected_message({ count: selectedIds.length }),
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: async () => {
					isLoading.removing = true;
					let successCount = 0;
					let failureCount = 0;

					for (const apiKeyId of selectedIds) {
						const result = await tryCatch(apiKeyService.delete(apiKeyId));
						handleApiResultWithCallbacks({
							result,
							message: m.api_key_delete_failed({ name: apiKeyId }),
							setLoadingState: () => {},
							onSuccess: () => {
								successCount++;
							}
						});

						if (result.error) {
							failureCount++;
						}
					}

					isLoading.removing = false;

					if (successCount > 0) {
						toast.success(m.api_key_bulk_delete_success({ count: successCount }));
						await onApiKeysChanged();
					}

					if (failureCount > 0) {
						toast.error(m.api_key_bulk_delete_failed({ count: failureCount }));
					}

					selectedIds = [];
				}
			}
		});
	}

	async function handleDeleteApiKey(apiKeyId: string, name: string) {
		const safeName = name?.trim() || m.common_unknown();
		openConfirmDialog({
			title: m.api_key_delete_title({ name: safeName }),
			message: m.api_key_delete_message({ name: safeName }),
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: async () => {
					isLoading.removing = true;
					handleApiResultWithCallbacks({
						result: await tryCatch(apiKeyService.delete(apiKeyId)),
						message: m.api_key_delete_failed({ name: safeName }),
						setLoadingState: (value) => (isLoading.removing = value),
						onSuccess: async () => {
							toast.success(m.api_key_delete_success({ name: safeName }));
							await onApiKeysChanged();
						}
					});
				}
			}
		});
	}

	const columns = [
		{ accessorKey: 'name', title: m.api_key_name(), sortable: true, cell: NameCell },
		{ accessorKey: 'description', title: m.api_key_description_label(), sortable: false, cell: DescriptionCell },
		{ accessorKey: 'keyPrefix', title: m.api_key_key_prefix(), sortable: false, cell: KeyPrefixCell },
		{ accessorKey: 'expiresAt', title: m.api_key_expires_at(), sortable: true, cell: ExpiresCell },
		{ accessorKey: 'lastUsedAt', title: m.api_key_last_used(), sortable: true, cell: LastUsedCell }
	] satisfies ColumnSpec<ApiKey>[];

	const mobileFields = [
		{ id: 'description', label: m.api_key_description_label(), defaultVisible: true },
		{ id: 'keyPrefix', label: m.api_key_key_prefix(), defaultVisible: true },
		{ id: 'expiresAt', label: m.api_key_expires_at(), defaultVisible: true },
		{ id: 'lastUsedAt', label: m.api_key_last_used(), defaultVisible: true }
	];

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
</script>

{#snippet NameCell({ item }: { item: ApiKey })}
	<span class="font-medium">{item.name}</span>
{/snippet}

{#snippet DescriptionCell({ item }: { item: ApiKey })}
	<span class="text-muted-foreground">{item.description || '-'}</span>
{/snippet}

{#snippet KeyPrefixCell({ item }: { item: ApiKey })}
	<div class="flex items-center gap-2">
		<code class="bg-muted rounded px-2 py-1 text-xs">{item.keyPrefix}...</code>
		<Button variant="ghost" size="icon" class="size-6" onclick={() => copyToClipboard(item.keyPrefix)}>
			<CopyIcon class="size-3" />
		</Button>
	</div>
{/snippet}

{#snippet ExpiresCell({ item }: { item: ApiKey })}
	<div class="flex items-center gap-2">
		{#if item.expiresAt}
			<span class={isExpired(item.expiresAt) ? 'text-red-500' : ''}>{formatDate(item.expiresAt)}</span>
		{:else}
			<span class="text-muted-foreground">{m.api_key_expires_never()}</span>
		{/if}
		<StatusBadge text={getStatusText(item)} variant={getStatusVariant(item)} />
	</div>
{/snippet}

{#snippet LastUsedCell({ item }: { item: ApiKey })}
	{formatDate(item.lastUsedAt)}
{/snippet}

{#snippet ApiKeyMobileCardSnippet({
	row,
	item,
	mobileFieldVisibility
}: {
	row: any;
	item: ApiKey;
	mobileFieldVisibility: Record<string, boolean>;
})}
	<UniversalMobileCard
		{item}
		icon={{ component: KeyIcon, variant: 'blue' }}
		title={(item: ApiKey) => item.name}
		subtitle={(item: ApiKey) => ((mobileFieldVisibility.keyPrefix ?? true) ? `${item.keyPrefix}...` : null)}
		badges={[
			(item: ApiKey) => ({
				variant: getStatusVariant(item),
				text: getStatusText(item)
			})
		]}
		fields={[
			{
				label: m.api_key_description_label(),
				getValue: (item: ApiKey) => item.description || '-',
				icon: KeyIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility.description ?? true
			},
			{
				label: m.api_key_expires_at(),
				getValue: (item: ApiKey) => (item.expiresAt ? formatDate(item.expiresAt) : m.api_key_expires_never()),
				icon: KeyIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility.expiresAt ?? true
			},
			{
				label: m.api_key_last_used(),
				getValue: (item: ApiKey) => formatDate(item.lastUsedAt),
				icon: KeyIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility.lastUsedAt ?? true
			}
		]}
		rowActions={RowActions}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: ApiKey })}
	<DropdownMenu.Root>
		<DropdownMenu.Trigger>
			{#snippet child({ props })}
				<Button {...props} variant="ghost" size="icon" class="relative size-8 p-0">
					<span class="sr-only">{m.common_open_menu()}</span>
					<EllipsisIcon />
				</Button>
			{/snippet}
		</DropdownMenu.Trigger>
		<DropdownMenu.Content align="end">
			<DropdownMenu.Group>
				<DropdownMenu.Item onclick={() => onEditApiKey(item)}>
					<EditIcon class="size-4" />
					{m.common_edit()}
				</DropdownMenu.Item>
				<DropdownMenu.Item variant="destructive" onclick={() => handleDeleteApiKey(item.id, item.name)}>
					<Trash2Icon class="size-4" />
					{m.common_delete()}
				</DropdownMenu.Item>
			</DropdownMenu.Group>
		</DropdownMenu.Content>
	</DropdownMenu.Root>
{/snippet}

<ArcaneTable
	persistKey="arcane-api-keys-table"
	items={apiKeys}
	bind:requestOptions
	bind:selectedIds
	bind:mobileFieldVisibility
	onRemoveSelected={(ids) => handleDeleteSelected()}
	onRefresh={async (options) => {
		requestOptions = options;
		await onApiKeysChanged();
		return apiKeys;
	}}
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={ApiKeyMobileCardSnippet}
/>
