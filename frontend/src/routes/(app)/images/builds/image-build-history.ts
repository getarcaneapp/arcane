import * as m from '#lib/paraglide/messages.js';
import type { ImageBuildRecord, ImageBuildStatus } from '#lib/types/image.js';
import { formatDateTimeShort, sanitizeLogText } from '#lib/utils/formatting.js';

export type BuildOutputEntry = {
	text: string;
	isError: boolean;
};

export function formatKeyValueMap(values?: Record<string, string>): string {
	if (!values || Object.keys(values).length === 0) return '';
	return Object.entries(values)
		.map(([key, value]) => `${key}=${value}`)
		.join('\n');
}

export const formatBuildArgs = formatKeyValueMap;

export function formatStringList(values?: string[]): string {
	return values?.length ? values.join('\n') : '';
}

export function formatBuildTimestamp(value?: string): string {
	if (!value) return '-';
	return formatDateTimeShort(value) || value;
}

export function formatBuildDuration(ms?: number): string {
	if (!ms || ms <= 0) return '-';
	const totalSeconds = Math.round(ms / 1000);
	const minutes = Math.floor(totalSeconds / 60);
	const seconds = totalSeconds % 60;
	return minutes > 0 ? `${minutes}m ${seconds}s` : `${seconds}s`;
}

export function isGitBuildContextSource(value?: string): boolean {
	const trimmed = value?.trim();
	if (!trimmed) return false;
	const base = trimmed.split('#', 1)[0]?.trim() ?? '';
	if (!base) return false;
	if (base.startsWith('git@')) return true;
	try {
		const parsed = new URL(base);
		const protocol = parsed.protocol.toLowerCase();
		if (protocol === 'ssh:' || protocol === 'git:') return true;
		return (protocol === 'http:' || protocol === 'https:') && parsed.pathname !== '/';
	} catch {
		return false;
	}
}

export function buildContextDisplayName(value?: string): string {
	const trimmed = value?.trim();
	if (!trimmed) return '';
	if (!isGitBuildContextSource(trimmed)) return trimmed.split('/').pop() || trimmed;
	const withoutFragment = trimmed.split('#', 1)[0] ?? trimmed;
	const normalized = withoutFragment.endsWith('/') ? withoutFragment.slice(0, -1) : withoutFragment;
	return (normalized.split('/').pop() || normalized).replace(/\.git$/i, '');
}

export function getBuildTitle(build: ImageBuildRecord): string {
	return build.tags?.[0] || buildContextDisplayName(build.contextDir) || build.contextDir;
}

export function getContextPathFromBuild(build: ImageBuildRecord, buildsRoot: string): string {
	if (isGitBuildContextSource(build.contextDir)) return '';
	const root = buildsRoot.endsWith('/') ? buildsRoot.slice(0, -1) : buildsRoot;
	if (!build.contextDir || build.contextDir === root) return '';
	return build.contextDir.startsWith(`${root}/`) ? build.contextDir.slice(root.length + 1) : '';
}

export function parseBuildOutput(output: string): BuildOutputEntry[] {
	if (!output) return [];
	return output
		.split('\n')
		.map((line) => line.trim())
		.filter(Boolean)
		.map((line) => {
			try {
				const data = JSON.parse(line) as Record<string, unknown>;
				const error = data['error'];
				if (error) {
					const message = typeof error === 'string' ? error : ((error as { message?: string }).message ?? line);
					return { text: sanitizeLogText(message), isError: true } satisfies BuildOutputEntry;
				}
				const text = data['log'] ?? data['status'] ?? data['stream'];
				if (typeof text === 'string') return { text: sanitizeLogText(text), isError: false } satisfies BuildOutputEntry;
			} catch {
				// Raw text line.
			}
			return { text: sanitizeLogText(line), isError: false } satisfies BuildOutputEntry;
		})
		.filter((entry) => entry.text.trim() !== '');
}

export function buildHistoryStatusLabel(status?: ImageBuildStatus): string {
	switch (status) {
		case 'running':
			return m.common_running();
		case 'success':
			return m.common_success();
		case 'failed':
			return m.common_failed();
		default:
			return m.common_unknown();
	}
}

export function getBuildStatusBadgeVariant(status?: ImageBuildStatus): 'green' | 'red' | 'blue' | 'gray' {
	switch (status) {
		case 'success':
			return 'green';
		case 'failed':
			return 'red';
		case 'running':
			return 'blue';
		default:
			return 'gray';
	}
}
