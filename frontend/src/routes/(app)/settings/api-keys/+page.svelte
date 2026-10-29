<script lang="ts">
	import { toast } from 'svelte-sonner';

	import type { ActionButton } from '#lib/components/action-button-group/types.js';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import ApiKeyFormSheet from '#lib/components/sheets/api-key-form-sheet.svelte';
	import * as ResponsiveDialog from '#lib/components/ui/responsive-dialog/index.js';
	import { Snippet } from '#lib/components/ui/snippet/index.js';
	import { ApiKeyIcon } from '#lib/icons/index.js';
	import { SettingsPageLayout } from '#lib/layouts/index.js';
	import * as m from '#lib/paraglide/messages.js';
	import { apiKeyService } from '#lib/services/api-key-service.js';
	import type { ApiKey, ApiKeyCreated, ApiKeyPermissionGrant, ApiKeyUpdate, PersonalApiKeyCreate } from '#lib/types/auth.js';
	import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { base } from '#lib/utils/base-path.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import ApiKeyTable from './components/api-key-table.svelte';

	let { data } = $props();

	let apiKeys = $derived(data.apiKeys);
	let selectedIds = $state<string[]>([]);
	let requestOptions = $derived<SearchPaginationSortRequest>(data.apiKeyRequestOptions);

	let isDialogOpen = $state({
		create: false,
		edit: false,
		showKey: false
	});

	let apiKeyToEdit = $state<ApiKey | null>(null);
	let newlyCreatedKey = $state<ApiKeyCreated | null>(null);

	let isLoading = $state({
		creating: false,
		editing: false,
		refresh: false
	});

	function openCreateDialog() {
		apiKeyToEdit = null;
		isDialogOpen.create = true;
	}

	function openEditDialog(apiKey: ApiKey) {
		apiKeyToEdit = apiKey;
		isDialogOpen.edit = true;
	}

	async function refreshApiKeys() {
		await handleApiResultWithCallbacks({
			result: await tryCatch(apiKeyService.getApiKeys(requestOptions)),
			message: m.common_refresh_failed({ resource: m.api_key_page_title() }),
			onSuccess: (list) => {
				apiKeys = list;
			}
		});
	}

	async function handleApiKeySubmit(
		submission:
			| { isEditMode: true; apiKeyId: string; apiKey: ApiKeyUpdate }
			| { isEditMode: false; apiKey: PersonalApiKeyCreate & { permissions?: ApiKeyPermissionGrant[] } }
	) {
		const loading = submission.isEditMode ? 'editing' : 'creating';
		isLoading[loading] = true;
		const safeName = submission.apiKey.name?.trim() || apiKeyToEdit?.name || 'Unknown';

		if (submission.isEditMode) {
			await handleApiResultWithCallbacks({
				result: await tryCatch(apiKeyService.update(submission.apiKeyId, submission.apiKey)),
				message: m.api_key_update_failed({ name: safeName }),
				setLoadingState: (value) => (isLoading[loading] = value),
				onSuccess: async () => {
					toast.success(m.api_key_updated_success({ name: safeName }));
					isDialogOpen.edit = false;
					apiKeyToEdit = null;
					await refreshApiKeys();
				}
			});
		} else {
			await handleApiResultWithCallbacks({
				result: await tryCatch(apiKeyService.create({ ...submission.apiKey, permissions: submission.apiKey.permissions ?? [] })),
				message: m.api_key_create_failed({ name: safeName }),
				setLoadingState: (value) => (isLoading[loading] = value),
				onSuccess: async (createdKey) => {
					toast.success(m.api_key_created_success({ name: safeName }));
					isDialogOpen.create = false;
					newlyCreatedKey = createdKey as ApiKeyCreated;
					isDialogOpen.showKey = true;
					await refreshApiKeys();
				}
			});
		}
	}

	const actionButtons: ActionButton[] = $derived.by(() => [
		{
			id: 'create',
			action: 'create',
			placement: 'primary',
			label: m.create_api_key(),
			onclick: openCreateDialog,
			loading: isLoading.creating,
			disabled: isLoading.creating
		}
	]);
</script>

<SettingsPageLayout
	title={m.api_key_page_title()}
	description={m.api_key_page_description().replaceAll("href='/api/", `href='${base}/api/`)}
	icon={ApiKeyIcon}
	pageType="management"
	{actionButtons}
>
	{#snippet mainContent()}
		<ApiKeyTable
			bind:apiKeys
			bind:selectedIds
			bind:requestOptions
			onApiKeysChanged={async () => {
				apiKeys = await apiKeyService.getApiKeys(requestOptions);
			}}
			onEditApiKey={openEditDialog}
		/>
	{/snippet}

	{#snippet additionalContent()}
		<ApiKeyFormSheet
			bind:open={isDialogOpen.create}
			apiKeyToEdit={null}
			manifest={data.permissionsManifest}
			availablePermissions={[]}
			onSubmit={handleApiKeySubmit}
			isLoading={isLoading.creating}
		/>

		<ApiKeyFormSheet
			bind:open={isDialogOpen.edit}
			{apiKeyToEdit}
			manifest={data.permissionsManifest}
			availablePermissions={apiKeyToEdit?.permissions ?? []}
			onSubmit={handleApiKeySubmit}
			isLoading={isLoading.editing}
		/>

		<ResponsiveDialog.Root
			bind:open={isDialogOpen.showKey}
			title={m.api_key_created_title()}
			description={m.api_key_created_description()}
			contentClass="!max-w-fit"
		>
			{#snippet children()}
				<div class="space-y-4 py-4">
					<div class="rounded-lg bg-muted p-4">
						<p class="mb-2 text-sm font-medium text-muted-foreground">{m.api_key_your_key()}</p>
						<Snippet
							text={newlyCreatedKey?.key || ''}
							onCopy={(status) => {
								if (status === 'success') {
									toast.success(m.api_key_copied_success());
								}
							}}
						/>
					</div>
					<div class="rounded-lg border border-warning/30 bg-warning/10 p-4">
						<p class="text-sm text-warning">
							<strong>{m.common_important()}:</strong>
							{m.api_key_important_warning()}
						</p>
					</div>
				</div>
			{/snippet}
			{#snippet footer()}
				<ArcaneButton action="confirm" onclick={() => (isDialogOpen.showKey = false)} customLabel={m.common_done()} />
			{/snippet}
		</ResponsiveDialog.Root>
	{/snippet}
</SettingsPageLayout>
