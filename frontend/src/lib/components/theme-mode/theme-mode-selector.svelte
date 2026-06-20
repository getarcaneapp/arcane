<script lang="ts">
	import { userPrefersMode, setMode } from 'mode-watcher';
	import { m } from '$lib/paraglide/messages';
	import { SunIcon, MoonIcon, MonitorIcon } from '$lib/icons';
	import { cn } from '$lib/utils';

	type Props = {
		disabled?: boolean;
		class?: string;
	};

	let { disabled = false, class: className = '' }: Props = $props();

	const options = $derived([
		{ value: 'light', label: m.sidebar_light_mode(), icon: SunIcon },
		{ value: 'dark', label: m.sidebar_dark_mode(), icon: MoonIcon },
		{ value: 'system', label: m.sidebar_system_mode(), icon: MonitorIcon }
	] as const);

	const current = $derived(userPrefersMode.current);
</script>

<div class={cn('bg-muted/40 inline-flex rounded-lg p-0.5', className)} role="group" aria-label={m.common_toggle_theme()}>
	{#each options as option (option.value)}
		{@const Icon = option.icon}
		<button
			type="button"
			{disabled}
			aria-pressed={current === option.value}
			onclick={() => setMode(option.value)}
			class={cn(
				'inline-flex items-center justify-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium transition-colors',
				current === option.value ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'
			)}
		>
			<Icon class="size-3.5" />
			{option.label}
		</button>
	{/each}
</div>
