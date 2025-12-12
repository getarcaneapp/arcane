<script lang="ts">
	import { MediaQuery } from 'svelte/reactivity';
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import * as Drawer from '$lib/components/ui/drawer/index.js';
	import * as Sheet from '$lib/components/ui/sheet/index.js';
	import type { ResponsiveDialogProps } from './responsive-dialog.type.js';
	import { cn } from '$lib/utils.js';

	let {
		open = $bindable(false),
		onOpenChange,
		trigger,
		title,
		description,
		children,
		footer,
		class: className,
		contentClass,
		variant = 'dialog'
	}: ResponsiveDialogProps = $props();

	const isDesktop = new MediaQuery('(min-width: 768px)');

	function handleOpenChange(newOpen: boolean) {
		open = newOpen;
		onOpenChange?.(newOpen);
	}
</script>

{#if isDesktop.current}
	{#if variant === 'sheet'}
		<Sheet.Root bind:open onOpenChange={handleOpenChange}>
			{#if trigger}
				<Sheet.Trigger>
					{@render trigger()}
				</Sheet.Trigger>
			{/if}
			<Sheet.Content class={cn('flex flex-col overflow-hidden p-0', contentClass)}>
				{#if title || description}
					<Sheet.Header class="shrink-0 px-6 pt-6">
						{#if title}
							<Sheet.Title>{title}</Sheet.Title>
						{/if}
						{#if description}
							<Sheet.Description>{description}</Sheet.Description>
						{/if}
					</Sheet.Header>
				{/if}
				<div class={cn('min-h-0 flex-1 overflow-y-auto px-6', className)}>
					{@render children()}
				</div>
				{#if footer}
					<Sheet.Footer class="shrink-0 px-6 pb-6">
						{@render footer()}
					</Sheet.Footer>
				{/if}
			</Sheet.Content>
		</Sheet.Root>
	{:else}
		<Dialog.Root bind:open onOpenChange={handleOpenChange}>
			{#if trigger}
				<Dialog.Trigger>
					{@render trigger()}
				</Dialog.Trigger>
			{/if}
			<Dialog.Content
				class={cn(
					'max-h-[calc(100vh-2rem)] grid-rows-[auto_minmax(0,1fr)_auto]! overflow-hidden p-0!',
					contentClass ?? 'sm:max-w-[425px]'
				)}
			>
				{#if title || description}
					<Dialog.Header class="shrink-0 px-6 pt-6">
						{#if title}
							<Dialog.Title>{title}</Dialog.Title>
						{/if}
						{#if description}
							<Dialog.Description>{description}</Dialog.Description>
						{/if}
					</Dialog.Header>
				{/if}
				<div class={cn('min-h-0 overflow-y-auto px-6', className)}>
					{@render children()}
				</div>
				{#if footer}
					<Dialog.Footer class="shrink-0 px-6 pb-6">
						{@render footer()}
					</Dialog.Footer>
				{/if}
			</Dialog.Content>
		</Dialog.Root>
	{/if}
{:else}
	<Drawer.Root bind:open onOpenChange={handleOpenChange}>
		{#if trigger}
			<Drawer.Trigger>
				{@render trigger()}
			</Drawer.Trigger>
		{/if}
		<Drawer.Content class="flex max-h-[85vh] flex-col overflow-hidden">
			{#if title || description}
				<Drawer.Header class="shrink-0 text-left">
					{#if title}
						<Drawer.Title>{title}</Drawer.Title>
					{/if}
					{#if description}
						<Drawer.Description>{description}</Drawer.Description>
					{/if}
				</Drawer.Header>
			{/if}
			<div class={cn('min-h-0 flex-1 overflow-y-auto', className ?? 'px-4 pb-4')}>
				{@render children()}
			</div>
			{#if footer}
				<Drawer.Footer class="shrink-0 pt-2">
					{@render footer()}
				</Drawer.Footer>
			{/if}
		</Drawer.Content>
	</Drawer.Root>
{/if}

<style>
	:global(html:has([data-slot='dialog-overlay'][data-state='open'])),
	:global(body:has([data-slot='dialog-overlay'][data-state='open'])) {
		overflow: hidden !important;
	}

	:global(html:has([data-vaul-drawer])),
	:global(body:has([data-vaul-drawer])) {
		overflow: hidden !important;
	}
</style>
