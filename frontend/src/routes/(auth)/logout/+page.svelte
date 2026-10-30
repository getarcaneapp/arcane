<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { useQueryClient } from '@tanstack/svelte-query';
	import { onMount } from 'svelte';
	import { toast } from 'svelte-sonner';

	import OidcStatusPanel from '#lib/components/oidc-status-panel.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { authService } from '#lib/services/auth-service.js';

	const queryClient = useQueryClient();

	// Runs on page entry only, so preloading the logout link never signs anyone out.
	onMount(async () => {
		const revoked = await authService.logout(queryClient);
		await goto(resolve('/(auth)/login/[[mode]]', {}), { replaceState: true });
		if (!revoked) toast.error(m.auth_logout_revocation_unconfirmed());
	});
</script>

<OidcStatusPanel busy busyTitle={m.auth_signing_out()} busyDescription={m.auth_signing_out_description()} error="" />
