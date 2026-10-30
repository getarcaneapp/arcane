<script lang="ts">
	import { resolve } from '$app/paths';

	import ErrorView from '#lib/components/error.svelte';
	import { m } from '#lib/paraglide/messages.js';
</script>

<ErrorView
	title={m.no_access_page_title()}
	message={m.no_access_page_body()}
	status={403}
	actionHref={resolve('/(auth)/logout')}
	actionLabel={m.common_logout()}
/>
