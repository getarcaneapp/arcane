<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { goto } from '$app/navigation';
	import { toast } from 'svelte-sonner';
	import { format } from 'date-fns';
	import { truncateString } from '$lib/utils/string.utils';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import { CopyButton } from '$lib/components/ui/copy-button';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
	import type { SecretSummaryDto } from '$lib/types/secret.type';
	import type { ColumnSpec, MobileFieldVisibility, BulkAction } from '$lib/components/arcane-table';
	import { UniversalMobileCard } from '$lib/components/arcane-table/index.js';
	import { m } from '$lib/paraglide/messages';
	import { secretService } from '$lib/services/secret-service';
	import { TrashIcon, EllipsisIcon, InspectIcon, SecretsIcon, CalendarIcon } from '$lib/icons';

	let {
		secrets = $bindable(),
		selectedIds = $bindable(),
		requestOptions = $bindable()
	}: {
		secrets: Paginated<SecretSummaryDto>;
		selectedIds: string[];
		requestOptions: SearchPaginationSortRequest;
	} = $props();

	let isLoading = $state({
		removing: false
	});

	async function handleRemoveSecretConfirm(secret: SecretSummaryDto) {
		const safeName = secret.name?.trim() || m.common_unknown();
		openConfirmDialog({
			title: m.common_remove_title({ resource: m.resource_secret() }),
			message: m.common_remove_confirm({ resource: `${m.resource_secret()} "${safeName}"` }),
			confirm: {
				label: m.common_remove(),
				destructive: true,
				action: async () => {
					isLoading.removing = true;
					handleApiResultWithCallbacks({
						result: await tryCatch(secretService.deleteSecret(secret.id)),
						message: m.common_remove_failed({ resource: `${m.resource_secret()} "${safeName}"` }),
						setLoadingState: (value) => (isLoading.removing = value),
						onSuccess: async () => {
							toast.success(m.common_remove_success({ resource: `${m.resource_secret()} "${safeName}"` }));
							secrets = await secretService.getSecrets(requestOptions);
						}
					});
				}
			}
		});
	}

	async function handleDeleteSelected(ids: string[]) {
		if (!ids?.length) return;

		openConfirmDialog({
			title: m.common_remove_selected_count({ count: ids.length }),
			message: m.common_remove_confirm({ resource: `${ids.length} ${m.secrets_title()}` }),
			confirm: {
				label: m.common_remove(),
				destructive: true,
				action: async () => {
					isLoading.removing = true;
					let successCount = 0;
					let failureCount = 0;

					const idToSecret = new Map(secrets.data.map((s) => [s.id, s] as const));

					for (const id of ids) {
						const item = idToSecret.get(id);
						const safeName = item?.name?.trim() || m.common_unknown();
						const result = await tryCatch(secretService.deleteSecret(id));
						handleApiResultWithCallbacks({
							result,
							message: m.common_remove_failed({ resource: `${m.resource_secret()} "${safeName}"` }),
							setLoadingState: () => {},
							onSuccess: () => {
								successCount += 1;
							}
						});
						if (result.error) failureCount += 1;
					}

					isLoading.removing = false;
					if (successCount > 0) {
						toast.success(m.common_bulk_remove_success({ count: successCount, resource: m.secrets_title() }));
						secrets = await secretService.getSecrets(requestOptions);
					}
					if (failureCount > 0) {
						toast.error(m.common_bulk_remove_failed({ count: failureCount, resource: m.secrets_title() }));
					}
					selectedIds = [];
				}
			}
		});
	}

	const columns = [
		{ accessorKey: 'id', title: m.common_id(), hidden: true },
		{ accessorKey: 'name', title: m.common_name(), sortable: true, cell: NameCell },
		{ accessorKey: 'composePath', title: m.secrets_mount_path_label(), cell: MountPathCell },
		{ accessorKey: 'description', title: m.common_description(), sortable: true, cell: DescriptionCell },
		{ accessorKey: 'createdAt', title: m.common_created(), sortable: true, cell: CreatedCell }
	] satisfies ColumnSpec<SecretSummaryDto>[];

	const mobileFields = [
		{ id: 'composePath', label: m.secrets_mount_path_label(), defaultVisible: true },
		{ id: 'description', label: m.common_description(), defaultVisible: true },
		{ id: 'createdAt', label: m.common_created(), defaultVisible: true }
	];

	const bulkActions = $derived.by<BulkAction[]>(() => [
		{
			id: 'remove',
			label: m.common_remove_selected_count({ count: selectedIds?.length ?? 0 }),
			action: 'remove',
			onClick: handleDeleteSelected,
			loading: isLoading.removing,
			disabled: isLoading.removing,
			icon: TrashIcon
		}
	]);

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
</script>

