<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { toast } from 'svelte-sonner';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { handleApiResultWithCallbacks } from '$lib/utils/api';
	import { tryCatch } from '$lib/utils/api';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/shared';
	import type { FederatedCredential } from '$lib/types/auth';
	import type { ColumnSpec, MobileFieldVisibility, BulkAction } from '$lib/components/arcane-table';
	import { UniversalMobileCard } from '$lib/components/arcane-table';
	import { federatedCredentialService } from '$lib/services/federated-credential-service';
	import { formatOptionalDateTime, isPastDate } from '$lib/utils/formatting';
	import * as m from '$lib/paraglide/messages.js';
	import { LockIcon, TrashIcon, EditIcon, EllipsisIcon } from '$lib/icons';
	import { isGlobalAdmin } from '$lib/utils/auth';

	let {
		federatedCredentials = $bindable(),
		selectedIds = $bindable(),
		requestOptions = $bindable(),
		onFederatedCredentialsChanged,
		onEditFederatedCredential
	}: {
		federatedCredentials: Paginated<FederatedCredential>;
		selectedIds: string[];
		requestOptions: SearchPaginationSortRequest;
		onFederatedCredentialsChanged: () => Promise<void>;
		onEditFederatedCredential: (credential: FederatedCredential) => void;
	} = $props();

	let isLoading = $state({
		removing: false
	});
	const canManageFederatedCredentials = $derived(isGlobalAdmin());

	function getStatusText(credential: FederatedCredential): string {
		if (isPastDate(credential.expiresAt)) return m.federated_credential_status_expired();
		if (!credential.enabled) return m.common_disabled();
		return m.common_enabled();
	}

	function getStatusVariant(credential: FederatedCredential): 'red' | 'green' | 'gray' {
		if (isPastDate(credential.expiresAt)) return 'red';
		if (!credential.enabled) return 'gray';
		return 'green';
	}

	function getScope(credential: FederatedCredential): string {
		return credential.environmentName || m.federated_credential_scope_global_option();
	}

	function getRoleScope(credential: FederatedCredential): string {
		const role = credential.roleName || credential.roleId;
		return `${role} / ${getScope(credential)}`;
	}

	async function handleDeleteSelected() {
		if (selectedIds.length === 0) return;

		openConfirmDialog({
			title: m.federated_credential_delete_selected_title({ count: selectedIds.length }),
			message: m.federated_credential_delete_selected_message({ count: selectedIds.length }),
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: async () => {
					isLoading.removing = true;
					let successCount = 0;
					let failureCount = 0;

					for (const credentialId of selectedIds) {
						const result = await tryCatch(federatedCredentialService.delete(credentialId));
						handleApiResultWithCallbacks({
							result,
							message: m.federated_credential_delete_failed({ name: credentialId }),
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
						toast.success(m.federated_credential_bulk_delete_success({ count: successCount }));
						await onFederatedCredentialsChanged();
					}

					if (failureCount > 0) {
						toast.error(m.federated_credential_bulk_delete_failed({ count: failureCount }));
					}

					selectedIds = [];
				}
			}
		});
	}

	async function handleDeleteFederatedCredential(credentialId: string, name: string) {
		const safeName = name?.trim() || m.common_unknown();
		openConfirmDialog({
			title: m.federated_credential_delete_title({ name: safeName }),
			message: m.federated_credential_delete_message({ name: safeName }),
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: async () => {
					isLoading.removing = true;
					handleApiResultWithCallbacks({
						result: await tryCatch(federatedCredentialService.delete(credentialId)),
						message: m.federated_credential_delete_failed({ name: safeName }),
						setLoadingState: (value) => (isLoading.removing = value),
						onSuccess: async () => {
							toast.success(m.federated_credential_delete_success({ name: safeName }));
							await onFederatedCredentialsChanged();
						}
					});
				}
			}
		});
	}

	const columns = [
		{ accessorKey: 'name', title: m.common_name(), sortable: true, cell: NameCell },
		{ accessorKey: 'issuerUrl', title: m.federated_credential_issuer_label(), sortable: true, cell: IssuerCell },
		{ accessorKey: 'subjectMatch', title: m.federated_credential_subject_match_label(), sortable: true, cell: SubjectCell },
		{ accessorKey: 'roleId', title: m.federated_credential_role_scope_column(), sortable: false, cell: RoleScopeCell },
		{ accessorKey: 'enabled', title: m.common_status(), sortable: true, cell: StatusCell },
		{ accessorKey: 'lastUsedAt', title: m.federated_credential_last_used(), sortable: true, cell: LastUsedCell }
	] satisfies ColumnSpec<FederatedCredential>[];

	const mobileFields = [
		{ id: 'issuerUrl', label: m.federated_credential_issuer_label(), defaultVisible: true },
		{ id: 'subjectMatch', label: m.federated_credential_subject_match_label(), defaultVisible: true },
		{ id: 'roleScope', label: m.federated_credential_role_scope_column(), defaultVisible: true },
		{ id: 'lastUsedAt', label: m.federated_credential_last_used(), defaultVisible: true }
	];

	const bulkActions = $derived.by<BulkAction[]>(() => {
		if (!canManageFederatedCredentials) return [];
		return [
			{
				id: 'remove',
				label: m.common_remove_selected_count({ count: selectedIds?.length ?? 0 }),
				action: 'remove',
				onClick: handleDeleteSelected,
				loading: isLoading.removing,
				disabled: isLoading.removing || selectedIds.length === 0,
				icon: TrashIcon
			}
		];
	});

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
</script>

