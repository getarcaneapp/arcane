<script lang="ts">
	import EnvironmentSettingsPage from '#lib/components/settings/environment/environment-settings-page.svelte';
	import TunnelTab from '#lib/components/settings/environment/tunnel-tab.svelte';
	import { m } from '#lib/paraglide/messages.js';
</script>

<EnvironmentSettingsPage
	title={m.agent_tunnel_settings()}
	description={m.agent_tunnel_settings_description()}
	keys={['http2ReceiveWindowMb', 'agentTunnelStreamWindowMb']}
>
	{#snippet content({ formInputs, mixed, lock })}
		<TunnelTab {formInputs} {mixed} {lock} />
	{/snippet}
</EnvironmentSettingsPage>
