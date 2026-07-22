<script lang="ts">
	import * as ResponsiveDialog from '$lib/components/ui/responsive-dialog/index.js';
	import SheetFooterActions from '$lib/components/sheets/sheet-footer-actions.svelte';
	import FormInput from '$lib/components/form/form-input.svelte';
	import PermissionPicker from '$lib/components/role-editor/permission-picker.svelte';
	import type { ApiKey } from '$lib/types/auth';
	import type { PermissionsManifest, ApiKeyPermissionGrant } from '$lib/types/auth';
	import { normalizePermissionSelection } from '$lib/utils/permissions';
	import { z } from 'zod/v4';
	import { createForm, preventDefault } from '$lib/utils/settings';
	import * as m from '$lib/paraglide/messages.js';

	type ApiKeyFormProps = {
		open: boolean;
		apiKeyToEdit: ApiKey | null;
		mode?: 'admin' | 'personal';
		manifest?: PermissionsManifest;
		availablePermissions?: ApiKeyPermissionGrant[];
		onSubmit: (data: {
			apiKey: {
				name: string;
				description?: string;
				expiresAt?: string;
				permissions?: ApiKeyPermissionGrant[];
			};
			isEditMode: boolean;
			apiKeyId?: string;
		}) => void;
		isLoading: boolean;
	};

	let {
		open = $bindable(false),
		apiKeyToEdit = $bindable(),
		mode = 'admin',
		manifest,
		availablePermissions = [],
		onSubmit,
		isLoading
	}: ApiKeyFormProps = $props();

	let isEditMode = $derived(!!apiKeyToEdit);
	let isStaticApiKey = $derived(apiKeyToEdit?.isStatic ?? false);
	let isBootstrapApiKey = $derived(apiKeyToEdit?.isBootstrap ?? false);
	let isReadOnlyApiKey = $derived(isStaticApiKey || isBootstrapApiKey);
	// Personal keys carry no grants; they inherit the owner's role permissions.
	const hidePermissions = $derived(mode === 'personal' || apiKeyToEdit?.kind === 'personal');

	const formSchema = $derived(
		z.object({
			name: z.string().min(1, m.common_field_required({ field: m.common_name() })),
			description: z.string().optional(),
			expiresAt: z.date().optional(),
			permissions: hidePermissions
				? z.array(z.string()).default([])
				: z.array(z.string()).min(1, m.pick_at_least_one_permission())
		})
	);

	let formData = $derived({
		name: apiKeyToEdit?.name || '',
		description: apiKeyToEdit?.description || '',
		expiresAt: apiKeyToEdit?.expiresAt ? new Date(apiKeyToEdit.expiresAt) : undefined,
		permissions:
			hidePermissions || !manifest
				? []
				: normalizePermissionSelection(
						manifest,
						availablePermissions.map((p) => p.permission)
					)
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
			// env-scoped picking is a follow-up. Personal keys carry no grants.
			...(hidePermissions ? {} : { permissions: data.permissions.map((p) => ({ permission: p })) })
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
				: m.create_api_key()}
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
				<p class="text-sm text-muted-foreground">{m.api_key_bootstrap_locked_description()}</p>
			{/if}
			<FormInput
				label={m.common_name()}
				type="text"
				placeholder={m.api_key_name_placeholder()}
				description={m.api_key_name_description()}
				bind:input={$inputs.name}
				disabled={isReadOnlyApiKey}
			/>
			<FormInput
				label={m.common_description()}
				type="text"
				placeholder={m.optional_description_placeholder()}
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
				{#if hidePermissions}
					<p class="text-sm text-muted-foreground">{m.api_key_personal_inherits_description()}</p>
				{:else if manifest}
					<div>
						<label for="permissions" class="text-sm font-medium">{m.permissions()}</label>
						<p class="mb-3 text-xs text-muted-foreground">{m.api_key_permissions_description()}</p>
						<PermissionPicker {manifest} bind:selected={$inputs.permissions.value} showSearch />
						{#if $inputs.permissions.error}
							<p class="mt-1 text-xs text-destructive">{$inputs.permissions.error}</p>
						{/if}
					</div>
				{/if}
			{/if}
		</form>
	{/snippet}

	{#snippet footer()}
		<SheetFooterActions
			bind:open
			cancelDisabled={isLoading}
			showSubmit={!isReadOnlyApiKey}
			submitAction={isEditMode ? 'save' : 'create'}
			submitDisabled={isLoading}
			submitLoading={isLoading}
			onSubmit={handleSubmit}
			submitLabel={isEditMode ? m.common_save_changes() : m.create_api_key()}
		/>
	{/snippet}
</ResponsiveDialog.Root>
