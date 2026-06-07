<script lang="ts">
	import { cn } from '$lib/utils';
	import { m } from '$lib/paraglide/messages';
	import * as ArcaneTooltip from '$lib/components/arcane-tooltip';

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
		sm: 'h-5 px-2 text-[11px]',
		md: 'h-6 px-2.5 text-[12px]',
		lg: 'h-7 px-3 text-[13px]'
	};

	const variantStyles: Record<Variant, string> = {
		red: 'text-red-600 bg-red-500/10 border-red-500/20 dark:text-red-400 dark:bg-red-500/10 dark:border-red-500/30',
		purple:
			'text-purple-600 bg-purple-500/10 border-purple-500/20 dark:text-purple-400 dark:bg-purple-500/10 dark:border-purple-500/30',
		green:
			'text-emerald-600 bg-emerald-500/10 border-emerald-500/20 dark:text-emerald-400 dark:bg-emerald-500/10 dark:border-emerald-500/30',
		blue: 'text-blue-600 bg-blue-500/10 border-blue-500/20 dark:text-blue-400 dark:bg-blue-500/10 dark:border-blue-500/30',
		gray: 'text-muted-foreground bg-muted/50 border-border/50 dark:bg-muted/20 dark:border-border/20',
		amber: 'text-amber-600 bg-amber-500/10 border-amber-500/20 dark:text-amber-400 dark:bg-amber-500/10 dark:border-amber-500/30',
		pink: 'text-pink-600 bg-pink-500/10 border-pink-500/20 dark:text-pink-400 dark:bg-pink-500/10 dark:border-pink-500/30',
		indigo:
			'text-indigo-600 bg-indigo-500/10 border-indigo-500/20 dark:text-indigo-400 dark:bg-indigo-500/10 dark:border-indigo-500/30',
		cyan: 'text-cyan-600 bg-cyan-500/10 border-cyan-500/20 dark:text-cyan-400 dark:bg-cyan-500/10 dark:border-cyan-500/30',
		lime: 'text-lime-600 bg-lime-500/10 border-lime-500/20 dark:text-lime-400 dark:bg-lime-500/10 dark:border-lime-500/30',
		emerald:
			'text-emerald-600 bg-emerald-500/10 border-emerald-500/20 dark:text-emerald-400 dark:bg-emerald-500/10 dark:border-emerald-500/30',
		teal: 'text-teal-600 bg-teal-500/10 border-teal-500/20 dark:text-teal-400 dark:bg-teal-500/10 dark:border-teal-500/30',
		sky: 'text-sky-600 bg-sky-500/10 border-sky-500/20 dark:text-sky-400 dark:bg-sky-500/10 dark:border-sky-500/30',
		violet:
			'text-violet-600 bg-violet-500/10 border-violet-500/20 dark:text-violet-400 dark:bg-violet-500/10 dark:border-violet-500/30',
		fuchsia:
			'text-fuchsia-600 bg-fuchsia-500/10 border-fuchsia-500/20 dark:text-fuchsia-400 dark:bg-fuchsia-500/10 dark:border-fuchsia-500/30',
		rose: 'text-rose-600 bg-rose-500/10 border-rose-500/20 dark:text-rose-400 dark:bg-rose-500/10 dark:border-rose-500/30',
		orange:
			'text-orange-600 bg-orange-500/10 border-orange-500/20 dark:text-orange-400 dark:bg-orange-500/10 dark:border-orange-500/30'
	};

	const badgeClasses = $derived(
		cn(
			'inline-flex shrink-0 items-center justify-center whitespace-nowrap rounded-[var(--radius)] font-semibold tracking-tight',
			'border transition-all duration-300',
			sizeStyles[size as Size],
			variantStyles[variant as Variant],
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
	<ArcaneTooltip.Root>
		<ArcaneTooltip.Trigger>
			{@render BadgeContent()}
		</ArcaneTooltip.Trigger>
		<ArcaneTooltip.Content>
			<p class="max-w-xs text-xs">{tooltip}</p>
		</ArcaneTooltip.Content>
	</ArcaneTooltip.Root>
{:else}
	{@render BadgeContent()}
{/if}
