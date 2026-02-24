<script lang="ts">
	import * as Card from '$lib/components/ui/card/index.js';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { ArrowRightIcon, type IconType } from '$lib/icons';

	type StatusVariant =
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

	interface Props {
		title: string;
		value: string | number;
		description: string;
		icon: IconType;
		badgeText: string;
		badgeVariant?: StatusVariant;
		ctaLabel: string;
		href: string;
	}

	let { title, value, description, icon: Icon, badgeText, badgeVariant = 'red', ctaLabel, href }: Props = $props();
</script>

<Card.Root
	variant="outlined"
	class="border-border/70 bg-card/75 dark:border-surface/70 dark:bg-surface/25 shadow-sm supports-[backdrop-filter]:backdrop-blur-sm"
>
	<Card.Content class="space-y-2.5 p-4">
		<div class="flex items-center justify-between gap-2">
			<div class="text-foreground/80 flex items-center gap-2 text-sm font-medium">
				<Icon class="size-4" />
				{title}
			</div>
			<StatusBadge text={badgeText} variant={badgeVariant} size="sm" minWidth="none" />
		</div>
		<p class="text-2xl leading-none font-semibold tracking-tight">{value}</p>
		<p class="text-muted-foreground/90 text-xs leading-relaxed">{description}</p>
		<ArcaneButton
			action="base"
			tone="ghost"
			size="sm"
			{href}
			class="h-auto w-auto justify-start gap-1.5 !border-0 bg-transparent px-0 py-0 text-sm font-semibold shadow-none hover:bg-transparent"
		>
			{ctaLabel}
			<ArrowRightIcon class="size-4" />
		</ArcaneButton>
	</Card.Content>
</Card.Root>