{#snippet NameCell({ item }: { item: SecretSummaryDto })}
	<a class="font-medium hover:underline" href="/secrets/{item.id}" title={item.name}>
		{truncateString(item.name, 40)}
	</a>
{/snippet}

{#snippet DescriptionCell({ value }: { value: unknown })}
	{@const text = typeof value === 'string' && value.trim() ? value : m.common_no_description()}
	<span class="text-muted-foreground text-sm">{truncateString(text, 60)}</span>
{/snippet}

{#snippet MountPathCell({ item }: { item: SecretSummaryDto })}
	<div class="flex items-center gap-2">
		<span class="text-muted-foreground font-mono text-xs" title={item.composePath}>
			{item.composePath}
		</span>
		<CopyButton text={item.composePath} class="size-6" />
	</div>
{/snippet}

{#snippet CreatedCell({ value }: { value: unknown })}
	{format(new Date(String(value)), 'PP p')}
{/snippet}

{#snippet SecretMobileCardSnippet({
	item,
	mobileFieldVisibility
}: {
	item: SecretSummaryDto;
	mobileFieldVisibility: MobileFieldVisibility;
})}
	<UniversalMobileCard
		{item}
		icon={() => ({
			component: SecretsIcon,
			variant: 'emerald'
		})}
		title={(item) => item.name}
		subtitle={(item) => ((mobileFieldVisibility.id ?? true) ? item.id : null)}
		fields={[
			{
				label: m.secrets_mount_path_label(),
				getValue: (item: SecretSummaryDto) => item.composePath,
				icon: SecretsIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility.composePath ?? true
			},
			{
				label: m.common_description(),
				getValue: (item: SecretSummaryDto) => item.description || m.common_no_description(),
				icon: SecretsIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility.description ?? true
			},
			{
				label: m.common_created(),
				getValue: (item: SecretSummaryDto) => format(new Date(String(item.createdAt)), 'PP p'),
				icon: CalendarIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility.createdAt ?? true
			}
		]}
		rowActions={RowActions}
		onclick={() => goto(`/secrets/${item.id}`)}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: SecretSummaryDto })}
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
				<DropdownMenu.Item onclick={() => goto(`/secrets/${item.id}`)}>
					<InspectIcon class="size-4" />
					{m.common_inspect()}
				</DropdownMenu.Item>
				<DropdownMenu.Item variant="destructive" onclick={() => handleRemoveSecretConfirm(item)} disabled={isLoading.removing}>
					<TrashIcon class="size-4" />
					{m.common_remove()}
				</DropdownMenu.Item>
			</DropdownMenu.Group>
		</DropdownMenu.Content>
	</DropdownMenu.Root>
{/snippet}

<ArcaneTable
	persistKey="arcane-secrets-table"
	items={secrets}
	bind:requestOptions
	bind:selectedIds
	bind:mobileFieldVisibility
	{bulkActions}
	onRefresh={async (options) => (secrets = await secretService.getSecrets(options))}
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={SecretMobileCardSnippet}
/>
