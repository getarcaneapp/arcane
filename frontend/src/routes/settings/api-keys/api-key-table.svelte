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
		if (isExpired(apiKey.expiresAt)) return 'Expired';
		return 'Active';
	}

	function getStatusVariant(apiKey: ApiKey): 'red' | 'green' {
		if (isExpired(apiKey.expiresAt)) return 'red';
		return 'green';
	}

	async function copyToClipboard(text: string) {
		try {
			await navigator.clipboard.writeText(text);
			toast.success('Copied to clipboard');
		} catch {
			toast.error('Failed to copy to clipboard');
		}
	}

	async function handleDeleteSelected() {
		if (selectedIds.length === 0) return;

		openConfirmDialog({
			title: `Delete ${selectedIds.length} API Key${selectedIds.length > 1 ? 's' : ''}?`,
			message: `Are you sure you want to delete ${selectedIds.length} API key${selectedIds.length > 1 ? 's' : ''}? This action cannot be undone.`,
			confirm: {
				label: 'Delete',
				destructive: true,
				action: async () => {
					isLoading.removing = true;
					let successCount = 0;
					let failureCount = 0;

					for (const apiKeyId of selectedIds) {
						const result = await tryCatch(apiKeyService.delete(apiKeyId));
						handleApiResultWithCallbacks({
							result,
							message: `Failed to delete API key ${apiKeyId}`,
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
						toast.success(`Successfully deleted ${successCount} API key${successCount > 1 ? 's' : ''}`);
						await onApiKeysChanged();
					}

					if (failureCount > 0) {
						toast.error(`Failed to delete ${failureCount} API key${failureCount > 1 ? 's' : ''}`);
					}

					selectedIds = [];
				}
			}
		});
	}

	async function handleDeleteApiKey(apiKeyId: string, name: string) {
		const safeName = name?.trim() || 'Unknown';
		openConfirmDialog({
			title: `Delete API Key "${safeName}"?`,
			message: `Are you sure you want to delete the API key "${safeName}"? This action cannot be undone.`,
			confirm: {
				label: 'Delete',
				destructive: true,
				action: async () => {
					isLoading.removing = true;
					handleApiResultWithCallbacks({
						result: await tryCatch(apiKeyService.delete(apiKeyId)),
						message: `Failed to delete API key "${safeName}"`,
						setLoadingState: (value) => (isLoading.removing = value),
						onSuccess: async () => {
							toast.success(`API key "${safeName}" deleted successfully`);
							await onApiKeysChanged();
						}
					});
				}
			}
		});
	}

	const columns = [
		{ accessorKey: 'name', title: 'Name', sortable: true, cell: NameCell },
		{ accessorKey: 'keyPrefix', title: 'Key Prefix', sortable: false, cell: KeyPrefixCell },
		{ accessorKey: 'expiresAt', title: 'Expires', sortable: true, cell: ExpiresCell },
		{ accessorKey: 'lastUsedAt', title: 'Last Used', sortable: true, cell: LastUsedCell }
	] satisfies ColumnSpec<ApiKey>[];

	const mobileFields = [
		{ id: 'keyPrefix', label: 'Key Prefix', defaultVisible: true },
		{ id: 'expiresAt', label: 'Expires', defaultVisible: true },
		{ id: 'lastUsedAt', label: 'Last Used', defaultVisible: true }
	];

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
</script>

{#snippet NameCell({ item }: { item: ApiKey })}
	<span class="font-medium">{item.name}</span>
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
			<span class="text-muted-foreground">Never</span>
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
				label: 'Expires',
				getValue: (item: ApiKey) => (item.expiresAt ? formatDate(item.expiresAt) : 'Never'),
				icon: KeyIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility.expiresAt ?? true
			},
			{
				label: 'Last Used',
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
					<span class="sr-only">Open menu</span>
					<EllipsisIcon />
				</Button>
			{/snippet}
		</DropdownMenu.Trigger>
		<DropdownMenu.Content align="end">
			<DropdownMenu.Group>
				<DropdownMenu.Item onclick={() => onEditApiKey(item)}>
					<EditIcon class="size-4" />
					Edit
				</DropdownMenu.Item>
				<DropdownMenu.Item variant="destructive" onclick={() => handleDeleteApiKey(item.id, item.name)}>
					<Trash2Icon class="size-4" />
					Delete
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
