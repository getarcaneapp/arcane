<script lang="ts">
	import { z } from 'zod/v4';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import FormInput from '#lib/components/form/form-input.svelte';
	import PermissionPicker from '#lib/components/role-editor/permission-picker.svelte';
	import SheetFooterActions from '#lib/components/sheets/sheet-footer-actions.svelte';
	import * as ResponsiveDialog from '#lib/components/ui/responsive-dialog/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import { CloseIcon } from '#lib/icons/index.js';
	import * as m from '#lib/paraglide/messages.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import type { ApiKey, ApiKeyUpdate, PersonalApiKeyCreate } from '#lib/types/auth.js';
	import type { PermissionsManifest, ApiKeyPermissionGrant } from '#lib/types/auth.js';
	import { plainDateFromInstant, plainDateToInstantString } from '#lib/utils/formatting.js';
	import { buildGlobalEnvironmentOptions, GLOBAL_ENVIRONMENT_OPTION_ID } from '#lib/utils/options.js';
	import { changedFields } from '#lib/utils/payload.js';
	import { normalizePermissionSelection } from '#lib/utils/permissions.js';
	import { createForm, preventDefault } from '#lib/utils/settings.svelte.js';

	type ApiKeyFormProps = {
		open: boolean;
		apiKeyToEdit: ApiKey | null;
		mode?: 'admin' | 'personal';
		manifest?: PermissionsManifest;
		availablePermissions?: ApiKeyPermissionGrant[];
		onSubmit: (
			data:
				| { isEditMode: true; apiKeyId: string; apiKey: ApiKeyUpdate }
				| { isEditMode: false; apiKey: PersonalApiKeyCreate & { permissions?: ApiKeyPermissionGrant[] } }
		) => void;
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
			expiresAt: z.instanceof(Temporal.PlainDate).optional(),
			grants: z
				.array(z.object({ environmentId: z.string(), permissions: z.array(z.string()) }))
				.refine((rows) => hidePermissions || rows.some((row) => row.permissions.length > 0), m.pick_at_least_one_permission())
		})
	);

	const envOptions = $derived(buildGlobalEnvironmentOptions(environmentStore.available, m.global_org_wide()));

	function initialGrantRows(): { environmentId: string; permissions: string[] }[] {
		if (hidePermissions || !manifest) return [];
		const byEnv = new Map<string, string[]>([[GLOBAL_ENVIRONMENT_OPTION_ID, []]]);
		for (const grant of availablePermissions) {
			const key = grant.environmentId ?? GLOBAL_ENVIRONMENT_OPTION_ID;
			byEnv.set(key, [...(byEnv.get(key) ?? []), grant.permission]);
		}
		return [...byEnv.entries()].map(([environmentId, permissions]) => ({
			environmentId,
			permissions: normalizePermissionSelection(manifest, permissions)
		}));
	}

	function envLabel(environmentId: string): string {
		return envOptions.find((option) => option.id === environmentId)?.name ?? environmentId;
	}

	function addGrantRow() {
		const used = new Set(inputs.grants.value.map((row) => row.environmentId));
		const next = envOptions.find((option) => !used.has(option.id));
		if (!next) return;
		inputs.grants.value = [...inputs.grants.value, { environmentId: next.id, permissions: [] }];
	}

	function dropGrantRow(position: number) {
		inputs.grants.value = inputs.grants.value.filter((_, i) => i !== position);
	}

	let formData = $derived({
		name: apiKeyToEdit?.name || '',
		description: apiKeyToEdit?.description || '',
		expiresAt: plainDateFromInstant(apiKeyToEdit?.expiresAt),
		grants: initialGrantRows()
	});

	let form = $derived(createForm<typeof formSchema>(formSchema, formData));
	let inputs = $derived(form.inputs);

	function handleSubmit() {
		if (isReadOnlyApiKey) return;

		const data = form.validate();
		if (!data) return;

		const permissions: ApiKeyPermissionGrant[] = data.grants.flatMap((row) =>
			row.permissions.map((permission) => ({
				permission,
				environmentId: row.environmentId === GLOBAL_ENVIRONMENT_OPTION_ID ? undefined : row.environmentId
			}))
		);

		if (!apiKeyToEdit) {
			onSubmit({
				isEditMode: false,
				apiKey: {
					name: data.name,
					description: data.description || undefined,
					expiresAt: data.expiresAt ? plainDateToInstantString(data.expiresAt) : undefined,
					...(hidePermissions ? {} : { permissions })
				}
			});
			return;
		}

		const originalExpiry = plainDateFromInstant(apiKeyToEdit.expiresAt);
		onSubmit({
			isEditMode: true,
			apiKeyId: apiKeyToEdit.id,
			apiKey: changedFields(
				{
					...apiKeyToEdit,
					description: apiKeyToEdit.description || null,
					expiresAt: originalExpiry ? plainDateToInstantString(originalExpiry) : null
				},
				{
					name: data.name,
					description: data.description || null,
					expiresAt: data.expiresAt ? plainDateToInstantString(data.expiresAt) : null,
					permissions: hidePermissions ? apiKeyToEdit.permissions : permissions
				}
			)
		});
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
	contentClass="sm:max-w-160"
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
				bind:input={inputs.name}
				disabled={isReadOnlyApiKey}
			/>
			<FormInput
				label={m.common_description()}
				type="text"
				placeholder={m.optional_description_placeholder()}
				description={m.api_key_description_help()}
				bind:input={inputs.description}
				disabled={isReadOnlyApiKey}
			/>
			<FormInput
				label={m.api_key_expires_at()}
				type="date"
				description={m.api_key_expires_at_description()}
				bind:input={inputs.expiresAt}
				disabled={isReadOnlyApiKey}
			/>
			{#if !isReadOnlyApiKey}
				{#if hidePermissions}
					<p class="text-sm text-muted-foreground">{m.api_key_personal_inherits_description()}</p>
				{:else if manifest}
					<div class="space-y-3">
						<div>
							<span class="text-sm font-medium">{m.permissions()}</span>
							<p class="text-xs text-muted-foreground">{m.api_key_grants_description()}</p>
						</div>
						{#each inputs.grants.value as row, position (row)}
							{@const used = new Set(inputs.grants.value.map((entry) => entry.environmentId))}
							<div class="space-y-3 rounded-md border p-3">
								<div class="flex items-center gap-2">
									<Select.Root type="single" bind:value={row.environmentId}>
										<Select.Trigger class="w-full" aria-label={m.resource_environment_cap()}>
											<span class="min-w-0 truncate">{envLabel(row.environmentId)}</span>
										</Select.Trigger>
										<Select.Content>
											{#each envOptions as option (option.id)}
												<Select.Item
													value={option.id}
													label={option.name}
													disabled={option.id !== row.environmentId && used.has(option.id)}
												>
													{option.name}
												</Select.Item>
											{/each}
										</Select.Content>
									</Select.Root>
									<ArcaneButton
										action="base"
										tone="ghost"
										size="icon"
										icon={CloseIcon}
										showLabel={false}
										customLabel={m.common_remove()}
										disabled={inputs.grants.value.length === 1}
										onclick={() => dropGrantRow(position)}
									/>
								</div>
								<PermissionPicker
									{manifest}
									bind:selected={row.permissions}
									scope={row.environmentId === GLOBAL_ENVIRONMENT_OPTION_ID ? 'global' : 'env'}
									showSearch
								/>
							</div>
						{/each}
						<ArcaneButton
							action="base"
							tone="outline"
							size="sm"
							onclick={addGrantRow}
							disabled={inputs.grants.value.length >= envOptions.length}
							customLabel={m.api_key_grants_add()}
						/>
						{#if inputs.grants.error}
							<p class="mt-1 text-xs text-destructive">{inputs.grants.error}</p>
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
