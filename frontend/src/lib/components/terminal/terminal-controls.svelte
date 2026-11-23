<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import { m } from '#lib/paraglide/messages.js';

	let {
		selectedShell = $bindable(),
		selectedUser = $bindable(),
		onShellChange,
		onUserChange,
		onReconnect
	}: {
		selectedShell: string;
		selectedUser: string;
		onShellChange?: (shell: string) => void;
		onUserChange?: (user: string) => void;
		onReconnect?: () => void;
	} = $props();

	const commonShells = [
		{ value: 'auto', label: m.auto() },
		{ value: '/bin/sh', label: 'sh' },
		{ value: '/bin/bash', label: 'bash' },
		{ value: '/bin/ash', label: 'ash' },
		{ value: '/bin/zsh', label: 'zsh' },
		{ value: 'custom', label: m.custom() }
	];

	const shellLabels: Record<string, string> = {
		auto: m.auto(),
		'/bin/sh': 'sh',
		'/bin/bash': 'bash',
		'/bin/ash': 'ash',
		'/bin/zsh': 'zsh',
		custom: m.custom()
	};

	let customShell = $derived.by(() => {
		if (selectedShell && !(selectedShell in shellLabels)) return selectedShell;
		return '';
	});
	const useCustomShell = $derived(selectedShell === 'custom' || (!!selectedShell && !(selectedShell in shellLabels)));

	function handleShellChange(value: string | undefined) {
		if (!value) return;

		if (value === 'custom') {
			const draft = customShell;
			selectedShell = value;
			customShell = draft;
		} else {
			selectedShell = value;
			onShellChange?.(value);
		}
	}

	function handleCustomShellSubmit() {
		if (customShell.trim()) {
			onShellChange?.(customShell);
		}
	}

	const userOptions = [
		{ value: 'default', label: m.container_default_user() },
		{ value: 'root', label: 'root' },
		{ value: 'custom', label: m.custom() }
	];

	let customUserOpen = $state(false);
	let customUser = $derived(selectedUser === 'root' ? '' : selectedUser);
	const userSelectValue = $derived.by(() => {
		if (customUserOpen || (selectedUser !== '' && selectedUser !== 'root')) return 'custom';
		if (selectedUser === 'root') return 'root';
		return 'default';
	});

	function handleUserChange(value: string | undefined) {
		if (!value) return;

		customUserOpen = value === 'custom';
		if (customUserOpen) return;
		selectedUser = value === 'root' ? 'root' : '';
		onUserChange?.(selectedUser);
	}

	function handleCustomUserSubmit() {
		const user = customUser.trim();
		if (!user) return;
		customUserOpen = false;
		selectedUser = user;
		onUserChange?.(user);
	}
</script>

<div class="flex flex-wrap items-center gap-2">
	<Select.Root value={selectedShell} type="single" onValueChange={handleShellChange}>
		<Select.Trigger class="h-8 w-35">
			{shellLabels[selectedShell] ?? m.select_shell_placeholder()}
		</Select.Trigger>
		<Select.Content>
			{#each commonShells as shell (shell.value)}
				<Select.Item value={shell.value}>
					{shell.label}
				</Select.Item>
			{/each}
		</Select.Content>
	</Select.Root>

	{#if useCustomShell}
		<Input
			type="text"
			bind:value={customShell}
			placeholder={m.shell_custom_placeholder()}
			class="h-8 w-45"
			onkeydown={(e) => {
				if (e.key === 'Enter') {
					handleCustomShellSubmit();
				}
			}}
		/>
		<ArcaneButton action="base" tone="outline" onclick={handleCustomShellSubmit} class="h-8" customLabel={m.apply()} />
	{/if}

	<Select.Root value={userSelectValue} type="single" onValueChange={handleUserChange}>
		<Select.Trigger class="h-8 w-40" aria-label={m.common_user()}>
			{userOptions.find((option) => option.value === userSelectValue)?.label}
		</Select.Trigger>
		<Select.Content>
			{#each userOptions as option (option.value)}
				<Select.Item value={option.value}>
					{option.label}
				</Select.Item>
			{/each}
		</Select.Content>
	</Select.Root>

	{#if userSelectValue === 'custom'}
		<Input
			type="text"
			bind:value={customUser}
			placeholder={m.container_user_placeholder()}
			aria-label={m.common_user()}
			class="h-8 w-45"
			onkeydown={(e) => {
				if (e.key === 'Enter') {
					handleCustomUserSubmit();
				}
			}}
		/>
		<ArcaneButton action="base" tone="outline" onclick={handleCustomUserSubmit} class="h-8" customLabel={m.apply()} />
	{/if}

	<ArcaneButton
		action="refresh"
		size="icon"
		tone="ghost"
		onclick={onReconnect}
		class="size-8"
		title={m.terminal_reconnect()}
		showLabel={false}
	/>
</div>
