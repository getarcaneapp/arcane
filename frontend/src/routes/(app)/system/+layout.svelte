<script lang="ts">
	import { resolve } from '$app/paths';

	import { MonitorIcon } from '#lib/icons/index.js';
	import { SettingsAreaLayout } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';

	import type { LayoutProps } from './$types';

	let { children }: LayoutProps = $props();
</script>

<SettingsAreaLayout rootUrl={resolve('/(app)/system')} rootTitle={m.system()} rootIcon={MonitorIcon}>
	{@render children()}
</SettingsAreaLayout>
