<script lang="ts">
	import KeyIcon from '@lucide/svelte/icons/key';
	import CopyIcon from '@lucide/svelte/icons/copy';
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

	let { data } = $props();

	let apiKeys = $state(data.apiKeys);
	let selectedIds = $state<string[]>([]);
	let requestOptions = $state<SearchPaginationSortRequest>(data.apiKeyRequestOptions);

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

	function copyKeyToClipboard() {
		if (newlyCreatedKey?.key) {
			navigator.clipboard.writeText(newlyCreatedKey.key);
			toast.success('API key copied to clipboard');
		}
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
					message: `Failed to update API key "${safeName}"`,
					setLoadingState: (value) => (isLoading[loading] = value),
					onSuccess: async () => {
						toast.success(`API key "${safeName}" updated successfully`);
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
					message: `Failed to create API key "${safeName}"`,
					setLoadingState: (value) => (isLoading[loading] = value),
					onSuccess: async (createdKey) => {
						toast.success(`API key "${safeName}" created successfully`);
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
			label: 'Create API Key',
			onclick: openCreateDialog,
			loading: isLoading.creating,
			disabled: isLoading.creating
		}
	]);
</script>

<SettingsPageLayout
	title="API Keys"
	description="Manage API keys for programmatic access to Arcane"
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

		<ApiKeyFormSheet
			bind:open={isDialogOpen.edit}
			{apiKeyToEdit}
			onSubmit={handleApiKeySubmit}
			isLoading={isLoading.editing}
		/>

		<Dialog.Root bind:open={isDialogOpen.showKey}>
			<Dialog.Content class="sm:max-w-lg">
				<Dialog.Header>
					<Dialog.Title class="flex items-center gap-2">
						<KeyIcon class="size-5" />
						API Key Created
					</Dialog.Title>
					<Dialog.Description>
						Copy your API key now. You won't be able to see it again!
					</Dialog.Description>
				</Dialog.Header>
				<div class="space-y-4 py-4">
					<div class="bg-muted rounded-lg p-4">
						<p class="text-muted-foreground mb-2 text-sm font-medium">Your API Key</p>
						<div class="flex items-center gap-2">
							<code class="bg-background flex-1 overflow-hidden rounded border p-2 text-sm break-all">
								{newlyCreatedKey?.key || ''}
							</code>
							<Button variant="outline" size="icon" onclick={copyKeyToClipboard}>
								<CopyIcon class="size-4" />
							</Button>
						</div>
					</div>
					<div class="bg-yellow-50 dark:bg-yellow-900/20 rounded-lg border border-yellow-200 dark:border-yellow-800 p-4">
						<p class="text-sm text-yellow-800 dark:text-yellow-200">
							<strong>Important:</strong> This is the only time you will see this key. Make sure to copy it and store it securely.
						</p>
					</div>
				</div>
				<Dialog.Footer>
					<Button onclick={() => (isDialogOpen.showKey = false)}>Done</Button>
				</Dialog.Footer>
			</Dialog.Content>
		</Dialog.Root>
	{/snippet}
</SettingsPageLayout>
