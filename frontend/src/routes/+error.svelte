<script lang="ts">
	import { page } from '$app/state';

	import ErrorView from '#lib/components/error.svelte';
	import { m } from '#lib/paraglide/messages.js';

	// Props can be missing in some cases (e.g., 404 on client nav)
	const props: { error?: App.Error | null; status?: number } = $props();

	const status = $derived(props.status ?? page.status ?? 500);
	const message = $derived(
		props.error?.message ?? page.error?.message ?? (status === 404 ? m.error_not_found() : m.error_generic())
	);
</script>

<ErrorView {status} {message} />
