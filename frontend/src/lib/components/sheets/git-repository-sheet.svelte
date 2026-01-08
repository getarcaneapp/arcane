<script lang="ts">
	import * as Sheet from '$lib/components/ui/sheet/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import FormInput from '$lib/components/form/form-input.svelte';
	import SwitchWithLabel from '$lib/components/form/labeled-switch.svelte';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import * as Select from '$lib/components/ui/select/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import type { GitRepository, GitRepositoryCreateDto, GitRepositoryUpdateDto } from '$lib/types/gitops.type';
	import { z } from 'zod/v4';
	import { createForm, preventDefault } from '$lib/utils/form.utils';
	import { m } from '$lib/paraglide/messages';
	import { GitBranchIcon } from '$lib/icons';

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
		description: open && repositoryToEdit ? repositoryToEdit.description || '' : '',
		enabled: open && repositoryToEdit ? (repositoryToEdit.enabled ?? true) : true
	});

	let { inputs, ...form } = $derived(createForm<typeof formSchema>(formSchema, formData));

	let selectedAuthType = $state<{ value: string; label: string }>({
		value: 'http',
		label: m.git_repository_auth_http()
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

	$effect(() => {
		if (open && repositoryToEdit) {
			selectedAuthType = {
				value: repositoryToEdit.authType,
				label: getAuthTypeLabel(repositoryToEdit.authType)
			};
		} else if (open && !repositoryToEdit) {
			selectedAuthType = { value: 'http', label: m.git_repository_auth_http() };
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
		}

		onSubmit({ repository: payload, isEditMode });
	}
</script>

<Sheet.Root bind:open>
	<Sheet.Content class="p-6">
		<Sheet.Header class="space-y-3 border-b pb-6">
			<div class="flex items-center gap-3">
				<div class="bg-primary/10 flex size-10 shrink-0 items-center justify-center rounded-lg">
					<GitBranchIcon class="text-primary size-5" />
				</div>
				<div>
					<Sheet.Title class="text-xl font-semibold">
						{isEditMode ? m.git_repository_edit_title() : m.git_repository_add_title()}
					</Sheet.Title>
					<Sheet.Description class="text-muted-foreground mt-1 text-sm">
						{isEditMode ? m.common_edit_description() : m.common_add_description()}
					</Sheet.Description>
				</div>
			</div>
		</Sheet.Header>
		<form onsubmit={preventDefault(handleSubmit)} class="grid gap-4 py-4">
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
					<Select.Trigger id="authType">
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

			<Sheet.Footer class="flex flex-row gap-2">
				<Button
					type="button"
					class="arcane-button-cancel flex-1"
					variant="outline"
					onclick={() => (open = false)}
					disabled={isLoading}
				>
					{m.common_cancel()}
				</Button>

				<Button type="submit" class="arcane-button-create flex-1" disabled={isLoading}>
					{#if isLoading}
						<Spinner class="mr-2 size-4" />
					{/if}
					{isEditMode ? m.common_save_changes() : m.common_add_button({ resource: m.resource_repository_cap() })}
				</Button>
			</Sheet.Footer>
		</form>
	</Sheet.Content>
</Sheet.Root>
