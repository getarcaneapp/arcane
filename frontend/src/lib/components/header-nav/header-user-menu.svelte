<script lang="ts">
	import * as Avatar from '$lib/components/ui/avatar/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import type { User } from '$lib/types/auth';
	import settingsStore from '$lib/stores/config-store';
	import UserMenuContent from '$lib/components/user-menu-content.svelte';

	let {
		user,
		autoLoginEnabled = false,
		versionLabel,
		onShowVersion
	}: {
		user: User;
		autoLoginEnabled?: boolean;
		versionLabel?: string;
		onShowVersion?: () => void;
	} = $props();

	let dropdownOpen = $state(false);

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

<DropdownMenu.Root bind:open={dropdownOpen}>
	<DropdownMenu.Trigger
		class="hover:ring-primary/40 data-[state=open]:ring-primary/40 flex shrink-0 items-center rounded-lg transition-shadow hover:ring-2 data-[state=open]:ring-2"
		aria-label={user.displayName ?? user.username}
	>
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
					{(user.displayName ?? user.username)?.charAt(0).toUpperCase()}
				</Avatar.Fallback>
			</Avatar.Root>
		{/key}
	</DropdownMenu.Trigger>
	<DropdownMenu.Content
		class="border-border/30 min-w-60 rounded-xl border p-1.5 shadow-lg backdrop-blur-2xl backdrop-saturate-150"
		side="bottom"
		align="end"
		sideOffset={8}
	>
		<UserMenuContent {user} {autoLoginEnabled} {versionLabel} {onShowVersion} onNavigate={() => (dropdownOpen = false)} />
	</DropdownMenu.Content>
</DropdownMenu.Root>
