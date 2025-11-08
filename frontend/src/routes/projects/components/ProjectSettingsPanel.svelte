<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import SwitchWithLabel from '$lib/components/form/labeled-switch.svelte';
	import ZapIcon from '@lucide/svelte/icons/zap';
	import GlobeIcon from '@lucide/svelte/icons/globe';
	import settingsStore from '$lib/stores/config-store';
	import * as ButtonGroup from '$lib/components/ui/button-group';
	import { Button } from '$lib/components/ui/button';
	import { m } from '$lib/paraglide/messages';
	import CronScheduleSelect from '$lib/components/cron-schedule-select.svelte';

	let {
		autoUpdate = $bindable(),
		autoUpdateCron = $bindable<string | null>()
	}: { autoUpdate?: boolean | null; autoUpdateCron?: string | null } = $props();

	const globalSettings = $derived($settingsStore);
	const globalAutoUpdate = $derived(globalSettings?.autoUpdate);

	const normalizeEmptyValue = (val: string | null): string | null => {
		return val === '' || val === null || val === undefined ? null : val;
	};

	const hasAutoUpdate = $derived(autoUpdate !== null);

	// Normalize autoUpdateCron whenever it changes
	$effect(() => {
		if (autoUpdateCron !== undefined && autoUpdateCron !== normalizeEmptyValue(autoUpdateCron)) {
			autoUpdateCron = normalizeEmptyValue(autoUpdateCron);
		}
	});
</script>

<div class="space-y-4 p-4 sm:space-y-6 sm:p-6">
	<fieldset class="relative space-y-4 sm:space-y-6">
		<Card.Root>
			<Card.Header icon={ZapIcon}>
				<div class="flex w-full flex-col space-y-3">
					<div class="flex flex-row items-center justify-between gap-3">
						<Card.Title class="flex-1">{m.project_settings_auto_update_title()}</Card.Title>
						<ButtonGroup.ButtonGroup class="shrink-0">
							<Button
								variant={!hasAutoUpdate ? 'default' : 'ghost'}
								size="sm"
								class="h-7 px-2.5 text-xs"
								onclick={() => (autoUpdate = null)}
							>
								<GlobeIcon class="mr-1 size-3" />
								{m.project_settings_global()}
							</Button>
							<Button
								variant={hasAutoUpdate ? 'default' : 'ghost'}
								size="sm"
								class="h-7 px-2.5 text-xs"
								onclick={() => {
									autoUpdate = globalAutoUpdate ?? true;
								}}
							>
								<ZapIcon class="mr-1 size-3" />
								{m.project_settings_project()}
							</Button>
						</ButtonGroup.ButtonGroup>
					</div>
					<Card.Description>
						{#if !hasAutoUpdate}
							{m.project_settings_using_global()}
						{:else}
							{m.project_settings_configure_project()}
						{/if}
					</Card.Description>
				</div>
			</Card.Header>
			{#if hasAutoUpdate}
				<Card.Content class="px-3 py-4 sm:px-6">
					<div class="border-primary/20 space-y-4 border-l-2 pl-4">
						<SwitchWithLabel
							id="project-auto-update"
							checked={autoUpdate!}
							onCheckedChange={(checked) => (autoUpdate = checked)}
							label={m.project_settings_enable_auto_update()}
							description={m.project_settings_enable_description()}
						/>

						{#if autoUpdate}
							<div class="border-primary/20 space-y-3 border-l-2 pl-3">
								<CronScheduleSelect bind:value={autoUpdateCron} />
							</div>
						{/if}
					</div>
				</Card.Content>
			{/if}
		</Card.Root>
	</fieldset>
</div>
