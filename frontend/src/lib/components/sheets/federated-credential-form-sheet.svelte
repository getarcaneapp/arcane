<script lang="ts">
	import * as ResponsiveDialog from '$lib/components/ui/responsive-dialog/index.js';
	import * as Select from '$lib/components/ui/select';
	import * as Alert from '$lib/components/ui/alert';
	import SheetFooterActions from '$lib/components/sheets/sheet-footer-actions.svelte';
	import RoleScopeSelects from '$lib/components/sheets/role-scope-selects.svelte';
	import FormInput from '$lib/components/form/form-input.svelte';
	import { Label } from '$lib/components/ui/label';
	import { Switch } from '$lib/components/ui/switch/index.js';
	import type { CreateFederatedCredential, FederatedCredential, FederatedCredentialMatchType, Role } from '$lib/types/auth';
	import type { Environment } from '$lib/types/environment';
	import { z } from 'zod/v4';
	import { createForm, preventDefault } from '$lib/utils/settings';
	import * as m from '$lib/paraglide/messages.js';
	import { InfoIcon } from '$lib/icons';

	type Props = {
		open: boolean;
		credentialToEdit: FederatedCredential | null;
		roles: Role[];
		environments: Environment[];
		isLoading: boolean;
		onSubmit: (data: { credential: CreateFederatedCredential; isEditMode: boolean; credentialId?: string }) => void;
	};

	let { open = $bindable(false), credentialToEdit, roles, environments, isLoading, onSubmit }: Props = $props();

	const GLOBAL_OPTION_ID = 'global';
	const MATCH_TYPES: { value: FederatedCredentialMatchType; label: string }[] = [
		{ value: 'exact', label: m.federated_credential_match_exact() },
		{ value: 'glob', label: m.federated_credential_match_glob() }
	];

	const isEditMode = $derived(!!credentialToEdit);

	const envOptions = $derived([
		{ id: GLOBAL_OPTION_ID, name: m.global() },
		...environments.map((env) => ({ id: env.id, name: env.name }))
	]);

	const formSchema = z.object({
		name: z.string().min(1, m.common_field_required({ field: m.common_name() })),
		description: z.string().optional(),
		enabled: z.boolean(),
		issuerUrl: z.url().min(1, m.federated_credential_issuer_required()),
		audience: z.string().min(1, m.federated_credential_audience_required()),
		subjectClaim: z.string().optional(),
		subjectMatch: z.string().min(1, m.federated_credential_subject_required()),
		matchType: z.enum(['exact', 'glob']),
		roleId: z.string().min(1, m.federated_credential_role_required()),
		environmentId: z.string(),
		tokenTtlSeconds: z.coerce
			.number()
			.int()
			.min(60, m.federated_credential_ttl_min())
			.max(3600, m.federated_credential_ttl_max()),
		expiresAt: z.date().optional()
	});

	const formData = $derived({
		name: credentialToEdit?.name ?? '',
		description: credentialToEdit?.description ?? '',
		enabled: credentialToEdit?.enabled ?? false,
		issuerUrl: credentialToEdit?.issuerUrl ?? '',
		audience: credentialToEdit?.audiences?.join('\n') ?? '',
		subjectClaim: credentialToEdit?.subjectClaim ?? 'sub',
		subjectMatch: credentialToEdit?.subjectMatch ?? '',
		matchType: credentialToEdit?.matchType ?? ('exact' as FederatedCredentialMatchType),
		roleId: credentialToEdit?.roleId ?? roles[0]?.id ?? '',
		environmentId: credentialToEdit?.environmentId ?? GLOBAL_OPTION_ID,
		tokenTtlSeconds: credentialToEdit?.tokenTtlSeconds ?? 900,
		expiresAt: credentialToEdit?.expiresAt ? new Date(credentialToEdit.expiresAt) : undefined
	});

	const { inputs, ...form } = $derived(createForm<typeof formSchema>(formSchema, formData));

	const hasWildcardWarning = $derived.by(() => {
		const value = String($inputs.subjectMatch.value ?? '').trim();
		return shouldWarnSubjectMatchInternal(value);
	});

	function shouldWarnSubjectMatchInternal(value: string): boolean {
		if (value === '' || value === '*') {
			return value === '*';
		}
		return value.endsWith('/*') || /^repo:[^/]+\/\*(?::|$)/.test(value);
	}

	function envSelectedLabel(value: string): string {
		return envOptions.find((o) => o.id === value)?.name ?? m.common_select_option();
	}

	function roleSelectedLabel(value: string): string {
		return roles.find((r) => r.id === value)?.name ?? m.common_select_option();
	}

	function matchTypeLabel(value: string): string {
		return MATCH_TYPES.find((option) => option.value === value)?.label ?? m.common_select_option();
	}

	function parseAudiencesInternal(value: string): string[] {
		return value
			.split(/[\n,]/)
			.map((entry) => entry.trim())
			.filter(Boolean);
	}

	function handleSubmit() {
		const data = form.validate();
		if (!data) return;

		const payload: CreateFederatedCredential = {
			name: data.name,
			description: data.description || undefined,
			enabled: data.enabled,
			issuerUrl: data.issuerUrl,
			audiences: parseAudiencesInternal(data.audience),
			subjectClaim: data.subjectClaim || 'sub',
			subjectMatch: data.subjectMatch,
			matchType: data.matchType,
			roleId: data.roleId,
			environmentId: data.environmentId === GLOBAL_OPTION_ID ? undefined : data.environmentId,
			tokenTtlSeconds: data.tokenTtlSeconds,
			expiresAt: data.expiresAt ? data.expiresAt.toISOString() : undefined
		};

		onSubmit({ credential: payload, isEditMode, credentialId: credentialToEdit?.id });
	}

	function handleOpenChange(newOpenState: boolean) {
		open = newOpenState;
	}
