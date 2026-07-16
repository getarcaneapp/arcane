<script lang="ts">
	import settingsStore from '$lib/stores/config-store';
	import { m } from '$lib/paraglide/messages';
	import { LockIcon } from '$lib/icons';
	import { Badge } from '$lib/components/ui/badge';
	import * as ArcaneTooltip from '$lib/components/arcane-tooltip';

	const active = $derived.by(() => $settingsStore.uiConfigDisabled);
</script>

{#if active}
	<ArcaneTooltip.Root>
		<ArcaneTooltip.Trigger>
			<Badge variant="amber">
				<LockIcon />
				Read-only
			</Badge>
		</ArcaneTooltip.Trigger>
		<ArcaneTooltip.Content side="bottom" class="max-w-xs">
			<div class="space-y-1">
				<p class="font-medium">{m.ui_config_disabled_title()}</p>
				<p class="text-xs opacity-90">{m.ui_config_disabled_description()}</p>
			</div>
		</ArcaneTooltip.Content>
	</ArcaneTooltip.Root>
{/if}
