<script lang="ts">
	import { goto } from '$app/navigation';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import ChevronRightIcon from '@lucide/svelte/icons/chevron-right';
	import BotIcon from '@lucide/svelte/icons/bot';
	import { Card } from '$lib/components/ui/card';
	import { m } from '$lib/paraglide/messages';

	const automationCategories = [
		{
			id: 'gitops',
			title: m.gitops_title(),
			description: m.gitops_subtitle(),
			url: '/automation/gitops',
			icon: RefreshCwIcon
		}
	];
</script>

<div class="px-2 py-4 sm:px-6 sm:py-6 lg:px-8">
	<div class="mb-6 sm:mb-8">
		<div
			class="from-background/60 via-background/40 to-background/60 relative overflow-hidden rounded-xl border bg-gradient-to-br p-4 shadow-sm sm:p-6"
		>
			<div class="bg-primary/10 pointer-events-none absolute -top-10 -right-10 size-40 rounded-full blur-3xl"></div>
			<div class="bg-muted/40 pointer-events-none absolute -bottom-10 -left-10 size-40 rounded-full blur-3xl"></div>
			<div class="relative">
				<div class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
					<div class="flex w-full items-start gap-3 sm:gap-4">
						<div
							class="bg-primary/10 text-primary ring-primary/20 flex size-8 shrink-0 items-center justify-center rounded-lg ring-1 sm:size-10"
						>
							<BotIcon class="size-4 sm:size-5" />
						</div>
						<div class="min-w-0 flex-1">
							<h1 class="min-w-0 text-xl font-bold tracking-tight sm:text-2xl">{m.automation_title()}</h1>
							<p class="text-muted-foreground mt-1 text-sm sm:text-base">{m.automation_subtitle()}</p>
						</div>
					</div>
				</div>
			</div>
		</div>
	</div>

	<div class="grid grid-cols-1 gap-4 sm:grid-cols-2 sm:gap-6 xl:grid-cols-3">
		{#each automationCategories as category}
			{@const Icon = category.icon}
			<Card class="hover:border-primary/20 group cursor-pointer transition-all duration-200 hover:shadow-md">
				<button onclick={() => goto(category.url)} class="w-full p-4 text-left sm:p-6">
					<div class="flex items-start justify-between gap-3">
						<div class="flex min-w-0 flex-1 items-start gap-3 sm:gap-4">
							<div
								class="bg-primary/5 text-primary ring-primary/10 group-hover:bg-primary/10 flex size-10 shrink-0 items-center justify-center rounded-lg ring-1 transition-colors sm:size-12"
							>
								<Icon class="size-5 sm:size-6" />
							</div>
							<div class="min-w-0 flex-1">
								<h3 class="text-sm leading-tight font-semibold sm:text-base">{category.title}</h3>
								<p class="text-muted-foreground mt-1 text-xs leading-relaxed sm:text-sm">{category.description}</p>
							</div>
						</div>
						<ChevronRightIcon class="text-muted-foreground group-hover:text-foreground mt-1 size-4 shrink-0 transition-colors" />
					</div>
				</button>
			</Card>
		{/each}
	</div>
</div>
