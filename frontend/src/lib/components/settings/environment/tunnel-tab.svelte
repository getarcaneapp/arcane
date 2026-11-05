<script lang="ts">
	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import SettingsSection from '#lib/components/settings/settings-section.svelte';
	import { Input } from '#lib/components/ui/input/index.js';
	import { m } from '#lib/paraglide/messages.js';

	import type { BuildsTabProps } from './tab-props';

	let { formInputs = $bindable(), settings, lock, mixed }: BuildsTabProps = $props();

	const supportsHttp2ReceiveWindow = $derived(settings.values.http2ReceiveWindowMb !== undefined);
	const supportsAgentTunnelStreamWindow = $derived(settings.values.agentTunnelStreamWindowMb !== undefined);
</script>

<div class="space-y-8">
	<SettingsSection title={m.buffer_windows()}>
		{#if supportsHttp2ReceiveWindow}
			<SettingsRow
				for="http2-receive-window"
				label={m.http2_receive_window()}
				description={m.http2_receive_window_description()}
				helpText={m.value_in_mib({ range: '1-256' })}
				warningText={m.http2_receive_window_restart()}
				error={formInputs.http2ReceiveWindowMb.error}
				lock={lock('http2ReceiveWindowMb')}
				mixed={mixed('http2ReceiveWindowMb')}
			>
				{#snippet children({ describedBy, errorId })}
					<Input
						id="http2-receive-window"
						type="number"
						placeholder={mixed('http2ReceiveWindowMb') ? m.mixed() : '32'}
						bind:value={formInputs.http2ReceiveWindowMb.value}
						aria-invalid={!!formInputs.http2ReceiveWindowMb.error}
						aria-describedby={describedBy}
						aria-errormessage={errorId}
					/>
				{/snippet}
			</SettingsRow>
		{/if}
		{#if supportsAgentTunnelStreamWindow}
			<SettingsRow
				for="agent-tunnel-stream-window"
				label={m.agent_tunnel_stream_window()}
				description={m.agent_tunnel_stream_window_description()}
				helpText={m.value_in_mib({ range: '1-64' })}
				warningText={m.agent_tunnel_stream_window_reconnect()}
				error={formInputs.agentTunnelStreamWindowMb.error}
				lock={lock('agentTunnelStreamWindowMb')}
				mixed={mixed('agentTunnelStreamWindowMb')}
			>
				{#snippet children({ describedBy, errorId })}
					<Input
						id="agent-tunnel-stream-window"
						type="number"
						placeholder={mixed('agentTunnelStreamWindowMb') ? m.mixed() : '16'}
						bind:value={formInputs.agentTunnelStreamWindowMb.value}
						aria-invalid={!!formInputs.agentTunnelStreamWindowMb.error}
						aria-describedby={describedBy}
						aria-errormessage={errorId}
					/>
				{/snippet}
			</SettingsRow>
		{/if}
	</SettingsSection>
</div>
