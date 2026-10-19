<script lang="ts">
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import { UniversalMobileCard } from '#lib/components/arcane-table/index.js';
	import type { ColumnSpec, MobileFieldVisibility } from '#lib/components/arcane-table/index.js';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import EmptyState from '#lib/components/states/empty-state.svelte';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Textarea } from '#lib/components/ui/textarea/index.js';
	import { LockIcon, TrashIcon } from '#lib/icons/index.js';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { podmanService } from '#lib/services/podman-service.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import type { PodmanSecret } from '#lib/types/podman.js';
	import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
	import { extractApiErrorMessage, handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { formatDateTime } from '#lib/utils/formatting.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	const queryClient = useQueryClient();
	const currentEnvId = $derived(environmentStore.selected?.id ?? '0');
	const canManage = $derived(hasPermission('podman:secrets', currentEnvId));

	const secretsQuery = createQuery(() => ({
		queryKey: queryKeys.podman.secrets(currentEnvId),
		queryFn: () => podmanService.getSecrets(),
		retry: false
	}));
	const secrets = $derived(secretsQuery.data ?? []);

	let requestOptions = $state<SearchPaginationSortRequest>({ pagination: { page: 1, limit: 20 } });
	let mobileFieldVisibility = $state<MobileFieldVisibility>({});
	let createName = $state('');
	let createData = $state('');
	let createDriver = $state('');
	let creating = $state(false);

	function buildTableData(items: PodmanSecret[]): Paginated<PodmanSecret> {
		return {
			data: items,
			pagination: {
				totalPages: 1,
				totalItems: items.length,
				currentPage: 1,
				itemsPerPage: Math.max(items.length, 1)
			}
		};
	}

	const tableData = $derived(buildTableData(secrets));

	async function refresh() {
		await queryClient.invalidateQueries({ queryKey: queryKeys.podman.secrets(currentEnvId) });
	}

	async function handleCreate() {
		const name = createName.trim();
		if (!name) {
			toast.error(m.common_field_required({ field: m.common_name() }));
			return;
		}
		if (!createData) {
			toast.error(m.common_field_required({ field: m.value() }));
			return;
		}
		creating = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(podmanService.createSecret({ name, data: createData, driver: createDriver.trim() || undefined })),
			message: m.common_create_failed({ resource: name }),
			setLoadingState: (value) => (creating = value),
			onSuccess: async () => {
				toast.success(m.common_create_success({ resource: name }));
				createName = '';
				createData = '';
				createDriver = '';
				await refresh();
			}
		});
	}

	function handleRemove(secret: PodmanSecret) {
		openConfirmDialog({
			title: m.common_remove_title({ resource: m.podman_secret() }),
			message: m.common_remove_confirm({ resource: secret.spec.name }),
			confirm: {
				label: m.common_remove(),
				destructive: true,
				action: async () => {
					await handleApiResultWithCallbacks({
						result: await tryCatch(podmanService.removeSecret(secret.id)),
						message: m.common_remove_failed({ resource: secret.spec.name }),
						onSuccess: async () => {
							toast.success(m.common_remove_success({ resource: secret.spec.name }));
							await refresh();
						}
					});
				}
			}
		});
	}

	const actionButtons: ActionButton[] = $derived([
		{
			id: 'refresh',
			action: 'refresh',
			placement: 'secondary',
			iconOnly: true,
			label: m.common_refresh(),
			onclick: refresh,
			loading: secretsQuery.isFetching,
			disabled: secretsQuery.isFetching
		}
	]);

	const statCards: StatCardConfig[] = $derived([
		{ title: m.podman_secrets_title(), value: secrets.length, icon: LockIcon, iconColor: 'text-info' }
	]);

	const columns = [
		{ id: 'name', accessorFn: (row) => row.spec.name, title: m.common_name(), sortable: true, cell: NameCell },
		{ id: 'driver', accessorFn: (row) => row.spec.driver.name, title: m.common_driver(), cell: DriverCell },
		{ accessorKey: 'createdAt', title: m.common_created(), sortable: true, cell: CreatedCell },
		{ accessorKey: 'updatedAt', title: m.common_updated(), sortable: true, cell: UpdatedCell }
	] satisfies ColumnSpec<PodmanSecret>[];

	const mobileFields = [
		{ id: 'driver', label: m.common_driver(), defaultVisible: true },
		{ id: 'createdAt', label: m.common_created(), defaultVisible: true }
	];
