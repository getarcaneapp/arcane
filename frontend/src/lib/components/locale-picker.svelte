<script lang="ts">
	import * as Select from '$lib/components/ui/select/index.js';
	import { getLocale, type Locale } from '$lib/paraglide/runtime';
	import { m } from '$lib/paraglide/messages';
	import userStore from '$lib/stores/user-store';
	import { setLocale } from '$lib/utils/locale.util';
	import { Label } from '$lib/components/ui/label/index.js';
	import { userService } from '$lib/services/user-service';

	let {
		inline = false,
		id = 'localePicker',
		class: className = '',
		onOpenChange
	}: {
		inline?: boolean;
		id?: string;
		class?: string;
		onOpenChange?: (open: boolean) => void;
	} = $props();

	const currentLocale = $state(getLocale());
	let isOpen = $state(false);

	const locales: Record<string, string> = {
		de: 'Deutsch',
		en: 'English',
		el: 'Ελληνικά',
		eo: 'Esperanto',
		es: 'Español',
		fr: 'Français',
		nl: 'Nederlands',
		it: 'Italiano',
		zh: 'Chinese'
	};

	async function updateLocale(locale: Locale) {
		try {
			if ($userStore) {
				await userService.update($userStore.id, { ...$userStore, locale });
			}
			await setLocale(locale);
		} catch (err) {
			console.error('Failed to update locale', err);
		}
	}
</script>

<div class={`locale-picker ${className}`}>
	{#if inline}
		<Select.Root
			type="single"
			value={currentLocale}
			onValueChange={(v) => updateLocale(v as Locale)}
			open={isOpen}
			onOpenChange={(open) => {
				isOpen = open;
				onOpenChange?.(open);
			}}
		>
			<Select.Trigger
				{id}
				class="text-foreground bg-popover/90 bubble bubble-pill bubble-shadow h-9 w-32 rounded-2xl border text-sm font-medium backdrop-blur-md"
			>
				<span class="truncate">{locales[currentLocale]}</span>
			</Select.Trigger>
			<Select.Content class="bg-card/60 bubble-shadow max-w-[280px] min-w-[160px] rounded-xl backdrop-blur-sm">
				{#each Object.entries(locales) as [value, label]}
					<Select.Item class="text-sm" {value}>{label}</Select.Item>
				{/each}
			</Select.Content>
		</Select.Root>
	{:else}
		<div class="px-3 py-2">
			<div class="grid gap-2">
				<Label for={id} class="text-sm leading-none font-medium">
					{m.language()}
				</Label>
				<Select.Root
					type="single"
					value={currentLocale}
					onValueChange={(v) => updateLocale(v as Locale)}
					open={isOpen}
					onOpenChange={(open) => {
						isOpen = open;
						onOpenChange?.(open);
					}}
				>
					<Select.Trigger
						{id}
						class="bg-popover/90 bubble bubble-pill bubble-shadow h-9 w-full justify-between rounded-2xl border backdrop-blur-md"
						aria-label={m.common_select_locale()}
					>
						<span class="truncate">{locales[currentLocale]}</span>
					</Select.Trigger>
					<Select.Content class="bg-card/60 bubble-shadow rounded-xl backdrop-blur-sm">
						{#each Object.entries(locales) as [value, label]}
							<Select.Item {value}>{label}</Select.Item>
						{/each}
					</Select.Content>
				</Select.Root>
			</div>
		</div>
	{/if}
</div>
