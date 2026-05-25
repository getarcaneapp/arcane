<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import FormInput from '$lib/components/form/form-input.svelte';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import PermissionPicker from './permission-picker.svelte';
	import type { Role, PermissionsManifest } from '$lib/types/auth';
	import { CopyIcon } from '$lib/icons';
	import { z } from 'zod/v4';
	import { createForm, preventDefault } from '$lib/utils/settings';
	import { m } from '$lib/paraglide/messages';

	type Props = {
		role: Role | null;
		manifest: PermissionsManifest;
		isLoading?: boolean;
		onSubmit: (data: { name: string; description?: string; permissions: string[] }) => void | Promise<void>;
		onClone?: () => void;
	};

	let { role, manifest, isLoading = false, onSubmit, onClone }: Props = $props();

	const isBuiltIn = $derived(role?.builtIn ?? false);
	const totalPermissions = $derived(manifest.resources.reduce((sum, r) => sum + r.actions.length, 0));

	const formSchema = z.object({
		name: z.string().min(1, m.roles_name_required()),
		description: z.string().optional().default(''),
		permissions: z.array(z.string()).min(1, m.roles_permissions_required())
	});

	const formData = $derived({
		name: role?.name ?? '',
		description: role?.description ?? '',
		permissions: role?.permissions ?? []
	});

	const { inputs, ...form } = $derived(createForm<typeof formSchema>(formSchema, formData));

	const selectedCount = $derived($inputs.permissions?.value?.length ?? 0);

	function handleSubmit() {
		if (isBuiltIn) return;
		const data = form.validate();
		if (!data) return;
		onSubmit({
			name: data.name,
			description: data.description ? data.description : undefined,
			permissions: data.permissions
		});
	}
</script>

<form onsubmit={preventDefault(handleSubmit)} novalidate class="grid grid-cols-1 gap-6 lg:grid-cols-[320px_1fr]">
	<div class="space-y-4">
		<Card.Root>
			<Card.Header>
				<Card.Title class="text-base">
					{role ? m.roles_edit_title() : m.roles_create_title()}
				</Card.Title>
			</Card.Header>
			<Card.Content class="space-y-4 p-6 pt-2">
				<div class="flex items-center gap-2">
					<StatusBadge
						text={isBuiltIn ? m.roles_built_in() : m.roles_custom()}
						variant={isBuiltIn ? 'blue' : 'green'}
						size="sm"
						minWidth="none"
					/>
				</div>

				<FormInput
					label={m.roles_name_label()}
					type="text"
					placeholder={m.roles_name_placeholder()}
					disabled={isBuiltIn || isLoading}
					bind:input={$inputs.name}
				/>

				<FormInput
					label={m.roles_description_label()}
					type="text"
					placeholder={m.roles_description_placeholder()}
					disabled={isBuiltIn || isLoading}
					bind:input={$inputs.description}
				/>

				<div>
					<div class="text-muted-foreground text-xs">
						{m.roles_permissions_count({ count: selectedCount, total: totalPermissions })}
					</div>
					{#if $inputs.permissions?.error}
						<p class="mt-1 text-sm text-red-500">{$inputs.permissions.error}</p>
					{/if}
				</div>

				{#if isBuiltIn}
					<p class="text-muted-foreground text-xs">{m.roles_built_in_note()}</p>
				{/if}

				{#if isBuiltIn && onClone}
					<ArcaneButton
						action="base"
						tone="outline"
						type="button"
						class="w-full"
						icon={CopyIcon}
						onclick={onClone}
						customLabel={m.roles_clone_button()}
						disabled={isLoading}
					/>
				{/if}

				{#if !isBuiltIn}
					<ArcaneButton
						action="save"
						type="submit"
						class="w-full"
						disabled={isLoading}
						loading={isLoading}
						onclick={handleSubmit}
						customLabel={role ? m.roles_save_changes() : m.common_create_button({ resource: m.roles_title() })}
					/>
				{/if}
			</Card.Content>
		</Card.Root>
	</div>

	<div>
		<PermissionPicker {manifest} bind:selected={$inputs.permissions.value} disabled={isBuiltIn || isLoading} />
	</div>
</form>
