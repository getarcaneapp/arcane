<script lang="ts">
	import RoleEditor from '#lib/components/role-editor/role-editor.svelte';
	import { ShieldAlertIcon } from '#lib/icons/index.js';
	import { SettingsPageLayout } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { PermissionsManifest, Role, RoleWritable } from '#lib/types/auth.js';

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

<SettingsPageLayout {title} description={m.roles_subtitle()} icon={ShieldAlertIcon} pageType="form">
	{#snippet mainContent()}
		<RoleEditor {role} {manifest} {isLoading} {onSubmit} {onClone} />
	{/snippet}
</SettingsPageLayout>
