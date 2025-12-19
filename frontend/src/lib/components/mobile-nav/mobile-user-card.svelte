<script lang="ts">
	import { cn } from '$lib/utils';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { mode, toggleMode } from 'mode-watcher';
	import { m } from '$lib/paraglide/messages';
	import type { User } from '$lib/types/user.type';
	import LocalePicker from '$lib/components/locale-picker.svelte';
	import EnvironmentSwitcherDialog from '$lib/components/dialogs/environment-switcher-dialog.svelte';
	import settingsStore from '$lib/stores/config-store';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import {
		ArrowDownIcon,
		MoonIcon,
		SunIcon,
		LogoutIcon,
		EnvironmentsIcon,
		RemoteEnvironmentIcon,
		LanguageIcon,
		ArrowRightIcon
	} from '$lib/icons';

	type Props = {
		user: User;
		class?: string;
	};

	let { user, class: className = '' }: Props = $props();

	let userCardExpanded = $state(false);
	let envDialogOpen = $state(false);

	const isDarkMode = $derived(mode.current === 'dark');

	const effectiveUser = $derived(user);
	const isAdmin = $derived(!!effectiveUser.roles?.includes('admin'));

	function getConnectionString(): string {
		if (!environmentStore.selected) return '';
		if (environmentStore.selected.id === '0') {
			return $settingsStore.dockerHost || 'unix:///var/run/docker.sock';
		} else {
			return environmentStore.selected.apiUrl;
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
				<ArrowDownIcon class="size-8" />
			</div>
			<form action="/logout" method="POST">
				<ArcaneButton
					action="base"
					tone="ghost"
					size="icon"
					type="submit"
					title={m.common_logout()}
					class="text-muted-foreground hover:text-destructive hover:bg-destructive/10 h-10 w-10 rounded-xl transition-all duration-200 hover:scale-105"
					onclick={(e) => e.stopPropagation()}
				>
					<LogoutIcon class="size-5" />
				</ArcaneButton>
			</form>
		</div>
	</button>

	{#if userCardExpanded}
		<div class="border-border/20 bg-muted/10 space-y-4 border-t p-4">
			{#if isAdmin}
				<button
					class="bg-background/50 border-border/20 hover:bg-muted/30 flex w-full items-center gap-3 rounded-2xl border p-4 text-left transition-colors"
					onclick={() => (envDialogOpen = true)}
				>
					<div class="bg-primary/10 text-primary flex aspect-square size-8 shrink-0 items-center justify-center rounded-lg">
						{#if environmentStore.selected?.id === '0'}
							<EnvironmentsIcon class="size-4" />
						{:else}
							<RemoteEnvironmentIcon class="size-4" />
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
							<div class="text-muted-foreground/60 truncate text-xs">
								{getConnectionString()}
							</div>
						{/if}
					</div>
					<ArrowRightIcon class="text-muted-foreground/60 size-5 shrink-0" />
				</button>
			{/if}

			<div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
				<div class="bg-background/50 border-border/20 rounded-2xl border p-4">
					<div class="flex h-full items-center gap-3">
						<div class="bg-primary/10 text-primary flex aspect-square size-8 items-center justify-center rounded-lg">
							<LanguageIcon class="size-4" />
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
								<SunIcon class="size-4" />
							{:else}
								<MoonIcon class="size-4" />
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

<EnvironmentSwitcherDialog bind:open={envDialogOpen} {isAdmin} />
