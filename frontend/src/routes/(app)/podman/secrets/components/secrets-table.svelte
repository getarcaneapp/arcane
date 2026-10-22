<script lang="ts">
	import { toast } from 'svelte-sonner';

	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import { environmentColumns } from '#lib/components/arcane-table/environment-column.js';
	import type { ColumnSpec, MobileFieldVisibility } from '#lib/components/arcane-table/index.js';
	import { UniversalMobileCard } from '#lib/components/arcane-table/index.js';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import { LabelBadges } from '#lib/components/badges/index.js';
	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { CalendarIcon, EditIcon, LockIcon, TrashIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { PERMISSIONS } from '#lib/permissions.generated.js';
	import { podmanService } from '#lib/services/podman-service.js';
	import userStore from '#lib/stores/user-store.svelte.js';
	import type { PodmanSecret } from '#lib/types/podman.js';
	import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { hasShownPermission, type EnvironmentPage, type EnvironmentRow } from '#lib/utils/environments.js';
	import { formatRelativeTime } from '#lib/utils/formatting.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	type SecretRow = EnvironmentRow<PodmanSecret>;

	let {
		secrets = $bindable(),
		requestOptions = $bindable(),
		onRefreshData,
		onEdit
	}: {
		secrets: EnvironmentPage<Paginated<PodmanSecret>>;
		requestOptions: SearchPaginationSortRequest;
		onRefreshData: (options: SearchPaginationSortRequest) => Promise<void>;
		onEdit: (secret: SecretRow) => void;
	} = $props();

	let isDeleting = $state(false);
	let mobileFieldVisibility = $state<Record<string, boolean>>({});

	const canManageSecrets = $derived.by(() => {
		userStore.current;
		return hasShownPermission(PERMISSIONS.PODMAN_SECRETS);
	});

	function handleDelete(secret: SecretRow) {
		const name = secret.spec.name;
		openConfirmDialog({
			title: m.common_delete_title({ resource: m.swarm_secret() }),
			message: m.swarm_secrets_delete_confirm({ name }),
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: async () => {
					isDeleting = true;
					await handleApiResultWithCallbacks({
						result: await tryCatch(podmanService.removeSecret(name, secret.environmentId)),
						message: m.swarm_secrets_delete_failed({ name }),
						setLoadingState: (value) => (isDeleting = value),
						onSuccess: async () => {
							toast.success(m.swarm_secrets_delete_success({ name }));
							await onRefreshData(requestOptions);
						}
					});
				}
			}
		});
	}

	const columns = $derived([
		...environmentColumns<SecretRow>(),
		{ accessorKey: 'id', title: m.common_id(), hidden: true },
		{ id: 'name', accessorFn: (item) => item.spec.name, title: m.common_name(), sortable: true, cell: NameCell },
		{ id: 'driver', accessorFn: (item) => item.spec.driver.name, title: m.common_driver(), sortable: true },
		{ id: 'labels', accessorFn: (item) => item.spec.labels, title: m.common_labels(), cell: LabelsCell },
		{ accessorKey: 'createdAt', title: m.common_created(), sortable: true, cell: TimeCell },
		{ accessorKey: 'updatedAt', title: m.common_updated(), sortable: true, cell: TimeCell }
	] satisfies ColumnSpec<SecretRow>[]);

	const mobileFields = [
		{ id: 'id', label: m.common_id(), defaultVisible: false },
		{ id: 'driver', label: m.common_driver(), defaultVisible: true },
		{ id: 'createdAt', label: m.common_created(), defaultVisible: true }
	];
</script>

{#snippet NameCell({ item }: { item: SecretRow })}
	<button type="button" class="font-medium hover:underline" onclick={() => onEdit(item)}>{item.spec.name}</button>
{/snippet}

{#snippet LabelsCell({ item }: { item: SecretRow })}
	<LabelBadges labels={item.spec.labels} />
{/snippet}

{#snippet TimeCell({ value }: { value: unknown })}
	<span class="text-sm">{formatRelativeTime(String(value ?? '')) || m.common_unknown()}</span>
{/snippet}

{#snippet SecretMobileCard({ item, mobileFieldVisibility }: { item: SecretRow; mobileFieldVisibility: MobileFieldVisibility })}
	<UniversalMobileCard
		{item}
		icon={{ component: LockIcon, variant: 'purple' }}
		title={(item: SecretRow) => item.spec.name}
		subtitle={(item: SecretRow) => ((mobileFieldVisibility['id'] ?? false) ? item.id : null)}
		fields={[
			{
				label: m.common_driver(),
				getValue: (item: SecretRow) => item.spec.driver.name,
				icon: LockIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['driver'] ?? true
			}
		]}
		footer={(mobileFieldVisibility['createdAt'] ?? true)
			? {
					label: m.common_created(),
					getValue: (item: SecretRow) => formatRelativeTime(item.createdAt) || m.common_unknown(),
					icon: CalendarIcon
				}
			: undefined}
		rowActions={RowActions}
		onclick={() => onEdit(item)}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: SecretRow })}
	<RowActionsMenu>
		<DropdownMenu.Item onclick={() => onEdit(item)}>
			<EditIcon class="size-4" />
			{m.common_edit()}
		</DropdownMenu.Item>
		{#if canManageSecrets && hasPermission(PERMISSIONS.PODMAN_SECRETS, item.environmentId)}
			<DropdownMenu.Separator />
			<DropdownMenu.Item variant="destructive" onclick={() => handleDelete(item)} disabled={isDeleting}>
				<TrashIcon class="size-4" />
				{m.common_delete()}
			</DropdownMenu.Item>
		{/if}
	</RowActionsMenu>
{/snippet}

<ArcaneTable
	persistKey="arcane-podman-secrets-table"
	items={secrets}
	bind:requestOptions
	bind:mobileFieldVisibility
	selectionDisabled={true}
	onRefresh={async (options) => {
		requestOptions = options;
		await onRefreshData(options);
		return secrets;
	}}
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={SecretMobileCard}
/>
