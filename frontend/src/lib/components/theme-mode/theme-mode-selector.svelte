<script lang="ts">
	import { userPrefersMode, setMode } from 'mode-watcher';

	import { SunIcon, MoonIcon, MonitorIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { userService } from '#lib/services/user-service.js';
	import userStore from '#lib/stores/user-store.svelte.js';
	import { cn } from '#lib/utils.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	type Props = {
		disabled?: boolean;
		class?: string;
		'aria-describedby'?: string;
	};

	let { disabled = false, class: className = '', 'aria-describedby': ariaDescribedby }: Props = $props();

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
		if (!userStore.current) return;

		await handleApiResultWithCallbacks({
			result: await tryCatch(userService.updateMyProfile({ preferences: { themeMode: value } })),
			message: m.common_update_failed({ resource: m.theme() }),
			onSuccess: (updated) => userStore.setUser(updated)
		});
	}
</script>

<div
	class={cn('inline-flex rounded-lg bg-muted/40 p-0.5', className)}
	role="group"
	aria-label={m.common_toggle_theme()}
	aria-describedby={ariaDescribedby}
>
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
