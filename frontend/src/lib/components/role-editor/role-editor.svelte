<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import SettingsSection from '#lib/components/settings/settings-section.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import PermissionPicker from './permission-picker.svelte';
	import type { Role, PermissionsManifest } from '#lib/types/auth.js';
	import { normalizePermissionSelection } from '#lib/utils/permissions.js';
	import { CopyIcon } from '#lib/icons/index.js';
	import { z } from 'zod/v4';
	import { createForm, preventDefault } from '#lib/utils/settings.svelte.js';

	import { m } from '#lib/paraglide/messages.js';

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
		name: z.string().min(1, m.common_name_required()),
		description: z.string().optional().default(''),
		permissions: z.array(z.string()).min(1, m.pick_at_least_one_permission())
	});

	const formData = $derived({
		name: role?.name ?? '',
		description: role?.description ?? '',
		permissions: normalizePermissionSelection(manifest, role?.permissions ?? [])
	});

	const form = $derived(createForm<typeof formSchema>(formSchema, formData));
	let inputs = $derived(form.inputs);

	const selectedCount = $derived(inputs.permissions?.value?.length ?? 0);

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

<form onsubmit={preventDefault(handleSubmit)} novalidate class="grid grid-cols-1 gap-6 lg:grid-cols-aside-80">
	<SettingsSection title={m.roles_details_section()}>
		{#snippet actions()}
			<Badge variant={isBuiltIn ? 'blue' : 'green'} size="sm">{isBuiltIn ? m.roles_built_in() : m.custom()}</Badge>
		{/snippet}
		<SettingsRow for="role-name" label={m.common_name()} error={inputs.name.error}>
			<Input
				id="role-name"
				placeholder={m.roles_name_placeholder()}
				disabled={isBuiltIn || isLoading}
				bind:value={inputs.name.value}
				aria-invalid={!!inputs.name.error}
			/>
		</SettingsRow>
		<SettingsRow for="role-description" label={m.common_description()} error={inputs.description.error}>
			<Input
				id="role-description"
				placeholder={m.roles_description_placeholder()}
				disabled={isBuiltIn || isLoading}
				bind:value={inputs.description.value}
				aria-invalid={!!inputs.description.error}
			/>
		</SettingsRow>
		<div class="flex flex-col gap-3 px-5 py-4">
			<p class="text-xs text-muted-foreground">
				{m.roles_permissions_count({ count: selectedCount, total: totalPermissions })}
			</p>
			{#if inputs.permissions?.error}
				<p class="text-xs font-medium text-destructive">{inputs.permissions.error}</p>
			{/if}
			{#if isBuiltIn}
				<p class="text-xs text-muted-foreground">{m.roles_built_in_note()}</p>
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
		</div>
	</SettingsSection>

	<div>
		<PermissionPicker {manifest} bind:selected={inputs.permissions.value} disabled={isBuiltIn || isLoading} />
	</div>
</form>
