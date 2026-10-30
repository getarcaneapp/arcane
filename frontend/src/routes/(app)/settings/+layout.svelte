<script lang="ts">
	import { resolve } from '$app/paths';

	import { SettingsIcon } from '#lib/icons/index.js';
	import { SettingsAreaLayout } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';

	import type { LayoutProps } from './$types';

	let { children }: LayoutProps = $props();
</script>

<SettingsAreaLayout rootUrl={resolve('/(app)/settings')} rootTitle={m.settings()} rootIcon={SettingsIcon}>
	{@render children()}
</SettingsAreaLayout>
