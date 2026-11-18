<script lang="ts" module>
	import { toast } from 'svelte-sonner';

	import ToastCopyButton from './toast-copy-button.svelte';

	const showError = toast.error;
	const showWarning = toast.warning;

	toast.error = (message, data) => {
		return showError(message, { cancel: ToastCopyButton, ...data });
	};
	toast.warning = (message, data) => {
		return showWarning(message, { cancel: ToastCopyButton, ...data });
	};
</script>

<script lang="ts">
	import { mode } from 'mode-watcher';
	import { Toaster as Sonner, type ToasterProps as SonnerProps } from 'svelte-sonner';

	let { ...restProps }: SonnerProps = $props();
</script>

<Sonner
	theme={mode.current}
	class="group z-(--arcane-z-toast)"
	toastOptions={{
		classes: {
			toast:
				'group toast group-data-[sonner-toaster]:bg-background group-data-[sonner-toaster]:text-foreground group-data-[sonner-toaster]:border-border group-data-[sonner-toaster]:rounded-xl group-data-[sonner-toaster]:shadow-lg',
			title: 'group-[.toast]:font-semibold',
			description: 'group-[.toast]:text-muted-foreground',
			actionButton: 'group-[.toast]:bg-primary group-[.toast]:text-primary-foreground',
			cancelButton: 'group-[.toast]:bg-muted group-[.toast]:text-muted-foreground',
			// Flat per-type accent: a colored left border, no glass/heavy bg.
			success: 'group-data-[sonner-toaster]:border-l-4 group-data-[sonner-toaster]:border-l-success',
			error: 'group-data-[sonner-toaster]:border-l-4 group-data-[sonner-toaster]:border-l-destructive',
			warning: 'group-data-[sonner-toaster]:border-l-4 group-data-[sonner-toaster]:border-l-warning',
			info: 'group-data-[sonner-toaster]:border-l-4 group-data-[sonner-toaster]:border-l-info'
		}
	}}
	{...restProps}
/>
