<script lang="ts">
	import { untrack } from 'svelte';
	import * as ResponsiveDialog from '#lib/components/ui/responsive-dialog/index.js';
	import SheetFooterActions from '#lib/components/sheets/sheet-footer-actions.svelte';
	import FormInput from '#lib/components/form/form-input.svelte';
	import SwitchWithLabel from '#lib/components/form/labeled-switch.svelte';
	import * as Select from '#lib/components/ui/select/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import type { GitRepository, GitRepositoryCreateDto, GitRepositoryUpdateDto } from '#lib/types/automation.js';
	import { z } from 'zod/v4';
	import { createForm, preventDefault } from '#lib/utils/settings.svelte.js';

	import { m } from '#lib/paraglide/messages.js';

	type GitRepositoryFormProps = {
		open: boolean;
		clearToken?: boolean;
		clearSshKey?: boolean;
		repositoryToEdit: GitRepository | null;
		onSubmit: (detail: { repository: GitRepositoryCreateDto | GitRepositoryUpdateDto; isEditMode: boolean }) => void;
		isLoading: boolean;
	};

	let {
		open = $bindable(false),
		clearToken = $bindable(false),
		clearSshKey = $bindable(false),
		repositoryToEdit = $bindable(),
		onSubmit,
		isLoading
	}: GitRepositoryFormProps = $props();

	let isEditMode = $derived(!!repositoryToEdit);

	const formSchema = z.object({
		name: z.string().min(1, m.common_name_required()),
		url: z.string().min(1, m.common_url_required()),
		authType: z.enum(['none', 'http', 'ssh']),
		username: z.string().optional(),
		token: z.string().optional(),
		sshKey: z.string().optional(),
		sshHostKeyVerification: z.enum(['strict', 'accept_new', 'skip']).default('accept_new'),
		description: z.string().optional(),
		enabled: z.boolean().default(true)
	});

	const formData = untrack(() => ({
		name: repositoryToEdit?.name ?? '',
		url: repositoryToEdit?.url ?? '',
		authType: (repositoryToEdit?.authType ?? 'http') as 'none' | 'http' | 'ssh',
		username: repositoryToEdit?.username || '',
		token: '',
		sshKey: '',
		sshHostKeyVerification: (repositoryToEdit?.sshHostKeyVerification || 'accept_new') as 'strict' | 'accept_new' | 'skip',
		description: repositoryToEdit?.description || '',
		enabled: repositoryToEdit?.enabled ?? true
	}));

	const form = createForm<typeof formSchema>(formSchema, formData);
	let inputs = $derived(form.inputs);

	let hasToken = $derived(!!repositoryToEdit?.hasToken);
	let hasSshKey = $derived(!!repositoryToEdit?.hasSshKey);
	let urlChanged = $derived(isEditMode && inputs.url.value.trim() !== repositoryToEdit?.url);
	let tokenNeedsAttention = $derived(urlChanged && hasToken && !clearToken && !inputs.token?.value?.trim());
	let sshKeyNeedsAttention = $derived(urlChanged && hasSshKey && !clearSshKey && !inputs.sshKey?.value?.trim());

	let selectedAuthType = $state<{ value: string; label: string }>({
		value: formData.authType,
		label: getAuthTypeLabel(formData.authType)
	});

	let selectedSshHostKeyVerification = $state<{ value: string; label: string }>({
		value: formData.sshHostKeyVerification,
		label: getSshHostKeyVerificationLabel(formData.sshHostKeyVerification)
	});

	function getAuthTypeLabel(type: string): string {
		switch (type) {
			case 'http':
				return m.git_repository_auth_http();
			case 'ssh':
				return m.git_repository_auth_ssh();
			default:
				return m.none();
		}
	}

	function getSshHostKeyVerificationLabel(mode: string): string {
		switch (mode) {
			case 'strict':
				return m.git_repository_ssh_host_key_strict();
			case 'skip':
				return m.git_repository_ssh_host_key_skip();
			default:
				return m.git_repository_ssh_host_key_accept_new();
		}
	}

	function clearCredentialErrors() {
		if (inputs.token) inputs.token.error = null;
		if (inputs.sshKey) inputs.sshKey.error = null;
	}

	function handleSubmit() {
		const data = form.validate();
		if (tokenNeedsAttention && inputs.token) inputs.token.error = m.git_repository_token_url_change();
		if (sshKeyNeedsAttention && inputs.sshKey) inputs.sshKey.error = m.git_repository_ssh_key_url_change();
		if (!data || tokenNeedsAttention || sshKeyNeedsAttention) return;

		const payload: GitRepositoryCreateDto | GitRepositoryUpdateDto = {
			name: data.name,
			url: data.url,
			authType: selectedAuthType.value,
			description: data.description,
			enabled: data.enabled
		};

		if (selectedAuthType.value === 'http') {
			if (data.username) payload.username = data.username;
		} else if (selectedAuthType.value === 'ssh') {
			payload.sshHostKeyVerification = selectedSshHostKeyVerification.value;
		}

		if (hasToken && clearToken) payload.token = '';
		else if ((selectedAuthType.value === 'http' || hasToken) && data.token) payload.token = data.token;
		if (hasSshKey && clearSshKey) payload.sshKey = '';
		else if ((selectedAuthType.value === 'ssh' || hasSshKey) && data.sshKey) payload.sshKey = data.sshKey;

		onSubmit({ repository: payload, isEditMode });
	}

	function handleOpenChange(newOpenState: boolean) {
		clearToken = false;
		clearSshKey = false;
		open = newOpenState;
	}
