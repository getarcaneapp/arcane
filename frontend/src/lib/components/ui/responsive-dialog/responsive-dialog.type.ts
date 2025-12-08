import { type Snippet } from 'svelte';

export interface ResponsiveDialogProps {
	/**
	 * Bindable open state of the dialog/drawer
	 * @default false
	 */
	open?: boolean;

	/**
	 * Callback function when open state changes
	 */
	onOpenChange?: (open: boolean) => void;

	/**
	 * Snippet for the trigger button (optional)
	 * If not provided, dialog must be controlled externally via `open` prop
	 */
	trigger?: Snippet;

	/**
	 * Title displayed in the dialog/drawer header
	 */
	title?: string;

	/**
	 * Description displayed in the dialog/drawer header
	 */
	description?: string;

	/**
	 * Main content snippet (required)
	 */
	children: Snippet;

	/**
	 * Footer content snippet (optional)
	 * Typically used for action buttons
	 */
	footer?: Snippet;

	/**
	 * CSS class for the content wrapper
	 * Desktop: No default padding
	 * Mobile: Defaults to 'px-4'
	 */
	class?: string;

	/**
	 * CSS class for the dialog content container
	 * @default 'sm:max-w-[425px]'
	 */
	contentClass?: string;

	/**
	 * The variant of the dialog on desktop
	 * @default 'dialog'
	 */
	variant?: 'dialog' | 'sheet';
}
