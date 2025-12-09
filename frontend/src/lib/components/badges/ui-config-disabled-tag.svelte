<script lang="ts">
	import settingsStore from '$lib/stores/config-store';
	import { m } from '$lib/paraglide/messages';
	import { LockIcon } from '$lib/icons';
	import * as Tooltip from '$lib/components/ui/tooltip';

	const active = $derived.by(() => $settingsStore.uiConfigDisabled);
</script>

{#if active}
	<Tooltip.Provider>
		<Tooltip.Root>
			<Tooltip.Trigger>
				<span
					class="inline-flex items-center gap-1.5 rounded-full bg-amber-100 px-2.5 py-1 text-xs font-medium text-amber-800 ring-1 ring-amber-200 dark:bg-amber-900/50 dark:text-amber-200 dark:ring-amber-800"
				>
					<LockIcon class="size-3" />
					Read-only
				</span>
			</Tooltip.Trigger>
			<Tooltip.Content side="bottom" class="max-w-xs">
				<div class="space-y-1">
					<p class="font-medium">{m.ui_config_disabled_title()}</p>
					<p class="text-xs opacity-90">{m.ui_config_disabled_description()}</p>
				</div>
			</Tooltip.Content>
		</Tooltip.Root>
	</Tooltip.Provider>
{/if}
