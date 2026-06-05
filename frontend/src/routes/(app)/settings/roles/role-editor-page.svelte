<script lang="ts">
	import RoleEditor from '$lib/components/role-editor/role-editor.svelte';
	import { SettingsPageLayout } from '$lib/layouts/index.js';
	import { ShieldAlertIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import type { CreateRole, PermissionsManifest, Role, UpdateRole } from '$lib/types/auth';

	type Props = {
		title: string;
		role: Role | null;
		manifest: PermissionsManifest;
		isLoading?: boolean;
		onSubmit: (payload: CreateRole | UpdateRole) => void | Promise<void>;
		onClone?: () => void | Promise<void>;
	};

	let { title, role, manifest, isLoading = false, onSubmit, onClone }: Props = $props();
</script>

<SettingsPageLayout {title} description={m.roles_subtitle()} icon={ShieldAlertIcon} pageType="form">
	{#snippet mainContent()}
		<RoleEditor {role} {manifest} {isLoading} {onSubmit} {onClone} />
	{/snippet}
</SettingsPageLayout>
