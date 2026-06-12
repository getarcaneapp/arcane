<script lang="ts">
	import { Calendar as CalendarPrimitive } from 'bits-ui';
	import Cell from './calendar-cell.svelte';
	import Day from './calendar-day.svelte';
	import Grid from './calendar-grid.svelte';
	import Header from './calendar-header.svelte';
	import Months from './calendar-months.svelte';
	import GridRow from './calendar-grid-row.svelte';
	import GridBody from './calendar-grid-body.svelte';
	import GridHead from './calendar-grid-head.svelte';
	import HeadCell from './calendar-head-cell.svelte';
	import NextButton from './calendar-next-button.svelte';
	import PrevButton from './calendar-prev-button.svelte';
	import Month from './calendar-month.svelte';
	import Nav from './calendar-nav.svelte';
	import Caption from './calendar-caption.svelte';
	import { cn, type WithoutChildrenOrChild } from '$lib/utils.js';
	import type { ButtonVariant } from '../button/button.svelte';
	import { isEqualMonth, type DateValue } from '@internationalized/date';
	import type { Snippet } from 'svelte';

	let {
		ref = $bindable(null),
		value = $bindable(),
		placeholder = $bindable(),
		class: className,
		weekdayFormat = 'short',
		buttonVariant = 'ghost',
		captionLayout = 'label',
		locale = 'en-US',
		months: monthsProp,
		years,
		monthFormat: monthFormatProp,
		yearFormat = 'numeric',
		day,
		disableDaysOutsideMonth = false,
		...restProps
	}: WithoutChildrenOrChild<CalendarPrimitive.RootProps> & {
		buttonVariant?: ButtonVariant;
		captionLayout?: 'dropdown' | 'dropdown-months' | 'dropdown-years' | 'label';
		months?: CalendarPrimitive.MonthSelectProps['months'];
		years?: CalendarPrimitive.YearSelectProps['years'];
		monthFormat?: CalendarPrimitive.MonthSelectProps['monthFormat'];
		yearFormat?: CalendarPrimitive.YearSelectProps['yearFormat'];
		day?: Snippet<[{ day: DateValue; outsideMonth: boolean }]>;
	} = $props();

	const monthFormat = $derived.by(() => {
		if (monthFormatProp) return monthFormatProp;
		if (captionLayout.startsWith('dropdown')) return 'short';
		return 'long';
	});
</script>

<!--
Discriminated Unions + Destructing (required for bindable) do not
get along, so we shut typescript up by casting `value` to `never`.
-->
<CalendarPrimitive.Root
	bind:value={value as never}
	bind:ref
	bind:placeholder
	{weekdayFormat}
	{disableDaysOutsideMonth}
	class={cn(
		'bg-background group/calendar p-3 [--cell-size:--spacing(8)] [[data-slot=card-content]_&]:bg-transparent [[data-slot=popover-content]_&]:bg-transparent',
		className
	)}
	{locale}
	{monthFormat}
	{yearFormat}
	{...restProps}
>
	{#snippet children({ months, weekdays })}
		<Months>
			<Nav>
				<PrevButton variant={buttonVariant} />
				<NextButton variant={buttonVariant} />
			</Nav>
			{#each months as month, monthIndex (month)}
				<Month>
					<Header>
						<Caption
							{captionLayout}
							months={monthsProp}
							{monthFormat}
							{years}
							{yearFormat}
							month={month.value}
							bind:placeholder
							{locale}
							{monthIndex}
						/>
					</Header>
					<Grid>
						<GridHead>
							<GridRow class="select-none">
								{#each weekdays as weekday (weekday)}
									<HeadCell>
										{weekday.slice(0, 2)}
									</HeadCell>
								{/each}
							</GridRow>
						</GridHead>
						<GridBody>
							{#each month.weeks as weekDates (weekDates)}
								<GridRow class="mt-2 w-full">
									{#each weekDates as date (date)}
										<Cell {date} month={month.value}>
											{#if day}
												{@render day({
													day: date,
													outsideMonth: !isEqualMonth(date, month.value)
												})}
											{:else}
												<Day />
											{/if}
										</Cell>
									{/each}
								</GridRow>
							{/each}
						</GridBody>
					</Grid>
				</Month>
			{/each}
		</Months>
	{/snippet}
</CalendarPrimitive.Root>
