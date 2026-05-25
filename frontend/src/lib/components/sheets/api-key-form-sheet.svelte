<script lang="ts">
	import * as ResponsiveDialog from '$lib/components/ui/responsive-dialog/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import FormInput from '$lib/components/form/form-input.svelte';
	import PermissionPicker from '$lib/components/role-editor/permission-picker.svelte';
	import type { ApiKey } from '$lib/types/auth';
	import type { PermissionsManifest, ApiKeyPermissionGrant } from '$lib/types/auth';
	import { z } from 'zod/v4';
	import { createForm, preventDefault } from '$lib/utils/settings';
	import * as m from '$lib/paraglide/messages.js';

	type ApiKeyFormProps = {
		open: boolean;
		apiKeyToEdit: ApiKey | null;
		manifest: PermissionsManifest;
		availablePermissions?: ApiKeyPermissionGrant[];
		onSubmit: (data: {
			apiKey: {
				name: string;
				description?: string;
				expiresAt?: string;
				permissions: ApiKeyPermissionGrant[];
			};
			isEditMode: boolean;
			apiKeyId?: string;
		}) => void;
		isLoading: boolean;
	};

	let {
		open = $bindable(false),
		apiKeyToEdit = $bindable(),
		manifest,
		availablePermissions = [],
		onSubmit,
		isLoading
	}: ApiKeyFormProps = $props();

	let isEditMode = $derived(!!apiKeyToEdit);
	let isStaticApiKey = $derived(apiKeyToEdit?.isStatic ?? false);
	let isBootstrapApiKey = $derived(apiKeyToEdit?.isBootstrap ?? false);
	let isReadOnlyApiKey = $derived(isStaticApiKey || isBootstrapApiKey);

	const formSchema = z.object({
		name: z.string().min(1, m.common_field_required({ field: m.api_key_name() })),
		description: z.string().optional(),
		expiresAt: z.date().optional(),
		permissions: z.array(z.string()).min(1, m.api_key_permissions_required())
	});

	let formData = $derived({
		name: apiKeyToEdit?.name || '',
		description: apiKeyToEdit?.description || '',
		expiresAt: apiKeyToEdit?.expiresAt ? new Date(apiKeyToEdit.expiresAt) : undefined,
		permissions: availablePermissions.map((p) => p.permission)
	});

	let { inputs, ...form } = $derived(createForm<typeof formSchema>(formSchema, formData));

	function handleSubmit() {
		if (isReadOnlyApiKey) return;

		const data = form.validate();
		if (!data) return;

		const apiKeyData = {
			name: data.name,
			description: data.description || undefined,
			expiresAt: data.expiresAt ? data.expiresAt.toISOString() : undefined,
			// v1: persist all picks as global grants (environmentId undefined).
			// env-scoped picking is a follow-up.
			permissions: data.permissions.map((p) => ({ permission: p }))
		};

		onSubmit({ apiKey: apiKeyData, isEditMode, apiKeyId: apiKeyToEdit?.id });
	}

	function handleOpenChange(newOpenState: boolean) {
		open = newOpenState;
		if (!newOpenState) {
			apiKeyToEdit = null;
		}
	}
</script>

<ResponsiveDialog.Root
	{open}
	onOpenChange={handleOpenChange}
	variant="sheet"
	title={isStaticApiKey
		? (apiKeyToEdit?.name ?? m.api_key_static_title())
		: isBootstrapApiKey
			? (apiKeyToEdit?.name ?? m.api_key_bootstrap_title())
			: isEditMode
				? m.api_key_edit_title()
				: m.api_key_create_title()}
	description={isEditMode
		? isStaticApiKey
			? m.api_key_static_description()
			: isBootstrapApiKey
				? m.api_key_bootstrap_description()
				: m.api_key_edit_description({ name: apiKeyToEdit?.name ?? m.common_unknown() })
		: m.api_key_create_description()}
	contentClass="sm:max-w-[500px]"
>
	{#snippet children()}
		<form onsubmit={preventDefault(handleSubmit)} class="grid gap-4 py-6">
			{#if isBootstrapApiKey && !isStaticApiKey}
				<p class="text-muted-foreground text-sm">{m.api_key_bootstrap_locked_description()}</p>
			{/if}
			<FormInput
				label={m.api_key_name()}
				type="text"
				placeholder={m.api_key_name_placeholder()}
				description={m.api_key_name_description()}
				bind:input={$inputs.name}
				disabled={isReadOnlyApiKey}
			/>
			<FormInput
				label={m.api_key_description_label()}
				type="text"
				placeholder={m.api_key_description_placeholder()}
				description={m.api_key_description_help()}
				bind:input={$inputs.description}
				disabled={isReadOnlyApiKey}
			/>
			<FormInput
				label={m.api_key_expires_at()}
				type="date"
				description={m.api_key_expires_at_description()}
				bind:input={$inputs.expiresAt}
				disabled={isReadOnlyApiKey}
			/>
			{#if !isReadOnlyApiKey}
				<div>
					<label for="permissions" class="text-sm font-medium">{m.roles_permissions_label()}</label>
					<p class="text-muted-foreground mb-3 text-xs">{m.api_key_permissions_description()}</p>
					<PermissionPicker {manifest} bind:selected={$inputs.permissions.value} showSearch />
					{#if $inputs.permissions.error}
						<p class="text-destructive mt-1 text-xs">{$inputs.permissions.error}</p>
					{/if}
				</div>
			{/if}
		</form>
	{/snippet}

	{#snippet footer()}
		<div class="flex w-full flex-row gap-2">
			<ArcaneButton
				action="cancel"
				tone="outline"
				type="button"
				class="flex-1"
				onclick={() => (open = false)}
				disabled={isLoading}
			/>
			{#if !isReadOnlyApiKey}
				<ArcaneButton
					action={isEditMode ? 'save' : 'create'}
					type="submit"
					class="flex-1"
					disabled={isLoading}
					loading={isLoading}
					onclick={handleSubmit}
					customLabel={isEditMode ? m.api_key_save_changes() : m.api_key_create_button()}
				/>
			{/if}
		</div>
	{/snippet}
</ResponsiveDialog.Root>
