<script lang="ts">
	import RoleEditor from '#lib/components/role-editor/role-editor.svelte';
	import { SettingsPageLayout } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { PermissionsManifest, Role } from '#lib/types/role.js';
	import type { RoleWritable } from '#lib/types/ui/role.js';

	type Props = {
		title: string;
		role: Role | null;
		manifest: PermissionsManifest;
		isLoading?: boolean;
		onSubmit: (payload: Pick<Role, RoleWritable>) => void | Promise<void>;
		onClone?: () => void | Promise<void>;
	};

	let { title, role, manifest, isLoading = false, onSubmit, onClone }: Props = $props();
</script>

<SettingsPageLayout {title} description={m.roles_subtitle()} pageType="form">
	{#snippet mainContent()}
		{#key role?.id ?? 'new'}
			<RoleEditor {role} {manifest} {isLoading} {onSubmit} {onClone} />
		{/key}
	{/snippet}
</SettingsPageLayout>
