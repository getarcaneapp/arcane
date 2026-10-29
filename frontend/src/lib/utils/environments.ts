import { error } from '@sveltejs/kit';
import { toast } from 'svelte-sonner';

import { m } from '#lib/paraglide/messages.js';
import type { APIRequestConfig } from '#lib/services/api-service.js';
import settingsStore from '#lib/stores/config-store.svelte.js';
import { environmentStore, LOCAL_DOCKER_ENVIRONMENT_ID, type EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
import { extractApiErrorMessage } from '#lib/utils/api.js';
import { hasPermission } from '#lib/utils/auth.js';
import { withBase } from '#lib/utils/base-path.js';
import { tryCatch } from '#lib/utils/try-catch.js';

/** A row tagged by environment; `environmentTagged` tells it apart from DTOs that carry an environment. */
export type EnvironmentRow<T> = T & { environmentId: string; environmentName: string; environmentTagged: true };
export type EnvironmentFailure = { environment: EnvironmentRef; message: string };

type Groups<T> = { groups?: { groupName: string; items: T[] }[] };
type Page<T> = Paginated<T, unknown> & Groups<T>;
export type EnvironmentPage<P extends Page<object>> = Omit<P, 'data' | 'groups'> & {
	data: EnvironmentRow<P['data'][number]>[];
	groups?: { groupName: string; items: EnvironmentRow<P['data'][number]>[] }[];
	/** Environments whose request failed; the list renders without them. */
	failedEnvironments?: EnvironmentFailure[];
	excludedEnvironments?: EnvironmentRef[];
};

// Unit separator: resource IDs never contain it, so table keys stay unambiguous.
const KEY_SEPARATOR = '\u001f';

/** Table row key; tagged rows are keyed by environment, since IDs can repeat across environments. */
export function rowKey(row: { id: string; environmentId?: string; environmentTagged?: boolean }): string {
	return row.environmentTagged && row.environmentId !== undefined ? `${row.environmentId}${KEY_SEPARATOR}${row.id}` : row.id;
}

function tagEnvironment<T extends object>(row: T, environment: EnvironmentRef): EnvironmentRow<T> {
	return { ...row, environmentId: environment.id, environmentName: environment.name, environmentTagged: true };
}

export function parseRowKey(key: string): { environmentId?: string; id: string } {
	const index = key.indexOf(KEY_SEPARATOR);
	if (index < 0) return { id: key };
	return { environmentId: key.slice(0, index), id: key.slice(index + 1) };
}

export function parseEnvironmentRowKey(key: string): { environmentId: string; id: string } {
	const { environmentId, id } = parseRowKey(key);
	if (environmentId === undefined) throw new Error(`Row key "${key}" is not tagged with an environment`);
	return { environmentId, id };
}

export function environmentName(environmentId: string): string {
	return environmentStore.available.find((env) => env.id === environmentId)?.name ?? environmentId;
}

/** Filter key of the environment facet; fetchAcrossEnvironments consumes it, so the backend never sees it. */
export const ENVIRONMENT_FILTER = 'environment';

export function environmentFilterIds(value: unknown, environments: EnvironmentRef[] = environmentStore.shown): string[] {
	if (!Array.isArray(value)) return [];
	return value.map(String).filter((id) => environments.some((env) => env.id === id));
}

/**
 * Shown environments a page-wide action runs in: those granting every permission. When none do, all
 * shown ones are returned so the server still reports the denial, as with one environment.
 */
export function actionEnvironments(...permissions: string[]): EnvironmentRef[] {
	const permitted = environmentStore.shown.filter((env) => permissions.every((permission) => hasPermission(permission, env.id)));
	return permitted.length > 0 ? permitted : environmentStore.shown;
}

export function readableEnvironments(permission: string): EnvironmentRef[] {
	return environmentStore.shown.filter((env) => hasPermission(permission, env.id));
}

/** Whether a permission is held in any shown environment; row actions check the row's own environment. */
export function hasShownPermission(permission: string): boolean {
	return environmentStore.shown.some((env) => hasPermission(permission, env.id));
}

export function environmentHref(path: string, environmentId: string): string {
	return withBase(`${path}${path.includes('?') ? '&' : '?'}env=${encodeURIComponent(environmentId)}`);
}

export async function resolveDetailEnvironmentId(url: URL): Promise<string> {
	await environmentStore.ready;
	const requested = url.searchParams.get('env');
	if (!requested || !environmentStore.available.some((env) => env.id === requested))
		error(404, m.common_not_found_title({ resource: m.resource_environment_cap() }));
	return requested;
}

export function destinationEnvironmentId(
	picked: string,
	permission: string,
	environments: EnvironmentRef[] = environmentStore.shown
): string {
	const permitted = environments.filter((env) => hasPermission(permission, env.id)).map((env) => env.id);
	if (permitted.includes(picked)) return picked;
	if (permitted.includes(LOCAL_DOCKER_ENVIRONMENT_ID)) return LOCAL_DOCKER_ENVIRONMENT_ID;
	return permitted[0] ?? (picked || LOCAL_DOCKER_ENVIRONMENT_ID);
}

export function listScopes(environmentIds: string[]): string[] {
	return [...new Set([...environmentIds, environmentIds.join(','), environmentStore.shownKey])];
}

/** Appended to an existing confirmation when an action runs across several environments. */
export function acrossEnvironmentsNote(environments: EnvironmentRef[]): string {
	if (environments.length < 2) return '';
	return m.all_environments_confirm_note({ count: environments.length });
}

export async function collectAcrossEnvironments<T>(
	environments: EnvironmentRef[],
	run: (environmentId: string, config: APIRequestConfig) => Promise<T>,
	reportInline = true
): Promise<{ values: { environment: EnvironmentRef; value: T }[]; failures: EnvironmentFailure[] }> {
	const config: APIRequestConfig = { suppressAccessDeniedToast: reportInline };
	const outcomes = await Promise.all(
		environments.map(async (environment) => ({ environment, result: await tryCatch(run(environment.id, config)) }))
	);
	const failed = outcomes.filter((outcome) => outcome.result.error !== null);
	if (failed.length === 1 && outcomes.length === 1 && !reportInline) throw failed[0]!.result.error;
	const failures = failed.map(({ environment, result }) => ({ environment, message: extractApiErrorMessage(result.error) }));
	if (failed.length > 0 && failed.length === outcomes.length) {
		throw new Error(failures.map(({ environment, message }) => `${environment.name}: ${message}`).join('\n'));
	}
	return {
		values: outcomes.flatMap(({ environment, result }) => (result.error === null ? [{ environment, value: result.data }] : [])),
		failures
	};
}

/**
 * Runs a user-initiated action once per environment, like the CLI. A failing environment is
 * toasted and the others still return; it throws only when every environment failed.
 */
export async function settleAcrossEnvironments<T>(
	environments: EnvironmentRef[],
	run: (environmentId: string) => Promise<T>
): Promise<{ environment: EnvironmentRef; value: T }[]> {
	const { values, failures } = await collectAcrossEnvironments(environments, (id) => run(id), environments.length > 1);
	for (const { environment, message } of failures) {
		toast.error(m.all_environments_request_failed({ environment: environment.name }), {
			id: `environment-request-failed-${environment.id}`,
			description: message
		});
	}
	return values;
}

export async function readAcrossEnvironments<T>(
	environments: EnvironmentRef[],
	run: (scope: string, config: APIRequestConfig) => Promise<T>,
	reportInline = true
): Promise<{ value: T; failures: EnvironmentFailure[]; excluded: EnvironmentRef[] }> {
	if (environments.length === 0) throw new Error('readAcrossEnvironments needs at least one environment');
	const limit = Number(settingsStore.current?.allEnvironmentsLimit) || 64;
	const included = environments.slice(0, limit);
	let reported: EnvironmentFailure[] = [];
	const result = await tryCatch(
		run(included.map((env) => env.id).join(','), {
			suppressAccessDeniedToast: reportInline,
			onFailedEnvironments: (failures) => {
				reported = failures;
			}
		})
	);
	if (result.error !== null) {
		if (!reportInline || included.length > 1) throw result.error;
		throw new Error(`${included[0]!.name}: ${extractApiErrorMessage(result.error)}`);
	}
	const failures = reported.map(({ environment, message }) => ({
		environment: included.find((env) => env.id === environment.id) ?? environment,
		message
	}));
	if (failures.length > 0 && failures.length === included.length) {
		throw new Error(failures.map(({ environment, message }) => `${environment.name}: ${message}`).join('\n'));
	}
	return { value: result.data, failures, excluded: environments.slice(limit) };
}

export function environmentRows<T extends object>(rows: T[], environments: EnvironmentRef[]): EnvironmentRow<T>[] {
	if (environments.length === 1) return rows.map((row) => tagEnvironment(row, environments[0]!));
	return rows.map((row) => ({ ...(row as EnvironmentRow<T>), environmentTagged: true }));
}

export type EnvironmentSum<C> = C & {
	failedEnvironments: EnvironmentFailure[];
	excludedEnvironments: EnvironmentRef[];
	byEnvironment: { environment: EnvironmentRef; data: C }[];
};

export async function sumAcrossEnvironments<C extends object>(
	environments: EnvironmentRef[],
	fetch: (scope: string, config: APIRequestConfig) => Promise<C>,
	empty: C
): Promise<EnvironmentSum<C>> {
	if (environments.length === 0) return { ...empty, failedEnvironments: [], excludedEnvironments: [], byEnvironment: [] };
	let breakdown: { environment: EnvironmentRef; data: C }[] | undefined;
	const { value, failures, excluded } = await readAcrossEnvironments(environments, (scope, config) =>
		fetch(scope, {
			...config,
			onEnvironments: (entries) => {
				breakdown = entries.map(({ environment, data }) => ({
					environment: environments.find((env) => env.id === environment.id) ?? environment,
					data: data as C
				}));
			}
		})
	);
	const single = environments.length === 1 && failures.length === 0 ? [{ environment: environments[0]!, data: value }] : [];
	return { ...value, failedEnvironments: failures, excludedEnvironments: excluded, byEnvironment: breakdown ?? single };
}

/** Adds failures from related reads, such as counts or tags, to a list's inline alert, once per environment. */
export function withFailedEnvironments<P extends { failedEnvironments?: EnvironmentFailure[] }>(
	page: P,
	...related: (EnvironmentFailure[] | undefined)[]
): P {
	const failures = [page.failedEnvironments, ...related].flatMap((list) => list ?? []);
	if (failures.length === 0) return page;
	return {
		...page,
		failedEnvironments: failures.filter(
			(failure, index) => failures.findIndex((other) => other.environment.id === failure.environment.id) === index
		)
	};
}

export async function fetchAcrossEnvironments<
	P extends Page<T>,
	O extends SearchPaginationSortRequest,
	T extends object = P['data'][number]
>(
	environments: EnvironmentRef[],
	options: O,
	fetch: (scope: string, options: O, config: APIRequestConfig) => Promise<P>
): Promise<EnvironmentPage<P>> {
	const { [ENVIRONMENT_FILTER]: environmentFilter, ...filters } = options.filters ?? {};
	// Picks are checked against every shown environment, so picking only ones this page skips shows nothing.
	const picked = environmentFilterIds(environmentFilter);
	const targets = picked.length > 0 ? environments.filter((env) => picked.includes(env.id)) : environments;
	if (targets.length === 0) {
		const pagination = { totalPages: 0, totalItems: 0, currentPage: 1, itemsPerPage: options.pagination?.limit ?? 0 };
		return { data: [], pagination, failedEnvironments: [], excludedEnvironments: [] } as unknown as EnvironmentPage<P>;
	}
	const sortsByEnvironment = options.sort?.column === ENVIRONMENT_FILTER && targets.length === 1;
	const request: O = { ...options, filters: options.filters && filters, sort: sortsByEnvironment ? undefined : options.sort };
	const {
		value: page,
		failures,
		excluded
	} = await readAcrossEnvironments(targets, (scope, config) => fetch(scope, request, config));
	return {
		...page,
		data: environmentRows(page.data, targets),
		groups: page.groups?.map((group) => ({ ...group, items: environmentRows(group.items, targets) })),
		failedEnvironments: failures,
		excludedEnvironments: excluded
	};
}
