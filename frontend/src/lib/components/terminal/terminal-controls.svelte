<script lang="ts">
	import * as Select from '$lib/components/ui/select/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { m } from '$lib/paraglide/messages';
	import { RefreshIcon } from '$lib/icons';

	let {
		selectedShell = $bindable(),
		onShellChange,
		onReconnect
	}: {
		selectedShell: string;
		onShellChange?: (shell: string) => void;
		onReconnect?: () => void;
	} = $props();

	let customShell = $state('');
	let useCustomShell = $state(false);

	const commonShells = [
		{ value: '/bin/sh', label: 'sh' },
		{ value: '/bin/bash', label: 'bash' },
		{ value: '/bin/ash', label: 'ash' },
		{ value: '/bin/zsh', label: 'zsh' },
		{ value: 'custom', label: m.custom() }
	];

	const shellLabels: Record<string, string> = {
		'/bin/sh': 'sh',
		'/bin/bash': 'bash',
		'/bin/ash': 'ash',
		'/bin/zsh': 'zsh',
		custom: m.custom()
	};

	function handleShellChange(value: string | undefined) {
		if (!value) return;

		if (value === 'custom') {
			useCustomShell = true;
			selectedShell = value;
		} else {
			useCustomShell = false;
			selectedShell = value;
			onShellChange?.(value);
		}
	}

	function handleCustomShellSubmit() {
		if (customShell.trim()) {
			onShellChange?.(customShell);
		}
	}

	$effect(() => {
		if (!selectedShell) {
			return;
		}

		const isKnownShell = selectedShell in shellLabels;
		if (!isKnownShell && selectedShell !== 'custom') {
			useCustomShell = true;
			customShell = selectedShell;
			return;
		}

		if (selectedShell === 'custom') {
			useCustomShell = true;
			return;
		}

		if (isKnownShell) {
			useCustomShell = false;
			customShell = '';
		}
	});
</script>

<div class="flex items-center gap-2">
	<Select.Root bind:value={selectedShell} type="single" onValueChange={handleShellChange}>
		<Select.Trigger class="h-8 w-[140px]">
			{shellLabels[selectedShell] ?? m.shell_select_placeholder()}
		</Select.Trigger>
		<Select.Content>
			{#each commonShells as shell}
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
			class="h-8 w-[180px]"
			onkeydown={(e) => {
				if (e.key === 'Enter') {
					handleCustomShellSubmit();
				}
			}}
		/>
		<ArcaneButton action="base" size="sm" tone="outline" onclick={handleCustomShellSubmit} class="h-8" customLabel={m.apply()} />
	{/if}

	<ArcaneButton
		action="refresh"
		size="icon"
		tone="ghost"
		onclick={onReconnect}
		class="size-8"
		title="Reconnect shell"
		showLabel={false}
	/>
</div>
