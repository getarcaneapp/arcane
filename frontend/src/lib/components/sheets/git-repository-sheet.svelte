<script lang="ts">
	import * as ResponsiveDialog from '$lib/components/ui/responsive-dialog/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import FormInput from '$lib/components/form/form-input.svelte';
	import SwitchWithLabel from '$lib/components/form/labeled-switch.svelte';
	import * as Select from '$lib/components/ui/select/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import type { GitRepository, GitRepositoryCreateDto, GitRepositoryUpdateDto } from '$lib/types/gitops.type';
	import { z } from 'zod/v4';
	import { createForm, preventDefault } from '$lib/utils/form.utils';
	import { m } from '$lib/paraglide/messages';

	type GitRepositoryFormProps = {
		open: boolean;
		repositoryToEdit: GitRepository | null;
		onSubmit: (detail: { repository: GitRepositoryCreateDto | GitRepositoryUpdateDto; isEditMode: boolean }) => void;
		isLoading: boolean;
	};

	let { open = $bindable(false), repositoryToEdit = $bindable(), onSubmit, isLoading }: GitRepositoryFormProps = $props();

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

	let formData = $derived({
		name: open && repositoryToEdit ? repositoryToEdit.name : '',
		url: open && repositoryToEdit ? repositoryToEdit.url : '',
		authType: (open && repositoryToEdit ? repositoryToEdit.authType : 'http') as 'none' | 'http' | 'ssh',
		username: open && repositoryToEdit ? repositoryToEdit.username || '' : '',
		token: '',
		sshKey: '',
		sshHostKeyVerification: (open && repositoryToEdit
			? repositoryToEdit.sshHostKeyVerification || 'accept_new'
			: 'accept_new') as 'strict' | 'accept_new' | 'skip',
		description: open && repositoryToEdit ? repositoryToEdit.description || '' : '',
		enabled: open && repositoryToEdit ? (repositoryToEdit.enabled ?? true) : true
	});

	let { inputs, ...form } = $derived(createForm<typeof formSchema>(formSchema, formData));

	let selectedAuthType = $state<{ value: string; label: string }>({
		value: 'http',
		label: m.git_repository_auth_http()
	});

	let selectedSshHostKeyVerification = $state<{ value: string; label: string }>({
		value: 'accept_new',
		label: m.git_repository_ssh_host_key_accept_new()
	});

	function getAuthTypeLabel(type: string): string {
		switch (type) {
			case 'http':
				return m.git_repository_auth_http();
			case 'ssh':
				return m.git_repository_auth_ssh();
			default:
				return m.git_repository_auth_none();
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

	$effect(() => {
		if (open && repositoryToEdit) {
			selectedAuthType = {
				value: repositoryToEdit.authType,
				label: getAuthTypeLabel(repositoryToEdit.authType)
			};
			selectedSshHostKeyVerification = {
				value: repositoryToEdit.sshHostKeyVerification || 'accept_new',
				label: getSshHostKeyVerificationLabel(repositoryToEdit.sshHostKeyVerification || 'accept_new')
			};
		} else if (open && !repositoryToEdit) {
			selectedAuthType = { value: 'http', label: m.git_repository_auth_http() };
			selectedSshHostKeyVerification = { value: 'accept_new', label: m.git_repository_ssh_host_key_accept_new() };
		}
	});

	function handleSubmit() {
		const data = form.validate();
		if (!data) return;

		const payload: GitRepositoryCreateDto | GitRepositoryUpdateDto = {
			name: data.name,
			url: data.url,
			authType: selectedAuthType.value,
			description: data.description,
			enabled: data.enabled
		};

		if (selectedAuthType.value === 'http') {
			if (data.username) payload.username = data.username;
			if (data.token) payload.token = data.token;
		} else if (selectedAuthType.value === 'ssh') {
			if (data.sshKey) payload.sshKey = data.sshKey;
			payload.sshHostKeyVerification = selectedSshHostKeyVerification.value;
		}

		onSubmit({ repository: payload, isEditMode });
	}

	function handleOpenChange(newOpenState: boolean) {
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
			<FormInput
				label={m.git_repository_name()}
				type="text"
				placeholder={m.common_name_placeholder()}
				bind:input={$inputs.name}
			/>

			<FormInput
				label={m.git_repository_url()}
				type="text"
				placeholder="https://github.com/user/repo.git"
				bind:input={$inputs.url}
			/>

			<div class="space-y-2">
				<Label for="authType">{m.git_repository_auth_type()}</Label>
				<Select.Root
					type="single"
					bind:value={selectedAuthType.value}
					onValueChange={(v) => {
						if (v) {
							selectedAuthType = { value: v, label: getAuthTypeLabel(v) };
							$inputs.authType.value = v as any;
						}
					}}
				>
					<Select.Trigger id="authType" class="w-full">
						<span>{selectedAuthType.label}</span>
					</Select.Trigger>
					<Select.Content>
						<Select.Item value="none">{m.git_repository_auth_none()}</Select.Item>
						<Select.Item value="http">{m.git_repository_auth_http()}</Select.Item>
						<Select.Item value="ssh">{m.git_repository_auth_ssh()}</Select.Item>
					</Select.Content>
				</Select.Root>
			</div>

			{#if selectedAuthType.value === 'http'}
				<FormInput label={m.common_username()} type="text" bind:input={$inputs.username} />
				<FormInput
					label={m.common_token()}
					type="password"
					placeholder={isEditMode ? m.common_keep_placeholder() : m.common_token_placeholder()}
					bind:input={$inputs.token}
				/>
			{:else if selectedAuthType.value === 'ssh'}
				{#if $inputs.sshKey}
					<div class="space-y-2">
						<Label for="sshKey">{m.git_repository_ssh_key_label()}</Label>
						<Textarea
							id="sshKey"
							bind:value={$inputs.sshKey.value}
							placeholder={m.git_repository_ssh_key_placeholder()}
							rows={6}
							class="font-mono text-xs"
						/>
					</div>
				{/if}

				<div class="space-y-2">
					<Label for="sshHostKeyVerification">{m.git_repository_ssh_host_key_verification()}</Label>
					<Select.Root
						type="single"
						bind:value={selectedSshHostKeyVerification.value}
						onValueChange={(v) => {
							if (v) {
								selectedSshHostKeyVerification = { value: v, label: getSshHostKeyVerificationLabel(v) };
								$inputs.sshHostKeyVerification.value = v as any;
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
									<span class="text-muted-foreground text-xs">{m.git_repository_ssh_host_key_accept_new_description()}</span>
								</div>
							</Select.Item>
							<Select.Item value="strict">
								<div class="flex flex-col">
									<span>{m.git_repository_ssh_host_key_strict()}</span>
									<span class="text-muted-foreground text-xs">{m.git_repository_ssh_host_key_strict_description()}</span>
								</div>
							</Select.Item>
							<Select.Item value="skip">
								<div class="flex flex-col">
									<span>{m.git_repository_ssh_host_key_skip()}</span>
									<span class="text-muted-foreground text-xs">{m.git_repository_ssh_host_key_skip_description()}</span>
								</div>
							</Select.Item>
						</Select.Content>
					</Select.Root>
					<p class="text-muted-foreground text-xs">{m.git_repository_ssh_host_key_verification_description()}</p>
				</div>
			{/if}

			<FormInput
				label={m.common_description()}
				type="text"
				placeholder={m.common_description_placeholder()}
				bind:input={$inputs.description}
			/>

			<SwitchWithLabel
				id="isEnabledSwitch"
				label={m.common_enabled()}
				description={m.common_enabled_description()}
				bind:checked={$inputs.enabled.value}
			/>
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

			<ArcaneButton
				action={isEditMode ? 'save' : 'create'}
				type="submit"
				form="git-repository-form"
				class="flex-1"
				disabled={isLoading}
				loading={isLoading}
				customLabel={isEditMode ? m.common_save_changes() : m.common_add_button({ resource: m.resource_repository_cap() })}
			/>
		</div>
	{/snippet}
</ResponsiveDialog.Root>
