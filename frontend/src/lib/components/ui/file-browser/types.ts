import type { Snippet } from 'svelte';
import type { HTMLAttributes } from 'svelte/elements';

export interface FileBrowserFile {
	name: string;
	path: string;
	type: string;
	size?: number;
}

export type FileBrowserRootProps = HTMLAttributes<HTMLDivElement> & {
	path?: string;
	loading?: boolean;
	error?: string | null;
};

export type FileBrowserBreadcrumbProps = HTMLAttributes<HTMLDivElement> & {
	path: string;
	onNavigate?: (path: string) => void;
};

export type FileBrowserListProps<T extends FileBrowserFile = FileBrowserFile> = HTMLAttributes<HTMLDivElement> & {
	files: T[];
	selectedPath?: string | null;
	onSelect?: (file: T) => void;
	onOpen?: (file: T) => void;
	icon?: Snippet<[{ file: T }]>;
};

export type FileBrowserItemProps<T extends FileBrowserFile = FileBrowserFile> = HTMLAttributes<HTMLButtonElement> & {
	file: T;
	selected?: boolean;
	icon?: Snippet<[{ file: T }]>;
};

export type FileBrowserEmptyProps = HTMLAttributes<HTMLDivElement> & {
	message?: string;
};

export type FileBrowserLoadingProps = HTMLAttributes<HTMLDivElement> & {
	message?: string;
};

export type FileBrowserErrorProps = HTMLAttributes<HTMLDivElement> & {
	message: string;
	onRetry?: () => void;
};

export type FileBrowserPreviewProps<T extends FileBrowserFile = FileBrowserFile> = HTMLAttributes<HTMLDivElement> & {
	file: T | null;
	content?: string;
	loading?: boolean;
	isBinary?: boolean;
	truncated?: boolean;
	error?: string | null;
};
