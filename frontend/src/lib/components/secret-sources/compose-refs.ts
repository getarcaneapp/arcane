import type { ProjectSecretBindingDto } from '#lib/types/secret-source.js';

// A selection is a set of "service\u0000key" pairs: add KEY to that service.
const separator = '\u0000';

export function pairKey(service: string, key: string): string {
	return `${service}${separator}${key}`;
}

// Stands in for the project's .env in a selection; no service can be named this.
export const ENV_TARGET = '\u0001env';

export function selectionToAssignments(selection: Iterable<string>): Record<string, string[]> {
	const assignments: Record<string, string[]> = {};
	for (const pair of selection) {
		const [service = '', key = ''] = pair.split(separator);
		if (!service || !key || service === ENV_TARGET) continue;
		(assignments[service] ??= []).push(key);
	}
	return assignments;
}

export function selectionToEnvKeys(selection: Iterable<string>): string[] {
	const keys: string[] = [];
	for (const pair of selection) {
		const [service = '', key = ''] = pair.split(separator);
		if (service === ENV_TARGET && key) keys.push(key);
	}
	return keys;
}

// Keys that .env content defines, for showing which ones it already has. The
// server makes the final call when it adds references.
export function declaredEnvKeys(env: string): Set<string> {
	const keys = new Set<string>();
	for (const line of env.split('\n')) {
		const match = /^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*[=:]/.exec(line);
		if (match?.[1]) keys.add(match[1]);
	}
	return keys;
}

// Where Add to compose writes on an existing project: into the open editor,
// so the user reviews the change and saves it with the rest.
export type SecretRefsEditor = {
	compose: string;
	env: string;
	canEditCompose: boolean;
	canEditEnv: boolean;
	// Compose or .env has unsaved edits.
	dirty: boolean;
	apply: (changes: { compose?: string; env?: string }) => void;
	// Reloads the drafts after the server changed the files (guided setup).
	reload: () => Promise<void>;
};

// Secrets chosen while creating a project, bound once the project exists.
export type NewProjectSecrets = {
	binding: ProjectSecretBindingDto;
	sourceName: string;
	keyCount: number;
};