</script>

<ResponsiveDialog.Root
	bind:open
	onOpenChange={handleOpenChange}
	variant="sheet"
	title={isEditMode ? m.git_repository_edit_title() : m.git_repository_add_title()}
	description={isEditMode ? m.common_edit_description() : m.common_add_description()}
	contentClass="sm:max-w-md"
>
	{#snippet children()}
		<form id="git-repository-form" onsubmit={preventDefault(handleSubmit)} class="grid gap-4 py-6">
			<FormInput label={m.git_repository_name()} type="text" placeholder={m.common_name_placeholder()} bind:input={inputs.name} />

			<FormInput
				label={m.git_repository_url()}
				type="text"
				placeholder={m.git_repository_url_placeholder()}
				oninput={clearCredentialErrors}
				bind:input={inputs.url}
			/>

			<div class="space-y-2">
				<Label for="authType">{m.git_repository_auth_type()}</Label>
				<Select.Root
					type="single"
					bind:value={selectedAuthType.value}
					onValueChange={(v) => {
						if (v === 'none' || v === 'http' || v === 'ssh') {
							selectedAuthType = { value: v, label: getAuthTypeLabel(v) };
							inputs.authType.value = v;
						}
					}}
				>
					<Select.Trigger id="authType" class="w-full">
						<span>{selectedAuthType.label}</span>
					</Select.Trigger>
					<Select.Content>
						<Select.Item value="none">{m.none()}</Select.Item>
						<Select.Item value="http">{m.git_repository_auth_http()}</Select.Item>
						<Select.Item value="ssh">{m.git_repository_auth_ssh()}</Select.Item>
					</Select.Content>
				</Select.Root>
			</div>

			{#if selectedAuthType.value === 'http'}
				<FormInput label={m.common_username()} type="text" bind:input={inputs.username} />
			{/if}
			{#if selectedAuthType.value === 'http' || hasToken}
				<FormInput
					label={m.common_token()}
					type="password"
					placeholder={urlChanged && hasToken
						? m.common_token_placeholder()
						: hasToken
							? m.common_keep_placeholder()
							: m.common_token_placeholder()}
					warningText={tokenNeedsAttention ? m.git_repository_token_url_change() : undefined}
					disabled={clearToken}
					oninput={clearCredentialErrors}
					bind:input={inputs.token}
				/>
				{#if hasToken}
					<SwitchWithLabel
						id="clearRepositoryToken"
						label={m.git_repository_clear_token()}
						onCheckedChange={clearCredentialErrors}
						bind:checked={clearToken}
					/>
				{/if}
			{/if}
			{#if selectedAuthType.value === 'ssh' || hasSshKey}
				<FormInput
					label={m.git_repository_ssh_key_label()}
					type="textarea"
					placeholder={hasSshKey && !urlChanged ? m.common_keep_placeholder() : m.git_repository_ssh_key_placeholder()}
					warningText={sshKeyNeedsAttention ? m.git_repository_ssh_key_url_change() : undefined}
					disabled={clearSshKey}
					rows={6}
					oninput={clearCredentialErrors}
					bind:input={inputs.sshKey}
				/>
				{#if hasSshKey}
					<SwitchWithLabel
						id="clearRepositorySshKey"
						label={m.git_repository_clear_ssh_key()}
						onCheckedChange={clearCredentialErrors}
						bind:checked={clearSshKey}
					/>
				{/if}
			{/if}
			{#if selectedAuthType.value === 'ssh'}
				<div class="space-y-2">
					<Label for="sshHostKeyVerification">{m.git_repository_ssh_host_key_verification()}</Label>
					<Select.Root
						type="single"
						bind:value={selectedSshHostKeyVerification.value}
						onValueChange={(v) => {
							if (v === 'strict' || v === 'accept_new' || v === 'skip') {
								selectedSshHostKeyVerification = { value: v, label: getSshHostKeyVerificationLabel(v) };
								inputs.sshHostKeyVerification.value = v;
							}
						}}
					>
						<Select.Trigger id="sshHostKeyVerification" class="w-full">
							<span>{selectedSshHostKeyVerification.label}</span>
						</Select.Trigger>
						<Select.Content>
							<Select.Item value="accept_new">
								<div class="flex flex-col">
									<span>{m.git_repository_ssh_host_key_accept_new()}</span>
									<span class="text-xs text-muted-foreground">{m.git_repository_ssh_host_key_accept_new_description()}</span>
								</div>
							</Select.Item>
							<Select.Item value="strict">
								<div class="flex flex-col">
									<span>{m.git_repository_ssh_host_key_strict()}</span>
									<span class="text-xs text-muted-foreground">{m.git_repository_ssh_host_key_strict_description()}</span>
								</div>
							</Select.Item>
							<Select.Item value="skip">
								<div class="flex flex-col">
									<span>{m.git_repository_ssh_host_key_skip()}</span>
									<span class="text-xs text-muted-foreground">{m.git_repository_ssh_host_key_skip_description()}</span>
								</div>
							</Select.Item>
						</Select.Content>
					</Select.Root>
					<p class="text-xs text-muted-foreground">{m.git_repository_ssh_host_key_verification_description()}</p>
				</div>
			{/if}

			<FormInput
				label={m.common_description()}
				type="text"
				placeholder={m.common_description_placeholder()}
				bind:input={inputs.description}
			/>

			<SwitchWithLabel
				id="isEnabledSwitch"
				label={m.common_enabled()}
				description={m.common_enabled_description()}
				bind:checked={inputs.enabled.value}
			/>
		</form>
	{/snippet}

	{#snippet footer()}
		<SheetFooterActions
			onCancel={() => handleOpenChange(false)}
			bind:open
			cancelDisabled={isLoading}
			submitAction={isEditMode ? 'save' : 'create'}
			submitForm="git-repository-form"
			submitDisabled={isLoading}
			submitLoading={isLoading}
			submitLabel={isEditMode ? m.common_save_changes() : m.common_add_button({ resource: m.resource_repository_cap() })}
		/>
	{/snippet}
</ResponsiveDialog.Root>
