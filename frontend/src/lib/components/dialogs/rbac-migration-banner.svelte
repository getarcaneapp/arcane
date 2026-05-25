<script lang="ts">
	import * as Alert from '$lib/components/ui/alert';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import userStore from '$lib/stores/user-store';
	import { BUILT_IN_ROLE_VIEWER } from '$lib/types/auth';
	import { CloseIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import type { User } from '$lib/types/auth';

	const STORAGE_KEY = 'rbac-migration-banner-dismissed';

	let dismissed = $state(false);

	$effect(() => {
		if (typeof window === 'undefined') return;
		try {
			if (window.localStorage.getItem(STORAGE_KEY) === 'true') {
				dismissed = true;
			}
		} catch {
			// Ignore storage access errors (e.g., private browsing).
		}
	});

	function shouldShowFor(user: User | null): boolean {
		if (!user) return false;
		const assignments = user.roleAssignments ?? [];
		if (assignments.length === 0) return false;

		const manualAssignments = assignments.filter((a) => a.source === 'manual');
		if (manualAssignments.length === 0) return false;

		const hasViewerOnEnv = manualAssignments.some((a) => a.roleId === BUILT_IN_ROLE_VIEWER && !!a.environmentId);
		if (!hasViewerOnEnv) return false;

		const hasNonViewerManual = manualAssignments.some((a) => a.roleId !== BUILT_IN_ROLE_VIEWER);
		return !hasNonViewerManual;
	}

	const visible = $derived(!dismissed && shouldShowFor($userStore));

	function dismiss() {
		try {
			if (typeof window !== 'undefined') {
				window.localStorage.setItem(STORAGE_KEY, 'true');
			}
		} catch {
			// Ignore storage access errors.
		}
		dismissed = true;
	}
</script>

{#if visible}
	<Alert.Root variant="default" class="mb-4 flex items-start gap-3">
		<div class="flex-1">
			<Alert.Title>{m.rbac_migration_banner_title()}</Alert.Title>
			<Alert.Description>{m.rbac_migration_banner_body()}</Alert.Description>
		</div>
		<ArcaneButton
			action="base"
			tone="ghost"
			size="icon"
			icon={CloseIcon}
			showLabel={false}
			onclick={dismiss}
			customLabel={m.rbac_migration_banner_dismiss()}
			class="text-muted-foreground hover:text-foreground"
		/>
	</Alert.Root>
{/if}