</script>

<ResponsiveDialog.Root
	bind:open
	onOpenChange={handleOpenChange}
	variant="sheet"
	title={isEditMode ? m.federated_credential_edit_title() : m.create_federated_credential()}
	description={isEditMode
		? m.federated_credential_edit_description({ name: credentialToEdit?.name ?? m.common_unknown() })
		: m.federated_credential_create_description()}
	contentClass="sm:max-w-[560px]"
>
	{#snippet children()}
		<form onsubmit={preventDefault(handleSubmit)} novalidate class="grid gap-4 py-6">
			<FormInput
				label={m.common_name()}
				type="text"
				placeholder={m.federated_credential_name_placeholder()}
				bind:input={$inputs.name}
				disabled={isLoading}
			/>
			<FormInput
				label={m.common_description()}
				type="text"
				placeholder={m.optional_description_placeholder()}
				bind:input={$inputs.description}
				disabled={isLoading}
			/>
			<FormInput
				label={m.federated_credential_issuer_label()}
				type="text"
				placeholder={m.federated_credential_issuer_placeholder()}
				description={m.federated_credential_issuer_description()}
				bind:input={$inputs.issuerUrl}
				disabled={isLoading}
			/>
			<FormInput
				label={m.federated_credential_audiences_label()}
				type="textarea"
				rows={3}
				placeholder={m.federated_credential_audiences_placeholder()}
				description={m.federated_credential_audiences_description()}
				bind:input={$inputs.audience}
				disabled={isLoading}
			/>
			<div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
				<FormInput
					label={m.federated_credential_subject_claim_label()}
					type="text"
					placeholder={m.federated_credential_subject_claim_placeholder()}
					bind:input={$inputs.subjectClaim}
					disabled={isLoading}
				/>
				<div class="space-y-2">
					<Label for="federated-match-type" class="mb-0">{m.federated_credential_match_type_label()}</Label>
					<Select.Root type="single" bind:value={$inputs.matchType.value} disabled={isLoading}>
						<Select.Trigger id="federated-match-type" class="w-full">
							<span>{matchTypeLabel($inputs.matchType.value)}</span>
						</Select.Trigger>
						<Select.Content>
							{#each MATCH_TYPES as option (option.value)}
								<Select.Item value={option.value} label={option.label}>
									{option.label}
								</Select.Item>
							{/each}
						</Select.Content>
					</Select.Root>
				</div>
			</div>
			<FormInput
				label={m.federated_credential_subject_match_label()}
				type="text"
				placeholder={m.federated_credential_subject_match_placeholder()}
				description={m.federated_credential_subject_match_description()}
				bind:input={$inputs.subjectMatch}
				disabled={isLoading}
			/>
			{#if hasWildcardWarning}
				<Alert.Root variant="default" class="border-amber-200 bg-amber-50 dark:border-amber-800 dark:bg-amber-950">
					<InfoIcon class="h-4 w-4 text-amber-600 dark:text-amber-500" />
					<Alert.Title class="text-amber-900 dark:text-amber-100">{m.federated_credential_wildcard_warning_title()}</Alert.Title>
					<Alert.Description class="text-amber-800 dark:text-amber-200">
						{m.federated_credential_wildcard_warning_description()}
					</Alert.Description>
				</Alert.Root>
			{/if}
			<RoleScopeSelects
				idPrefix="federated"
				roleLabel={m.common_role()}
				scopeLabel={m.common_scope()}
				{roles}
				{envOptions}
				bind:roleValue={$inputs.roleId.value}
				bind:environmentValue={$inputs.environmentId.value}
				roleError={$inputs.roleId.error}
				{roleSelectedLabel}
				{envSelectedLabel}
				disabled={isLoading}
				class="grid grid-cols-1 gap-4 sm:grid-cols-2"
			/>
			<div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
				<FormInput
					label={m.federated_credential_ttl_label()}
					type="number"
					description={m.federated_credential_ttl_description()}
					bind:input={$inputs.tokenTtlSeconds}
					disabled={isLoading}
				/>
				<FormInput
					label={m.federated_credential_expires_at_label()}
					type="date"
					description={m.federated_credential_expires_at_description()}
					bind:input={$inputs.expiresAt}
					disabled={isLoading}
				/>
			</div>
			<div class="flex items-center space-x-2">
				<Switch id="federated-enabled" bind:checked={$inputs.enabled.value} disabled={isLoading} />
				<div class="grid gap-1.5 leading-none">
					<Label for="federated-enabled" class="mb-0 text-sm leading-none font-medium">
						{m.common_enabled()}
					</Label>
					<p class="text-[0.8rem] text-muted-foreground">
						{m.federated_credential_enabled_description()}
					</p>
				</div>
				<!-- fallow-ignore-next-line code-duplication -- per-sheet footer wrapper ({#snippet footer} -> shared SheetFooterActions); ResponsiveDialog requires a footer snippet in each sheet -->
			</div>
		</form>
	{/snippet}

	{#snippet footer()}
		<SheetFooterActions
			bind:open
			cancelDisabled={isLoading}
			submitAction={isEditMode ? 'save' : 'create'}
			submitDisabled={isLoading}
			submitLoading={isLoading}
			onSubmit={handleSubmit}
			submitLabel={isEditMode ? m.common_save() : m.common_create()}
		/>
	{/snippet}
</ResponsiveDialog.Root>
