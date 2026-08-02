<script lang="ts">
	import { userPrefersMode, setMode } from 'mode-watcher';
	import { m } from '#lib/paraglide/messages';
	import { SunIcon, MoonIcon, MonitorIcon } from '#lib/icons';
	import { cn } from '#lib/utils';
	import { userService } from '#lib/services/user-service';
	import userStore from '#lib/stores/user-store';

	type Props = {
		disabled?: boolean;
		class?: string;
	};

	let { disabled = false, class: className = '' }: Props = $props();

	const options = $derived([
		{ value: 'light', label: m.sidebar_light_mode(), icon: SunIcon },
		{ value: 'dark', label: m.sidebar_dark_mode(), icon: MoonIcon },
		{ value: 'system', label: m.system(), icon: MonitorIcon }
	] as const);

	const current = $derived(userPrefersMode.current);

	// Applied to this device immediately, then stored on the account so the
	// choice follows the user to their other devices.
	async function selectMode(value: 'light' | 'dark' | 'system') {
		setMode(value);
		if (!$userStore) return;
		try {
			const updated = await userService.updateMyProfile({ preferences: { themeMode: value } });
			await userStore.setUser(updated);
		} catch (err) {
			console.error('Failed to save theme mode', err);
		}
	}
</script>

<div class={cn('inline-flex rounded-lg bg-muted/40 p-0.5', className)} role="group" aria-label={m.common_toggle_theme()}>
	{#each options as option (option.value)}
		{@const Icon = option.icon}
		<button
			type="button"
			{disabled}
			aria-pressed={current === option.value}
			onclick={() => void selectMode(option.value)}
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
