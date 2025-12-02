<script lang="ts">
	import { cn } from '$lib/utils';
	import { m } from '$lib/paraglide/messages';
	import * as Tooltip from '$lib/components/ui/tooltip';

	type Variant =
		| 'red'
		| 'purple'
		| 'green'
		| 'blue'
		| 'gray'
		| 'amber'
		| 'pink'
		| 'indigo'
		| 'cyan'
		| 'lime'
		| 'emerald'
		| 'teal'
		| 'sky'
		| 'violet'
		| 'fuchsia'
		| 'rose'
		| 'orange';

	type Size = 'sm' | 'md' | 'lg';
	type MinWidth = 'none' | '16' | '20' | '24' | '28';

	let {
		text = m.common_unknown(),
		variant = 'gray',
		size = 'md',
		minWidth = '20',
		tooltip,
		class: className = '',
		...restProps
	} = $props<{
		text: string;
		variant?: Variant;
		size?: Size;
		minWidth?: MinWidth;
		tooltip?: string;
		class?: string;
	}>();

	const minWidthClasses: Record<MinWidth, string> = {
		none: '',
		'16': 'min-w-16',
		'20': 'min-w-20',
		'24': 'min-w-24',
		'28': 'min-w-28'
	};

	const sizeStyles: Record<Size, string> = {
		sm: 'h-5 px-2 text-[12px] leading-[18px]',
		md: 'h-6 px-2.5 text-[13px] leading-[20px]',
		lg: 'h-7 px-3 text-[14px] leading-[22px]'
	};

	const variantStyles: Record<Variant, string> = {
		red: 'text-red-900 bg-red-200 ring-red-500 dark:text-red-300 dark:bg-red-400/15 dark:ring-red-300/25',
		purple: 'text-purple-900 bg-purple-200 ring-purple-500 dark:text-purple-300 dark:bg-purple-400/15 dark:ring-purple-300/25',
		green:
			'text-emerald-900 bg-emerald-200 ring-emerald-500 dark:text-emerald-300 dark:bg-emerald-400/15 dark:ring-emerald-300/25',
		blue: 'text-blue-900 bg-blue-200 ring-blue-500 dark:text-blue-300 dark:bg-blue-400/15 dark:ring-blue-300/25',
		gray: 'text-gray-900 bg-gray-200 ring-gray-500 dark:text-gray-300 dark:bg-gray-400/15 dark:ring-gray-300/25',
		amber: 'text-amber-900 bg-amber-200 ring-amber-500 dark:text-amber-300 dark:bg-amber-400/15 dark:ring-amber-300/25',
		pink: 'text-pink-900 bg-pink-200 ring-pink-500 dark:text-pink-300 dark:bg-pink-400/15 dark:ring-pink-300/25',
		indigo: 'text-indigo-900 bg-indigo-200 ring-indigo-500 dark:text-indigo-300 dark:bg-indigo-400/15 dark:ring-indigo-300/25',
		cyan: 'text-cyan-900 bg-cyan-200 ring-cyan-500 dark:text-cyan-300 dark:bg-cyan-400/15 dark:ring-cyan-300/25',
		lime: 'text-lime-900 bg-lime-200 ring-lime-500 dark:text-lime-300 dark:bg-lime-400/15 dark:ring-lime-300/25',
		emerald:
			'text-emerald-900 bg-emerald-200 ring-emerald-500 dark:text-emerald-300 dark:bg-emerald-400/15 dark:ring-emerald-300/25',
		teal: 'text-teal-900 bg-teal-200 ring-teal-500 dark:text-teal-300 dark:bg-teal-400/15 dark:ring-teal-300/25',
		sky: 'text-sky-900 bg-sky-200 ring-sky-500 dark:text-sky-300 dark:bg-sky-400/15 dark:ring-sky-300/25',
		violet: 'text-violet-900 bg-violet-200 ring-violet-500 dark:text-violet-300 dark:bg-violet-400/15 dark:ring-violet-300/25',
		fuchsia:
			'text-fuchsia-900 bg-fuchsia-200 ring-fuchsia-500 dark:text-fuchsia-300 dark:bg-fuchsia-400/15 dark:ring-fuchsia-300/25',
		rose: 'text-rose-900 bg-rose-200 ring-rose-500 dark:text-rose-300 dark:bg-rose-400/15 dark:ring-rose-300/25',
		orange: 'text-orange-900 bg-orange-200 ring-orange-500 dark:text-orange-300 dark:bg-orange-400/15 dark:ring-orange-300/25'
	};

	const badgeClasses = $derived(
		cn(
			// base
			'inline-flex items-center justify-center gap-1 whitespace-nowrap rounded-full font-medium',
			sizeStyles[size as Size],
			// subtle border and translucency
			'ring-1 ring-inset transition-colors',
			// variant styles
			variantStyles[variant as Variant],
			// optional fixed width
			minWidthClasses[minWidth as MinWidth],
			className
		)
	);
</script>

{#snippet BadgeContent()}
	<span class={badgeClasses} {...restProps}>
		{text}
	</span>
{/snippet}

{#if tooltip}
	<Tooltip.Root>
		<Tooltip.Trigger>
			{@render BadgeContent()}
		</Tooltip.Trigger>
		<Tooltip.Content>
			<p class="max-w-xs text-xs">{tooltip}</p>
		</Tooltip.Content>
	</Tooltip.Root>
{:else}
	{@render BadgeContent()}
{/if}
