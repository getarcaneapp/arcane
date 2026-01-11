<script lang="ts">
	import * as Avatar from '$lib/components/ui/avatar/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import * as Sidebar from '$lib/components/ui/sidebar/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { useSidebar } from '$lib/components/ui/sidebar/index.js';
	import type { User } from '$lib/types/user.type';
	import { mode, toggleMode } from 'mode-watcher';
	import { cn } from '$lib/utils';
	import settingsStore from '$lib/stores/config-store';
	import { m } from '$lib/paraglide/messages';
	import LocalePicker from '$lib/components/locale-picker.svelte';
	import { getDefaultProfilePicture } from '$lib/utils/image.util';
	import { SunIcon, MoonIcon } from '$lib/icons';

	let { user, isCollapsed }: { user: User; isCollapsed: boolean } = $props();
	const sidebar = useSidebar();

	let dropdownOpen = $state(false);
	let localePickerOpen = $state(false);

	$effect(() => {
		if (sidebar.state === 'collapsed' && !sidebar.isHovered && dropdownOpen) {
			dropdownOpen = false;
		}
	});

	async function getGravatarUrl(email: string | undefined, size = 40): Promise<string> {
		if (!email) return '';

		const encoder = new TextEncoder();
		const data = encoder.encode(email.toLowerCase().trim());
		const hashBuffer = await crypto.subtle.digest('SHA-256', data);
		const hashArray = Array.from(new Uint8Array(hashBuffer));
		const hash = hashArray.map((b) => b.toString(16).padStart(2, '0')).join('');

		return `https://www.gravatar.com/avatar/${hash}?s=${size}`;
	}
</script>

<Sidebar.Menu>
	<Sidebar.MenuItem>
		<DropdownMenu.Root bind:open={dropdownOpen}>
			<DropdownMenu.Trigger>
				{#snippet child({ props })}
					<Sidebar.MenuButton
						size="lg"
						class="data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground"
						{...props}
					>
						{#if user && user.displayName}
							<Avatar.Root class="size-8 rounded-lg">
								{#if $settingsStore.enableGravatar}
									{#await getGravatarUrl(user?.email)}
										<Avatar.Image src={getDefaultProfilePicture()} alt={user.displayName} />
									{:then url}
										<Avatar.Image src={url} alt={user.displayName} />
									{:catch _}
										<Avatar.Image src={getDefaultProfilePicture()} alt={user.displayName} />
									{/await}
								{:else}
									<Avatar.Image src={getDefaultProfilePicture()} alt={user.displayName} />
								{/if}
								<Avatar.Fallback
									class="from-primary/20 to-primary/10 text-primary border-primary/20 rounded-lg border bg-linear-to-br"
								>
									{user.displayName?.charAt(0).toUpperCase()}
								</Avatar.Fallback>
							</Avatar.Root>
							{#if !isCollapsed}
								<div class="grid flex-1 pl-0 text-left text-sm leading-tight">
									<span class="truncate font-medium">{user.displayName}</span>
									<span class="truncate text-xs">{user.email}</span>
								</div>
							{/if}
						{/if}
					</Sidebar.MenuButton>
				{/snippet}
			</DropdownMenu.Trigger>
			<DropdownMenu.Content
				class="border-border/20 min-w-56 rounded-2xl border p-0 shadow-lg backdrop-blur-2xl
 backdrop-saturate-150"
				side="right"
				align="end"
				sideOffset={12}
			>
				<div
					role="group"
					tabindex="-1"
					onmouseenter={() => {
						if (sidebar.state === 'collapsed') {
							sidebar.setHovered(true);
						}
					}}
					onmouseleave={() => {
						if (!localePickerOpen) {
							sidebar.setHovered(false, 150);
						}
					}}
				>
					<DropdownMenu.Label class="border-border/10 border-b px-4 pt-3 pb-3 font-normal">
						<div class="flex items-center gap-3 text-left text-sm">
							<Avatar.Root class="size-8 shrink-0 rounded-lg">
								{#if $settingsStore.enableGravatar}
									{#await getGravatarUrl(user?.email)}
										<Avatar.Image src={getDefaultProfilePicture()} alt={user.displayName} />
									{:then url}
										<Avatar.Image src={url} alt={user.displayName} />
									{:catch _}
										<Avatar.Image src={getDefaultProfilePicture()} alt={user.displayName} />
									{/await}
								{:else}
									<Avatar.Image src={getDefaultProfilePicture()} alt={user.displayName} />
								{/if}
								<Avatar.Fallback
									class="from-primary/20 to-primary/10 text-primary border-primary/20 rounded-lg border bg-linear-to-br"
								>
									{user.displayName?.charAt(0).toUpperCase()}
								</Avatar.Fallback>
							</Avatar.Root>
							<div class="grid min-w-0 flex-1 text-left text-sm leading-tight">
								<span class="truncate font-medium">{user.displayName}</span>
								<span class="truncate text-xs">{user.email}</span>
							</div>
						</div>
					</DropdownMenu.Label>
					<DropdownMenu.Separator />

					<LocalePicker
						inline={false}
						onOpenChange={(open: boolean) => {
							localePickerOpen = open;
							if (!open && sidebar.state === 'collapsed') {
								sidebar.setHovered(false, 150);
							}
						}}
					/>

					<DropdownMenu.Group class="px-3 pb-2">
						<ArcaneButton
							action="base"
							tone="ghost"
							class={cn(
								'text-muted-foreground flex w-full items-center rounded-xl text-sm font-medium transition-all duration-200 hover:bg-linear-to-br',
								'h-11 justify-start gap-3 px-3 py-2.5'
							)}
							title={m.common_toggle_theme()}
							onclick={toggleMode}
						>
							<div class="group-hover:bg-muted-foreground/10 rounded-lg bg-transparent p-1 transition-colors duration-200">
								{#if mode.current === 'dark'}
									<SunIcon class="size-4 transition-transform duration-200" />
								{:else}
									<MoonIcon class="size-4 transition-transform duration-200" />
								{/if}
							</div>
							<span class="font-medium">{m.common_toggle_theme()}</span>
						</ArcaneButton>
					</DropdownMenu.Group>
				</div>
			</DropdownMenu.Content>
		</DropdownMenu.Root>
	</Sidebar.MenuItem>
</Sidebar.Menu>
