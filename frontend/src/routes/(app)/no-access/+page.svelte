<script lang="ts">
	import ErrorView from '#lib/components/error.svelte';
	import { m } from '#lib/paraglide/messages';

	let {}: PageProps = $props();
</script>

<ErrorView
	title={m.no_access_page_title()}
	message={m.no_access_page_body()}
	status={403}
	actionHref="/logout"
	actionLabel={m.common_logout()}
/>
