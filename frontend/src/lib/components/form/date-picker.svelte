<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { Calendar } from '#lib/components/ui/calendar/index.js';
	import * as Popover from '#lib/components/ui/popover/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { getLocale } from '#lib/paraglide/runtime.js';
	import { cn } from '#lib/utils.js';
	import { CalendarDate, type DateValue } from '@internationalized/date';
	import { Temporal } from 'temporal-polyfill';
	import type { HTMLAttributes } from 'svelte/elements';
	import { CalendarIcon } from '#lib/icons/index.js';

	type Props = {
		value?: Temporal.PlainDate;
		id?: string;
		disabled?: boolean;
	} & HTMLAttributes<HTMLDivElement>;

	let { value = $bindable(undefined), id, disabled = false, ...restProps }: Props = $props();

	let open = $state(false);

	function toCalendarDateInternal(date: Temporal.PlainDate): CalendarDate {
		return new CalendarDate(date.year, date.month, date.day);
	}

	const calendarDisplayDate = $derived(value ? toCalendarDateInternal(value) : undefined);

	function handleCalendarInteraction(newDateValue?: DateValue) {
		value = newDateValue
			? Temporal.PlainDate.from({ year: newDateValue.year, month: newDateValue.month, day: newDateValue.day })
			: undefined;
		open = false;
	}

	function formatDateInternal(date: Temporal.PlainDate): string {
		return date.toLocaleString(getLocale(), { dateStyle: 'long' });
	}
</script>

<div class="w-full" {...restProps}>
	<Popover.Root {open} onOpenChange={(nextOpen) => (open = nextOpen)}>
		<Popover.Trigger {id} class="w-full">
			{#snippet child({ props })}
				<ArcaneButton
					{...props}
					action="base"
					tone="outline"
					class={cn('w-full justify-start text-left font-normal', !value && 'text-muted-foreground')}
					aria-label={m.select_a_date()}
					icon={CalendarIcon}
					customLabel={value ? formatDateInternal(value) : m.select_a_date()}
					{disabled}
				/>
			{/snippet}
		</Popover.Trigger>
		<Popover.Content class="w-auto p-0" align="start">
			<Calendar
				type="single"
				value={calendarDisplayDate}
				onValueChange={handleCalendarInteraction}
				locale={getLocale()}
				initialFocus
			/>
		</Popover.Content>
	</Popover.Root>
</div>