</script>

<ResourcePageLayout
	title={m.podman_secrets_title()}
	subtitle={m.podman_secrets_subtitle()}
	icon={LockIcon}
	{actionButtons}
	{statCards}
	environmentPicker
>
	{#snippet mainContent()}
		<div class={canManage ? 'grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,2fr)]' : 'grid gap-4'}>
			{#if canManage}
				<Card.Root>
					<Card.Header>
						<Card.Title>{m.common_create_button({ resource: m.podman_secret() })}</Card.Title>
						<Card.Description>{m.podman_secrets_create_description()}</Card.Description>
					</Card.Header>
					<Card.Content>
						<div class="flex flex-col gap-3">
							<Input placeholder={m.common_name()} bind:value={createName} />
							<Input placeholder={m.common_driver()} bind:value={createDriver} />
							<Textarea rows={8} bind:value={createData} placeholder={m.value()} mono size="sm" />
							<ArcaneButton
								action="create"
								customLabel={m.common_create_button({ resource: m.podman_secret() })}
								onclick={handleCreate}
								disabled={creating}
								loading={creating}
							/>
						</div>
					</Card.Content>
				</Card.Root>
			{/if}

			{#if secretsQuery.isError}
				<EmptyState
					icon={LockIcon}
					title={m.common_load_failed({ resource: m.podman_secrets_title() })}
					description={extractApiErrorMessage(secretsQuery.error)}
					actionLabel={m.common_retry()}
					onAction={() => secretsQuery.refetch()}
				/>
			{:else if !secretsQuery.isPending && secrets.length === 0}
				<EmptyState icon={LockIcon} title={m.podman_secrets_empty()} />
			{:else}
				<ArcaneTable
					persistKey="arcane-podman-secrets-table"
					items={tableData}
					bind:requestOptions
					bind:mobileFieldVisibility
					loading={secretsQuery.isPending}
					selectionDisabled
					withoutSearch
					withoutPagination
					onRefresh={async () => {
						await refresh();
						return buildTableData(secrets);
					}}
					{columns}
					{mobileFields}
					rowActions={RowActions}
					mobileCard={SecretMobileCard}
				/>
			{/if}
		</div>
	{/snippet}
</ResourcePageLayout>

{#snippet NameCell({ item }: { item: PodmanSecret })}
	<div class="min-w-0">
		<div class="truncate font-medium">{item.spec.name}</div>
		<div class="font-mono text-xs text-muted-foreground">{item.id.slice(0, 12)}</div>
	</div>
{/snippet}

{#snippet DriverCell({ item }: { item: PodmanSecret })}
	<span class="text-muted-foreground">{item.spec.driver.name || '-'}</span>
{/snippet}

{#snippet CreatedCell({ item }: { item: PodmanSecret })}
	<span class="text-muted-foreground">{formatDateTime(item.createdAt) || item.createdAt}</span>
{/snippet}

{#snippet UpdatedCell({ item }: { item: PodmanSecret })}
	<span class="text-muted-foreground">{formatDateTime(item.updatedAt) || item.updatedAt}</span>
{/snippet}

{#snippet SecretMobileCard({ item, mobileFieldVisibility }: { item: PodmanSecret; mobileFieldVisibility: MobileFieldVisibility })}
	<UniversalMobileCard
		{item}
		icon={{ component: LockIcon, variant: 'blue' }}
		title={(item: PodmanSecret) => item.spec.name}
		fields={[
			{
				label: m.common_driver(),
				getValue: (item: PodmanSecret) => item.spec.driver.name || '-',
				icon: LockIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['driver'] ?? true
			},
			{
				label: m.common_created(),
				getValue: (item: PodmanSecret) => formatDateTime(item.createdAt) || item.createdAt,
				icon: LockIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['createdAt'] ?? true
			}
		]}
		rowActions={RowActions}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: PodmanSecret })}
	<IfPermitted perm="podman:secrets">
		<RowActionsMenu>
			<DropdownMenu.Item variant="destructive" onclick={() => handleRemove(item)}>
				<TrashIcon class="size-4" />
				{m.common_remove()}
			</DropdownMenu.Item>
		</RowActionsMenu>
	</IfPermitted>
{/snippet}
