<script lang="ts">
	import { m } from '$lib/paraglide/messages';
	import * as Avatar from '$lib/components/ui/avatar/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import * as Sidebar from '$lib/components/ui/sidebar/index.js';
	import { useSidebar } from '$lib/components/ui/sidebar/index.js';
	import type { User } from '$lib/types/auth';
	import settingsStore from '$lib/stores/config-store';
	import { goto } from '$app/navigation';
	import { LogoutIcon, UserIcon } from '$lib/icons';

	let {
		user,
		isCollapsed,
		autoLoginEnabled = false
	}: { user: User; isCollapsed: boolean; autoLoginEnabled?: boolean } = $props();
	const sidebar = useSidebar();

	let dropdownOpen = $state(false);

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

		return `https://www.gravatar.com/avatar/${hash}?s=${size}&d=404`;
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
							{#key user?.updatedAt}
								<Avatar.Root class="size-8 rounded-lg">
									{#if user?.avatarUrl}
										<Avatar.Image src={`${user.avatarUrl}?t=${user.updatedAt}`} alt={user.displayName} />
									{:else if $settingsStore.enableGravatar}
										{#await getGravatarUrl(user?.email)}
											<!-- Loading gravatar, show fallback -->
										{:then url}
											<Avatar.Image src={url} alt={user.displayName} />
										{:catch}
											<!-- Gravatar failed, show fallback -->
										{/await}
									{/if}
									<Avatar.Fallback class="bg-primary text-primary-foreground rounded-lg text-sm font-semibold">
										{user.displayName?.charAt(0).toUpperCase()}
									</Avatar.Fallback>
								</Avatar.Root>
							{/key}
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
				class="border-border/30 min-w-60 rounded-xl border p-1.5 shadow-lg backdrop-blur-2xl backdrop-saturate-150"
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
						sidebar.setHovered(false, 150);
					}}
				>
					<div class="flex items-center gap-2.5 px-2 py-2">
						{#key user?.updatedAt}
							<Avatar.Root class="size-8 shrink-0 rounded-lg">
								{#if user?.avatarUrl}
									<Avatar.Image src={`${user.avatarUrl}?t=${user.updatedAt}`} alt={user.displayName} />
								{:else if $settingsStore.enableGravatar}
									{#await getGravatarUrl(user?.email)}
										<!-- Loading gravatar, show fallback -->
									{:then url}
										<Avatar.Image src={url} alt={user.displayName} />
									{:catch}
										<!-- Gravatar failed, show fallback -->
									{/await}
								{/if}
								<Avatar.Fallback class="bg-primary text-primary-foreground rounded-lg text-xs font-semibold">
									{user.displayName?.charAt(0).toUpperCase()}
								</Avatar.Fallback>
							</Avatar.Root>
						{/key}
						<div class="grid min-w-0 flex-1 leading-tight">
							<span class="truncate text-sm font-medium">{user.displayName}</span>
							<span class="text-muted-foreground truncate text-xs">{user.email}</span>
						</div>
					</div>

					<DropdownMenu.Separator class="my-1" />

					<button
						type="button"
						class="hover:bg-muted/60 text-foreground flex w-full items-center gap-2.5 rounded-lg px-2 py-2 text-sm transition-colors"
						onclick={() => {
							dropdownOpen = false;
							goto('/account');
						}}
					>
						<UserIcon class="text-muted-foreground size-4 shrink-0" />
						<span>{m.common_account()}</span>
					</button>

					{#if !autoLoginEnabled}
						<form action="/logout" method="POST" class="w-full">
							<button
								type="submit"
								class="hover:bg-destructive/10 text-destructive flex w-full items-center gap-2.5 rounded-lg px-2 py-2 text-sm transition-colors"
							>
								<LogoutIcon class="size-4 shrink-0" />
								<span>{m.common_log_out()}</span>
							</button>
						</form>
					{/if}
				</div>
			</DropdownMenu.Content>
		</DropdownMenu.Root>
	</Sidebar.MenuItem>
</Sidebar.Menu>
