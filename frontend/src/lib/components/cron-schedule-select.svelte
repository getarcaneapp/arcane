<script lang="ts">
	import SelectWithLabel from '$lib/components/form/select-with-label.svelte';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
	import { m } from '$lib/paraglide/messages';

	let {
		value = $bindable<string | null>(),
		error = null,
		label = m.project_settings_update_schedule(),
		placeholder = m.docker_polling_interval_placeholder_select(),
		disabled = false
	}: {
		value: string | null;
		error?: string | null;
		label?: string;
		placeholder?: string;
		disabled?: boolean;
	} = $props();

	const cronScheduleOptions = [
		{
			value: null,
			label: m.cron_immediate(),
			description: m.cron_immediate_description()
		},
		{
			value: '0 2 * * 6,0',
			label: m.cron_weekends_at({ hour: '2am' }),
			description: m.cron_weekends_at({ hour: '2am' })
		},
		{
			value: '0 3 * * 1-5',
			label: m.cron_weekdays_at({ hour: '3am' }),
			description: m.cron_weekdays_at({ hour: '3am' })
		},
		{
			value: '0 0 * * *',
			label: m.cron_daily_at({ time: 'midnight' }),
			description: m.cron_daily_at({ time: 'midnight' })
		},
		{
			value: '0 2 * * *',
			label: m.cron_daily_at({ time: '2am' }),
			description: m.cron_daily_at({ time: '2am' })
		},
		{
			value: '0 */6 * * *',
			label: m.cron_every_n_hours({ hours: '6' }),
			description: m.cron_every_n_hours({ hours: '6' })
		},
		{
			value: '0 */12 * * *',
			label: m.cron_every_n_hours({ hours: '12' }),
			description: m.cron_every_n_hours({ hours: '12' })
		},
		{
			value: 'custom',
			label: m.custom(),
			description: m.cron_custom_description()
		}
	] as const;

	let cronScheduleMode = $state<string | null>(
		(() => {
			const cron = value;
			if (!cron || cron.trim() === '') return null; // Default to immediate
			const found = cronScheduleOptions.find((o) => o.value === cron);
			return found?.value ?? 'custom';
		})()
	);

	$effect(() => {
		if (cronScheduleMode !== 'custom') {
            // NOTE: Temporary workaround for save/reset state management
			value = cronScheduleMode === null ? '' : cronScheduleMode;
		}
	});
</script>

<div class="space-y-3">
	<SelectWithLabel
		id="cronScheduleMode"
		name="cronScheduleMode"
		value={cronScheduleMode ?? 'null'}
		onValueChange={(v) => (cronScheduleMode = v === 'null' ? null : v)}
		{label}
		{placeholder}
		{disabled}
		options={cronScheduleOptions.map(({ value: v, label: l, description }) => ({
			value: v === null ? 'null' : v,
			label: l,
			description
		}))}
	/>

	{#if cronScheduleMode === 'custom'}
		<TextInputWithLabel
			value={value ?? ''}
			{error}
			label={m.custom()}
			placeholder="0 2 * * *"
			helpText={m.cron_help_text()}
			type="text"
			{disabled}
			onChange={(v) => (value = v || null)}
		/>
	{/if}
</div>