{#snippet NameCell({ item }: { item: FederatedCredential })}
	<span class="font-medium">{item.name}</span>
{/snippet}

{#snippet IssuerCell({ item }: { item: FederatedCredential })}
	<span class="text-muted-foreground max-w-[18rem] truncate">{item.issuerUrl}</span>
{/snippet}

{#snippet SubjectCell({ item }: { item: FederatedCredential })}
	<div class="flex flex-col gap-0.5">
		<span class="font-mono text-xs">{item.subjectMatch}</span>
		<span class="text-muted-foreground text-xs">{item.subjectClaim} / {item.matchType}</span>
	</div>
{/snippet}

{#snippet RoleScopeCell({ item }: { item: FederatedCredential })}
	<span>{getRoleScope(item)}</span>
{/snippet}

{#snippet StatusCell({ item }: { item: FederatedCredential })}
	<StatusBadge text={getStatusText(item)} variant={getStatusVariant(item)} />
{/snippet}

{#snippet LastUsedCell({ item }: { item: FederatedCredential })}
	{formatOptionalDateTime(item.lastUsedAt)}
{/snippet}

{#snippet FederatedCredentialMobileCardSnippet({
	item,
	mobileFieldVisibility
}: {
	item: FederatedCredential;
	mobileFieldVisibility: MobileFieldVisibility;
})}
	<UniversalMobileCard
		{item}
		icon={{ component: LockIcon, variant: 'blue' }}
		title={(item: FederatedCredential) => item.name}
		subtitle={(item: FederatedCredential) => ((mobileFieldVisibility['issuerUrl'] ?? true) ? item.issuerUrl : null)}
		badges={[
			(item: FederatedCredential) => ({
				variant: getStatusVariant(item),
				text: getStatusText(item)
			})
		]}
		fields={[
			{
				label: m.federated_credential_subject_match_label(),
				getValue: (item: FederatedCredential) => item.subjectMatch,
				icon: LockIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['subjectMatch'] ?? true
			},
			{
				label: m.federated_credential_role_scope_column(),
				getValue: (item: FederatedCredential) => getRoleScope(item),
				icon: LockIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['roleScope'] ?? true
			},
			{
				label: m.federated_credential_last_used(),
				getValue: (item: FederatedCredential) => formatOptionalDateTime(item.lastUsedAt),
				icon: LockIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['lastUsedAt'] ?? true
			}
		]}
		rowActions={RowActions}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: FederatedCredential })}
	{#if canManageFederatedCredentials}
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
					<DropdownMenu.Item onclick={() => onEditFederatedCredential(item)}>
						<EditIcon class="size-4" />
						{m.common_edit()}
					</DropdownMenu.Item>
					<DropdownMenu.Separator />
					<DropdownMenu.Item variant="destructive" onclick={() => handleDeleteFederatedCredential(item.id, item.name)}>
						<TrashIcon class="size-4" />
						{m.common_delete()}
					</DropdownMenu.Item>
				</DropdownMenu.Group>
			</DropdownMenu.Content>
		</DropdownMenu.Root>
	{/if}
{/snippet}

<ArcaneTable
	persistKey="arcane-federated-credentials-table"
	items={federatedCredentials}
	bind:requestOptions
	bind:selectedIds
	bind:mobileFieldVisibility
	{bulkActions}
	onRefresh={async (options) => {
		requestOptions = options;
		await onFederatedCredentialsChanged();
		return federatedCredentials;
	}}
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={FederatedCredentialMobileCardSnippet}
/>
