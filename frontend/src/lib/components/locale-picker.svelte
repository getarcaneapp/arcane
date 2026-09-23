<script lang="ts">
	import * as Select from '#lib/components/ui/select/index.js';
	import { getLocale, type Locale } from '#lib/paraglide/runtime.js';
	import { m } from '#lib/paraglide/messages.js';
	import userStore from '#lib/stores/user-store.svelte.js';
	import { setLocale } from '#lib/utils/formatting.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { userService } from '#lib/services/user-service.js';
	import { createMutation, useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';
	import { extractApiErrorMessage } from '#lib/utils/api.js';

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

	let currentLocale = $state<Locale>(getLocale());
	let isOpen = $state(false);
	const queryClient = useQueryClient();

	const locales: Record<string, string> = {
		cs: 'Čeština',
		da: 'Dansk',
		de: 'Deutsch',
		el: 'Ελληνικά',
		en: 'English',
		eo: 'Esperanto',
		es: 'Español',
		fr: 'Français',
		hu: 'Magyar',
		it: 'Italiano',
		ja: '日本語',
		ko: '한국어',
		nl: 'Nederlands',
		pl: 'Polski',
		'pt-BR': 'Português brasileiro',
		ru: 'Русский',
		sv: 'Svenska',
		tr: 'Türkçe',
		uk: 'Українська',
		vi: 'Tiếng Việt',
		'zh-CN': '中文',
		'zh-TW': '繁體中文'
	};

	const updateLocaleMutation = createMutation(() => ({
		mutationFn: async (locale: Locale) => {
			if (userStore.current) {
				await userService.updateMyProfile({ locale });
			}
			await setLocale(locale);
			return locale;
		},
		onMutate: (locale) => {
			const previousLocale = currentLocale;
			currentLocale = locale;
			return { previousLocale };
		},
		onSuccess: async (locale) => {
			currentLocale = locale;
			await queryClient.invalidateQueries({ queryKey: queryKeys.users.all });
		},
		onError: (err, _locale, context) => {
			currentLocale = context?.previousLocale ?? getLocale();
			toast.error(m.common_update_failed({ resource: m.language() }), { description: extractApiErrorMessage(err) });
		}
	}));

	function updateLocale(locale: Locale) {
		updateLocaleMutation.mutate(locale);
	}
</script>

{#snippet localeSelect(compact: boolean, ariaLabel?: string)}
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
		<Select.Trigger {id} class={compact ? 'h-9 w-32' : 'w-full'} aria-label={ariaLabel}>
			<span class="truncate">{locales[currentLocale]}</span>
		</Select.Trigger>
		<Select.Content class={compact ? 'max-w-70 min-w-40' : undefined}>
			{#each Object.entries(locales) as [value, label] (value)}
				<Select.Item {value}>{label}</Select.Item>
			{/each}
		</Select.Content>
	</Select.Root>
{/snippet}

<div class={className}>
	{#if inline}
		{@render localeSelect(true)}
	{:else}
		<div class="px-3 py-2">
			<div class="grid gap-2">
				<Label for={id}>
					{m.language()}
				</Label>
				{@render localeSelect(false, m.common_select_locale())}
			</div>
		</div>
	{/if}
</div>
