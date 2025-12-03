<script lang="ts">
	import * as Button from '$lib/components/ui/button/index.js';
	import * as Select from '$lib/components/ui/select/index.js';
	import { cn } from '$lib/utils';
	import LogOutIcon from '@lucide/svelte/icons/log-out';
	import RouterIcon from '@lucide/svelte/icons/router';
	import ServerIcon from '@lucide/svelte/icons/server';
	import LanguagesIcon from '@lucide/svelte/icons/languages';
	import Sun from '@lucide/svelte/icons/sun';
	import Moon from '@lucide/svelte/icons/moon';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import type { Environment } from '$lib/types/environment.type';
	import { mode, toggleMode } from 'mode-watcher';
	import { toast } from 'svelte-sonner';
	import { m } from '$lib/paraglide/messages';
	import type { User } from '$lib/types/user.type';
	import LocalePicker from '$lib/components/locale-picker.svelte';
	import ChevronDownIcon from '@lucide/svelte/icons/chevron-down';
	import settingsStore from '$lib/stores/config-store';

	type Props = {
		user: User;
		class?: string;
	};

	let { user, class: className = '' }: Props = $props();

	let userCardExpanded = $state(false);

	const isDarkMode = $derived(mode.current === 'dark');

	const effectiveUser = $derived(user);
	const isAdmin = $derived(!!effectiveUser.roles?.includes('admin'));
	const selectedValue = $derived(environmentStore.selected?.id || '');

	async function handleEnvSelect(envId: string) {
		const env = environmentStore.available.find((e) => e.id === envId);
		if (!env || !env.enabled) return;
		try {
			await environmentStore.setEnvironment(env);
		} catch (error) {
			console.error('Failed to set environment:', error);
			toast.error('Failed to Connect to Environment');
		}
	}

	function getConnectionString(env: Environment): string {
		if (env.id === '0') {
			return $settingsStore.dockerHost || 'unix:///var/run/docker.sock';
		} else {
			return env.apiUrl;
		}
	}
</script>

<div class={`bg-muted/30 border-border dark:border-border/20 overflow-hidden rounded-3xl border-2 ${className}`}>
	<button
		class="hover:bg-muted/40 flex w-full items-center gap-4 p-5 text-left transition-all duration-200"
		onclick={() => (userCardExpanded = !userCardExpanded)}
	>
		<div class="bg-muted/50 flex h-14 w-14 items-center justify-center rounded-2xl">
			<span class="text-foreground text-xl font-semibold">
				{(effectiveUser.displayName || effectiveUser.username)?.charAt(0).toUpperCase() || 'U'}
			</span>
		</div>
		<div class="flex-1">
			<h3 class="text-foreground text-lg font-semibold">{effectiveUser.displayName || effectiveUser.username}</h3>
			<p class="text-muted-foreground/80 text-sm">
				{effectiveUser.roles?.join(', ')}
			</p>
		</div>
		<div class="flex items-center gap-2">
			<div
				role="button"
				aria-label="Expand user card"
				class={cn('text-muted-foreground/60 transition-transform duration-200', userCardExpanded && 'rotate-180 transform')}
			>
				<ChevronDownIcon class="size-4" />
			</div>
			<form action="/logout" method="POST">
				<Button.Root
					variant="ghost"
					size="icon"
					type="submit"
					title={m.common_logout()}
					class="text-muted-foreground hover:text-destructive hover:bg-destructive/10 h-10 w-10 rounded-xl transition-all duration-200 hover:scale-105"
					onclick={(e) => e.stopPropagation()}
				>
					<LogOutIcon size={16} />
				</Button.Root>
			</form>
		</div>
	</button>

	{#if userCardExpanded}
		<div class="border-border/20 bg-muted/10 space-y-4 border-t p-4">
			{#if isAdmin}
				<div class="bg-background/50 border-border/20 rounded-2xl border p-4">
					<div class="space-y-3">
						<div class="flex items-start gap-3">
							<div class="bg-primary/10 text-primary flex aspect-square size-8 shrink-0 items-center justify-center rounded-lg">
								{#if environmentStore.selected?.id === '0'}
									<ServerIcon class="size-4" />
								{:else}
									<RouterIcon class="size-4" />
								{/if}
							</div>
							<div class="min-w-0 flex-1">
								<div class="text-muted-foreground/70 text-xs font-medium tracking-widest uppercase">
									{m.sidebar_environment_label()}
								</div>
								<div class="text-foreground text-sm font-medium">
									{environmentStore.selected ? environmentStore.selected.name : m.sidebar_no_environment()}
								</div>
								{#if environmentStore.selected}
									<div class="text-muted-foreground/60 text-xs">
										{getConnectionString(environmentStore.selected)}
									</div>
								{/if}
							</div>
						</div>
						{#if environmentStore.available.length > 1}
							<Select.Root type="single" value={selectedValue} onValueChange={handleEnvSelect}>
								<Select.Trigger class="bg-background/50 border-border/30 text-foreground h-9 w-full text-sm font-medium">
									<span class="truncate">{m.sidebar_select_environment()}</span>
								</Select.Trigger>
								<Select.Content class="max-w-[280px] min-w-[160px]">
									{#each environmentStore.available as env (env.id)}
										<Select.Item value={env.id} disabled={!env.enabled} class="text-sm">
											{env.name}
										</Select.Item>
									{/each}
								</Select.Content>
							</Select.Root>
						{/if}
					</div>
				</div>
			{/if}

			<div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
				<div class="bg-background/50 border-border/20 rounded-2xl border p-4">
					<div class="flex h-full items-center gap-3">
						<div class="bg-primary/10 text-primary flex aspect-square size-8 items-center justify-center rounded-lg">
							<LanguagesIcon class="size-4" />
						</div>
						<div class="min-w-0 flex-1">
							<div class="text-muted-foreground/70 mb-1 text-xs font-medium tracking-widest uppercase">
								{m.common_select_locale()}
							</div>
							<div class="text-foreground text-sm font-medium"></div>
						</div>
						<LocalePicker
							inline={true}
							id="mobileLocalePicker"
							class="bg-background/50 border-border/30 text-foreground h-9 w-32 text-sm font-medium"
						/>
					</div>
				</div>

				<div class="bg-background/50 border-border/20 rounded-2xl border p-4">
					<button class="flex h-full w-full items-center gap-3 text-left" onclick={toggleMode}>
						<div class="bg-primary/10 text-primary flex aspect-square size-8 items-center justify-center rounded-lg">
							{#if isDarkMode}
								<Sun class="size-4" />
							{:else}
								<Moon class="size-4" />
							{/if}
						</div>
						<div class="flex min-w-0 flex-1 flex-col justify-center">
							<div class="text-muted-foreground/70 mb-1 text-xs font-medium tracking-widest uppercase">
								{m.common_toggle_theme()}
							</div>
							<div class="text-foreground text-sm font-medium">
								{isDarkMode ? m.sidebar_dark_mode() : m.sidebar_light_mode()}
							</div>
						</div>
					</button>
				</div>
			</div>
		</div>
	{/if}
</div>
