<script lang="ts">
	import KeyIcon from '@lucide/svelte/icons/key';
	import { toast } from 'svelte-sonner';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import ApiKeyTable from './api-key-table.svelte';
	import ApiKeyFormSheet from '$lib/components/sheets/api-key-form-sheet.svelte';
	import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
	import type { ApiKey, ApiKeyCreated } from '$lib/types/api-key.type';
	import { apiKeyService } from '$lib/services/api-key-service';
	import { SettingsPageLayout, type SettingsActionButton } from '$lib/layouts/index.js';
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Snippet } from '$lib/components/ui/snippet/index.js';
	import * as m from '$lib/paraglide/messages.js';
	import { untrack } from 'svelte';

	let { data } = $props();

	let apiKeys = $state(untrack(() => data.apiKeys));
	let selectedIds = $state<string[]>([]);
	let requestOptions = $state<SearchPaginationSortRequest>(untrack(() => data.apiKeyRequestOptions));

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

	async function handleApiKeySubmit({
		apiKey,
		isEditMode,
		apiKeyId
	}: {
		apiKey: { name: string; description?: string; expiresAt?: string };
		isEditMode: boolean;
		apiKeyId?: string;
	}) {
		const loading = isEditMode ? 'editing' : 'creating';
		isLoading[loading] = true;

		try {
			if (isEditMode && apiKeyId) {
				const safeName = apiKey.name?.trim() || 'Unknown';
				const result = await tryCatch(apiKeyService.update(apiKeyId, apiKey));
				handleApiResultWithCallbacks({
					result,
					message: m.api_key_update_failed({ name: safeName }),
					setLoadingState: (value) => (isLoading[loading] = value),
					onSuccess: async () => {
						toast.success(m.api_key_updated_success({ name: safeName }));
						apiKeys = await apiKeyService.getApiKeys(requestOptions);
						isDialogOpen.edit = false;
						apiKeyToEdit = null;
					}
				});
			} else {
				const safeName = apiKey.name?.trim() || 'Unknown';
				const result = await tryCatch(apiKeyService.create(apiKey));
				handleApiResultWithCallbacks({
					result,
					message: m.api_key_create_failed({ name: safeName }),
					setLoadingState: (value) => (isLoading[loading] = value),
					onSuccess: async (createdKey) => {
						toast.success(m.api_key_created_success({ name: safeName }));
						apiKeys = await apiKeyService.getApiKeys(requestOptions);
						isDialogOpen.create = false;
						newlyCreatedKey = createdKey as ApiKeyCreated;
						isDialogOpen.showKey = true;
					}
				});
			}
		} catch (error) {
			console.error('Failed to submit API key:', error);
		}
	}

	const actionButtons: SettingsActionButton[] = $derived.by(() => [
		{
			id: 'create',
			action: 'create',
			label: m.api_key_create_button(),
			onclick: openCreateDialog,
			loading: isLoading.creating,
			disabled: isLoading.creating
		}
	]);
</script>

<SettingsPageLayout
	title={m.api_key_page_title()}
	description={m.api_key_page_description()}
	icon={KeyIcon}
	pageType="management"
	{actionButtons}
	statCardsColumns={3}
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
			onSubmit={handleApiKeySubmit}
			isLoading={isLoading.creating}
		/>

		<ApiKeyFormSheet bind:open={isDialogOpen.edit} {apiKeyToEdit} onSubmit={handleApiKeySubmit} isLoading={isLoading.editing} />

		<Dialog.Root bind:open={isDialogOpen.showKey}>
			<Dialog.Content class="!max-w-fit">
				<Dialog.Header>
					<Dialog.Title class="flex items-center gap-2">
						<KeyIcon class="size-5" />
						{m.api_key_created_title()}
					</Dialog.Title>
					<Dialog.Description>
						{m.api_key_created_description()}
					</Dialog.Description>
				</Dialog.Header>
				<div class="space-y-4 py-4">
					<div class="bg-muted rounded-lg p-4">
						<p class="text-muted-foreground mb-2 text-sm font-medium">{m.api_key_your_key()}</p>
						<Snippet
							text={newlyCreatedKey?.key || ''}
							onCopy={(status) => {
								if (status === 'success') {
									toast.success(m.api_key_copied_success());
								}
							}}
						/>
					</div>
					<div class="rounded-lg border border-yellow-200 bg-yellow-50 p-4 dark:border-yellow-800 dark:bg-yellow-900/20">
						<p class="text-sm text-yellow-800 dark:text-yellow-200">
							<strong>{m.common_important()}:</strong>
							{m.api_key_important_warning()}
						</p>
					</div>
				</div>
				<Dialog.Footer>
					<Button onclick={() => (isDialogOpen.showKey = false)}>{m.common_done()}</Button>
				</Dialog.Footer>
			</Dialog.Content>
		</Dialog.Root>
	{/snippet}
</SettingsPageLayout>
