import type { Snippet } from 'svelte';
import type { HTMLAttributes } from 'svelte/elements';
import type { FileEntry, FileEntryType } from '$lib/types/container.type';

export type FileBrowserRootProps = HTMLAttributes<HTMLDivElement> & {
	path?: string;
	loading?: boolean;
	error?: string | null;
};

export type FileBrowserBreadcrumbProps = HTMLAttributes<HTMLDivElement> & {
	path: string;
	onNavigate?: (path: string) => void;
};

export type FileBrowserListProps = HTMLAttributes<HTMLDivElement> & {
	files: FileEntry[];
	selectedPath?: string | null;
	onSelect?: (file: FileEntry) => void;
	onOpen?: (file: FileEntry) => void;
	icon?: Snippet<[{ file: FileEntry }]>;
};

export type FileBrowserItemProps = HTMLAttributes<HTMLButtonElement> & {
	file: FileEntry;
	selected?: boolean;
	icon?: Snippet<[{ file: FileEntry }]>;
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

export type FileBrowserPreviewProps = HTMLAttributes<HTMLDivElement> & {
	file: FileEntry | null;
	content?: string;
	loading?: boolean;
	isBinary?: boolean;
	truncated?: boolean;
	error?: string | null;
};
