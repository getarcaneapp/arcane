<script lang="ts">
	import { cn } from '$lib/utils';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import ThemeModeSelector from '$lib/components/theme-mode/theme-mode-selector.svelte';
	import { m } from '$lib/paraglide/messages';
	import type { User } from '$lib/types/auth';
	import LocalePicker from '$lib/components/locale-picker.svelte';
	import EnvironmentSwitcherDialog from '$lib/components/dialogs/environment-switcher-dialog.svelte';
	import settingsStore from '$lib/stores/config-store';
	import userStore from '$lib/stores/user-store';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import IfPermitted from '$lib/components/if-permitted.svelte';
	import { ArrowDownIcon, LogoutIcon, EnvironmentsIcon, RemoteEnvironmentIcon, LanguageIcon, ArrowRightIcon } from '$lib/icons';

	type Props = {
		user: User;
		class?: string;
	};

	let { user, class: className = '' }: Props = $props();

	let autoLoginEnabled = $state(false);
	$effect(() => {
		const unsub = settingsStore.autoLoginEnabled.subscribe((v) => (autoLoginEnabled = v));
		return unsub;
	});

	let userCardExpanded = $state(false);
	let envDialogOpen = $state(false);

	const effectiveUser = $derived(user);

	function getConnectionString(): string {
		if (!environmentStore.selected) return '';
		if (environmentStore.selected.id === '0') {
			return $settingsStore.dockerHost || 'unix:///var/run/docker.sock';
		} else {
			return environmentStore.selected.apiUrl;
		}
	}
</script>

<div class={`overflow-hidden rounded-3xl border-2 border-border bg-muted/30 dark:border-border/20 ${className}`}>
	<button
		class="flex w-full items-center gap-4 p-5 text-left transition-all duration-200 hover:bg-muted/40"
		onclick={() => (userCardExpanded = !userCardExpanded)}
	>
		<div class="flex h-14 w-14 items-center justify-center rounded-2xl bg-muted/50">
			<span class="text-xl font-semibold text-foreground">
				{(effectiveUser.displayName || effectiveUser.username)?.charAt(0).toUpperCase() || 'U'}
			</span>
		</div>
		<div class="flex-1">
			<h3 class="text-lg font-semibold text-foreground">{effectiveUser.displayName || effectiveUser.username}</h3>
			<p class="text-sm text-muted-foreground/80">
				{userStore.isGlobalAdmin() ? m.common_admin() : m.common_user()}
			</p>
		</div>
		<div class="flex items-center gap-2">
			<div
				role="button"
				aria-label={m.nav_expand_user_card()}
				class={cn('text-muted-foreground/60 transition-transform duration-200', userCardExpanded && 'rotate-180 transform')}
			>
				<ArrowDownIcon class="size-8" />
			</div>
			{#if !autoLoginEnabled}
				<form action="/logout" method="POST">
					<ArcaneButton
						action="base"
						tone="ghost"
						size="icon"
						type="submit"
						title={m.common_logout()}
						class="h-10 w-10 rounded-xl text-muted-foreground transition-all duration-200 hover:scale-105 hover:bg-destructive/10 hover:text-destructive"
						onclick={(e) => e.stopPropagation()}
					>
						<LogoutIcon class="size-5" />
					</ArcaneButton>
				</form>
			{/if}
		</div>
	</button>

	{#if userCardExpanded}
		<div class="space-y-4 border-t border-border/20 bg-muted/10 p-4">
			<IfPermitted adminOnly>
				<button
					class="flex w-full items-center gap-3 rounded-2xl border border-border/20 bg-background/50 p-4 text-left transition-colors hover:bg-muted/30"
					onclick={() => (envDialogOpen = true)}
				>
					<div class="flex aspect-square size-8 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
						{#if environmentStore.selected?.id === '0'}
							<EnvironmentsIcon class="size-4" />
						{:else}
							<RemoteEnvironmentIcon class="size-4" />
						{/if}
					</div>
					<div class="min-w-0 flex-1">
						<div class="text-xs font-medium tracking-widest text-muted-foreground/70 uppercase">
							{m.resource_environment_cap()}
						</div>
						<div class="text-sm font-medium text-foreground">
							{environmentStore.selected ? environmentStore.selected.name : m.sidebar_no_environment()}
						</div>
						{#if environmentStore.selected}
							<div class="truncate text-xs text-muted-foreground/60">
								{getConnectionString()}
							</div>
						{/if}
					</div>
					<ArrowRightIcon class="size-5 shrink-0 text-muted-foreground/60" />
				</button>
			</IfPermitted>

			<div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
				<div class="rounded-2xl border border-border/20 bg-background/50 p-4">
					<div class="flex h-full items-center gap-3">
						<div class="flex aspect-square size-8 items-center justify-center rounded-lg bg-primary/10 text-primary">
							<LanguageIcon class="size-4" />
						</div>
						<div class="min-w-0 flex-1">
							<div class="mb-1 text-xs font-medium tracking-widest text-muted-foreground/70 uppercase">
								{m.common_select_locale()}
							</div>
							<div class="text-sm font-medium text-foreground"></div>
						</div>
						<LocalePicker
							inline={true}
							id="mobileLocalePicker"
							class="h-9 w-32 border-border/30 bg-background/50 text-sm font-medium text-foreground"
						/>
					</div>
				</div>

				<div class="rounded-2xl border border-border/20 bg-background/50 p-4">
					<div class="flex h-full flex-col justify-center gap-2">
						<div class="text-xs font-medium tracking-widest text-muted-foreground/70 uppercase">
							{m.common_toggle_theme()}
						</div>
						<ThemeModeSelector class="grid w-full grid-cols-3" />
					</div>
				</div>
			</div>
		</div>
	{/if}
</div>

<EnvironmentSwitcherDialog bind:open={envDialogOpen} />
