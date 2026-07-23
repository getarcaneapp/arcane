<script lang="ts">
	import ErrorView from '#lib/components/error.svelte';
	import { page } from '$app/state';
	import { m } from '#lib/paraglide/messages';

	// Props can be missing in some cases (e.g., 404 on client nav)
	const props = $props<{ error?: any; status?: number }>();

	const status = $derived(props.status ?? page.status ?? 500);
	const message = $derived(
		props.error?.message ?? page.error?.message ?? (status === 404 ? m.error_not_found() : m.error_generic())
	);
</script>

<ErrorView {status} {message} />
