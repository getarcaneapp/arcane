<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';
	import type { AuditLog } from '#lib/types/auth.js';
	import { formatDateTime } from '#lib/utils/formatting.js';

	let { log }: { log: AuditLog } = $props();

	const requestRows = $derived(
		[
			{ label: m.events_col_time(), value: formatDateTime(log.createdAt, { includeSeconds: true }) },
			{ label: m.audit_detail_request(), value: [log.requestMethod, log.requestPath].filter(Boolean).join(' ') },
			{ label: m.audit_detail_remote_addr(), value: log.remoteAddr ?? '' },
			{ label: m.audit_detail_user_agent(), value: log.userAgent ?? '' },
			{ label: m.audit_detail_api_key(), value: log.apiKeyId ?? '' }
		].filter((row) => row.value)
	);

	const payloads = $derived(
		[
			{ label: m.audit_detail_before(), value: log.before },
			{ label: m.audit_detail_after(), value: log.after },
			{ label: m.audit_detail_metadata(), value: log.metadata }
		].filter((payload) => payload.value !== undefined && payload.value !== null)
	);
</script>

<div class="grid gap-4 p-4">
	{#if requestRows.length > 0}
		<dl class="grid gap-x-6 gap-y-1 text-sm sm:grid-cols-key-value">
			{#each requestRows as row (row.label)}
				<dt class="text-muted-foreground">{row.label}</dt>
				<dd class="font-mono text-xs break-all">{row.value}</dd>
			{/each}
		</dl>
	{/if}
	{#each payloads as payload (payload.label)}
		<div class="space-y-1">
			<p class="text-xs font-medium text-muted-foreground">{payload.label}</p>
			<pre class="max-h-64 overflow-auto rounded-md bg-muted p-3 text-xs">{JSON.stringify(payload.value, null, 2)}</pre>
		</div>
	{/each}
</div>
