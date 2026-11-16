<script lang="ts">
	import * as ArcaneTooltip from '#lib/components/arcane-tooltip/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { LockIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import settingsStore from '#lib/stores/config-store.svelte.js';
	import type { SettingState } from '#lib/types/settings.js';

	let { lock }: { lock?: SettingState } = $props();

	const envName = $derived(lock?.lockReason === 'env' ? lock.envName : undefined);
	const active = $derived(lock ? lock.locked : settingsStore.readOnly);
</script>

{#if active}
	<ArcaneTooltip.Root>
		{#if envName}
			<ArcaneTooltip.Trigger class="inline-flex items-center" aria-label={m.setting_lock_env_label({ envName })}>
				<LockIcon class="size-3.5 shrink-0 text-warning" />
			</ArcaneTooltip.Trigger>
		{:else}
			<ArcaneTooltip.Trigger>
				<Badge variant="amber">
					<LockIcon />
					{m.read_only_label()}
				</Badge>
			</ArcaneTooltip.Trigger>
		{/if}
		<ArcaneTooltip.Content side="bottom" class="max-w-xs">
			<div class="space-y-1">
				{#if envName}
					<p class="text-xs opacity-90">{m.setting_lock_env_description({ envName })}</p>
				{:else}
					<p class="font-medium">{m.ui_config_disabled_title()}</p>
					<p class="text-xs opacity-90">{m.ui_config_disabled_description()}</p>
				{/if}
			</div>
		</ArcaneTooltip.Content>
	</ArcaneTooltip.Root>
{/if}
