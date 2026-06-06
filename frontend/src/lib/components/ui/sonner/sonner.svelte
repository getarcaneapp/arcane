<script lang="ts">
	import { Toaster as Sonner, type ToasterProps as SonnerProps } from 'svelte-sonner';
	import { mode } from 'mode-watcher';

	let { ...restProps }: SonnerProps = $props();
</script>

<Sonner
	theme={mode.current}
	class="toaster group"
	toastOptions={{
		classes: {
			toast:
				'group toast group-[.toaster]:bg-background group-[.toaster]:text-foreground group-[.toaster]:border-border group-[.toaster]:rounded-xl group-[.toaster]:shadow-lg',
			title: 'group-[.toast]:font-semibold',
			description: 'group-[.toast]:text-muted-foreground',
			actionButton: 'group-[.toast]:bg-primary group-[.toast]:text-primary-foreground',
			cancelButton: 'group-[.toast]:bg-muted group-[.toast]:text-muted-foreground',
			// Flat per-type accent: a colored left border, no glass/heavy bg.
			success: 'group-[.toaster]:border-l-4 group-[.toaster]:border-l-emerald-500',
			error: 'group-[.toaster]:border-l-4 group-[.toaster]:border-l-red-500',
			warning: 'group-[.toaster]:border-l-4 group-[.toaster]:border-l-amber-500',
			info: 'group-[.toaster]:border-l-4 group-[.toaster]:border-l-sky-500'
		}
	}}
	{...restProps}
/>
