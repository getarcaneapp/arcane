<script lang="ts">
	import { onMount } from 'svelte';
	import { toast } from 'svelte-sonner';
	import ApiKeyFormSheet from '#lib/components/sheets/api-key-form-sheet.svelte';
	import { ArcaneButton } from '#lib/components/arcane-button';
	import { apiKeyService } from '#lib/services/api-key-service';
	import type { ApiKey, ApiKeyCreated, ApiKeyPermissionGrant, CreateUserApiKey } from '#lib/types/auth';
	import { AddIcon, ApiKeyIcon, CopyIcon, TrashIcon } from '#lib/icons';
	import { formatDate, formatRelativeTime } from '#lib/utils/formatting';
	import { m } from '#lib/paraglide/messages';
	import { confirmAndRun } from '#lib/utils/bulk-actions';

	let apiKeys = $state<ApiKey[]>([]);
	let apiKeysLoading = $state(false);
	let showCreateKeyForm = $state(false);
	let creatingKey = $state(false);
	let deletingKeyId = $state<string | null>(null);
	let createdKey = $state<ApiKeyCreated | null>(null);

	async function loadApiKeys() {
		apiKeysLoading = true;
		try {
			apiKeys = await apiKeyService.listMine();
		} catch (error) {
			toast.error(error instanceof Error ? error.message : m.common_refresh_failed({ resource: m.api_key_page_title() }));
		} finally {
			apiKeysLoading = false;
		}
	}

	async function createApiKey({
		apiKey
	}: {
		apiKey: { name: string; description?: string; expiresAt?: string; permissions?: ApiKeyPermissionGrant[] };
		isEditMode: boolean;
		apiKeyId?: string;
	}) {
		creatingKey = true;
		try {
			const payload: CreateUserApiKey = {
				name: apiKey.name,
				description: apiKey.description,
				expiresAt: apiKey.expiresAt
			};
			createdKey = await apiKeyService.createMine(payload);
			showCreateKeyForm = false;
			await loadApiKeys();
		} catch (error) {
			toast.error(error instanceof Error ? error.message : m.api_key_create_failed({ name: apiKey.name }));
		} finally {
			creatingKey = false;
		}
	}

	function deleteApiKey(id: string, name: string) {
		const safeName = name.trim() || m.common_unknown();
		confirmAndRun({
			title: m.api_key_delete_title({ name: safeName }),
			message: m.api_key_delete_message({ name: safeName }),
			confirmLabel: m.common_delete(),
			destructive: true,
			setLoading: (loading) => (deletingKeyId = loading ? id : null),
			run: () => apiKeyService.deleteMine(id),
			failureMessage: m.api_key_delete_failed({ name: safeName }),
			onSuccess: async () => {
				toast.success(m.account_api_key_deleted());
				await loadApiKeys();
			}
		});
	}

	function copyKeyToClipboard(key: string) {
		void navigator.clipboard.writeText(key);
		toast.success(m.common_key_copied());
	}

	onMount(() => {
		void loadApiKeys();
	});
</script>

<section class="space-y-5">
	<div class="flex flex-wrap items-start justify-between gap-3">
		<div class="min-w-0">
			<h2 class="text-base font-semibold tracking-tight sm:text-lg">{m.account_api_keys_title()}</h2>
			<p class="mt-1 text-xs text-muted-foreground sm:text-sm">{m.account_api_keys_description()}</p>
		</div>
		{#if !showCreateKeyForm && !createdKey}
			<ArcaneButton
				action="create"
				tone="outline"
				size="sm"
				customLabel={m.account_new_key()}
				icon={AddIcon}
				class="shrink-0"
				onclick={() => (showCreateKeyForm = true)}
			/>
		{/if}
	</div>
	{#if createdKey}
		<div class="mb-4 space-y-3 rounded-lg border border-primary/30 bg-primary/5 p-4">
			<div>
				<div class="truncate text-sm font-semibold">{m.api_key_created_title()}: {createdKey.name}</div>
				<p class="mt-1 text-xs text-muted-foreground">{m.api_key_save_warning()}</p>
			</div>
			<code class="block truncate rounded border bg-background px-3 py-2 font-mono text-xs">
				{createdKey.key}
			</code>
			<div class="flex flex-wrap justify-end gap-2">
				<ArcaneButton
					action="base"
					tone="outline"
					size="sm"
					customLabel={m.common_copy()}
					icon={CopyIcon}
					onclick={() => copyKeyToClipboard(createdKey!.key)}
				/>
				<ArcaneButton
					action="cancel"
					tone="ghost"
					size="sm"
					customLabel={m.common_ive_saved_it()}
					onclick={() => (createdKey = null)}
				/>
			</div>
		</div>
	{/if}

	<div class="overflow-hidden rounded-xl border border-border/60 bg-card/30">
		{#if apiKeysLoading && apiKeys.length === 0}
			<div class="p-6 text-center text-sm text-muted-foreground">{m.common_loading()}</div>
		{:else if apiKeys.length === 0}
			<div class="p-6 text-center text-sm text-muted-foreground">
				<ApiKeyIcon class="mx-auto mb-2 size-8 opacity-40" />
				{m.api_keys_empty()}
			</div>
		{:else}
			<ul class="divide-y divide-border/60">
				{#each apiKeys as key (key.id)}
					<li class="px-4 py-3">
						<div class="flex items-start justify-between gap-2">
							<div class="min-w-0 flex-1">
								<div class="truncate text-sm font-medium">{key.name}</div>
								{#if key.description}
									<div class="mt-0.5 truncate text-xs text-muted-foreground">{key.description}</div>
								{/if}
							</div>
							<ArcaneButton
								action="remove"
								tone="ghost"
								size="sm"
								icon={TrashIcon}
								customLabel={m.common_delete()}
								showLabel={false}
								loading={deletingKeyId === key.id}
								disabled={deletingKeyId !== null}
								class="shrink-0 text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
								onclick={() => deleteApiKey(key.id, key.name)}
							/>
						</div>
						<div class="mt-1.5 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
							<code class="rounded bg-muted/40 px-1.5 py-0.5 font-mono">{key.keyPrefix}…</code>
							{#if formatDate(key.createdAt)}
								<span>{m.common_created()} {formatDate(key.createdAt)}</span>
								<span aria-hidden="true">·</span>
							{/if}
							<span>{m.last_used()} {formatRelativeTime(key.lastUsedAt) || m.common_never()}</span>
						</div>
					</li>
				{/each}
			</ul>
		{/if}
	</div>
</section>

<ApiKeyFormSheet
	bind:open={showCreateKeyForm}
	apiKeyToEdit={null}
	mode="personal"
	onSubmit={createApiKey}
	isLoading={creatingKey}
/>
