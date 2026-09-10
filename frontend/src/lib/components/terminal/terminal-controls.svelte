<script lang="ts">
	import * as Select from '#lib/components/ui/select/index.js';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { m } from '#lib/paraglide/messages.js';

	let {
		selectedShell = $bindable(),
		onShellChange,
		onReconnect
	}: {
		selectedShell: string;
		onShellChange?: (shell: string) => void;
		onReconnect?: () => void;
	} = $props();

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
</script>

<div class="flex items-center gap-2">
	<Select.Root value={selectedShell} type="single" onValueChange={handleShellChange}>
		<Select.Trigger class="h-8 w-[140px]">
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
		title={m.terminal_reconnect()}
		showLabel={false}
	/>
</div>
