import { error } from '@sveltejs/kit';
import { toast } from 'svelte-sonner';

import { TABLE_PAGE_SIZE_ALL } from '#lib/constants/table-pagination.js';
import { m } from '#lib/paraglide/messages.js';
import type { APIRequestConfig } from '#lib/services/api-service.js';
import {
	allEnvironmentsPreview,
	environmentStore,
	LOCAL_DOCKER_ENVIRONMENT_ID,
	type EnvironmentRef
} from '#lib/stores/environment.store.svelte.js';
import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
import { extractApiErrorMessage } from '#lib/utils/api.js';
import { hasPermission } from '#lib/utils/auth.js';
import { tryCatch } from '#lib/utils/try-catch.js';

/** A row tagged by fetchAcrossEnvironments; `environmentTagged` tells it apart from DTOs that carry an environment. */
export type EnvironmentRow<T> = T & { environmentId: string; environmentName: string; environmentTagged: true };
export type EnvironmentFailure = { environment: EnvironmentRef; message: string };

type Groups<T> = { groups?: { groupName: string; items: T[] }[] };
type Page<T> = Paginated<T, unknown> & Groups<T>;
export type EnvironmentPage<P extends Page<object>> = Omit<P, 'data' | 'groups'> & {
	data: EnvironmentRow<P['data'][number]>[];
	groups?: { groupName: string; items: EnvironmentRow<P['data'][number]>[] }[];
	/** Environments whose request failed; the list renders without them. */
	failedEnvironments?: EnvironmentFailure[];
};

// Unit separator: resource IDs never contain it, so table keys stay unambiguous.
const KEY_SEPARATOR = '\u001f';

/** Table row key; tagged rows are keyed by environment, since IDs can repeat across environments. */
export function rowKey(row: { id: string; environmentId?: string; environmentTagged?: boolean }): string {
	return row.environmentTagged && row.environmentId !== undefined ? `${row.environmentId}${KEY_SEPARATOR}${row.id}` : row.id;
}

export function tagEnvironment<T extends object>(row: T, environment: EnvironmentRef): EnvironmentRow<T> {
	return { ...row, environmentId: environment.id, environmentName: environment.name, environmentTagged: true };
}

export function parseRowKey(key: string): { environmentId?: string; id: string } {
	const index = key.indexOf(KEY_SEPARATOR);
	if (index < 0) return { id: key };
	return { environmentId: key.slice(0, index), id: key.slice(index + 1) };
}

export function environmentName(environmentId: string): string {
	return environmentStore.available.find((env) => env.id === environmentId)?.name ?? environmentId;
}

/** Filter key of the environment facet; fetchAcrossEnvironments consumes it, so the backend never sees it. */
export const ENVIRONMENT_FILTER = 'environment';

/** Environment filter IDs among `environments`; stale IDs are dropped, and it is empty with one environment. */
export function environmentFilterIds(value: unknown, environments: EnvironmentRef[] = environmentStore.shown): string[] {
	if (environments.length < 2 || !Array.isArray(value)) return [];
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

/** Whether a permission is held in any shown environment; row actions check the row's own environment. */
export function hasShownPermission(permission: string): boolean {
	return environmentStore.shown.some((env) => hasPermission(permission, env.id));
}

/** Link to a resource detail page; `?env=` is added only for an environment other than the selected one. */
export function environmentHref(path: string, environmentId: string): string {
	if (environmentId === (environmentStore.selected?.id ?? LOCAL_DOCKER_ENVIRONMENT_ID)) return path;
	return `${path}${path.includes('?') ? '&' : '?'}env=${encodeURIComponent(environmentId)}`;
}

/**
 * Detail loaders: the `?env=` environment, otherwise the selected one. With the preview on, an `env` that is
 * not shown is a 404 rather than the selected environment's resource of the same name. With it off, `env` is ignored.
 */
export async function resolveDetailEnvironmentId(url: URL): Promise<string> {
	const current = await environmentStore.getCurrentEnvironmentId();
	const requested = url.searchParams.get('env');
	if (!requested || !allEnvironmentsPreview.current) return current;
	if (!environmentStore.shown.some((env) => env.id === requested))
		error(404, m.common_not_found_title({ resource: m.resource_environment_cap() }));
	return requested;
}

/**
 * Destination for a create: the picked shown environment that grants `permission`, else the selected
 * one, else the first shown environment that grants it. With one shown environment this is the selected one.
 */
export function destinationEnvironmentId(picked: string, permission: string): string {
	const selected = environmentStore.selected?.id ?? LOCAL_DOCKER_ENVIRONMENT_ID;
	const permitted = environmentStore.shown.filter((env) => hasPermission(permission, env.id)).map((env) => env.id);
	const candidate = permitted.includes(picked) ? picked : selected;
	return permitted.includes(candidate) ? candidate : (permitted[0] ?? candidate);
}

/**
 * List query scopes an operation in these environments affects: each environment's own, their
 * merged one and the currently shown one. With one environment shown this is just its ID.
 */
export function listScopes(environmentIds: string[]): string[] {
	return [...new Set([...environmentIds, environmentIds.join(','), environmentStore.shownKey])];
}

/** Appended to an existing confirmation when an action runs across several environments. */
export function acrossEnvironmentsNote(environments: EnvironmentRef[]): string {
	if (environments.length < 2) return '';
	return m.all_environments_confirm_note({ count: environments.length });
}

/**
 * Runs a read once per environment; failures are returned beside the values. When reporting inline
 * (by default, when several environments are read), an access-denied response is left to the inline
 * alert instead of a toast, and a failure names its environment.
 */
export async function collectAcrossEnvironments<T>(
	environments: EnvironmentRef[],
	run: (environmentId: string, config: APIRequestConfig) => Promise<T>,
	reportInline = environments.length > 1
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
	const { values, failures } = await collectAcrossEnvironments(environments, (id) => run(id));
	for (const { environment, message } of failures) {
		toast.error(m.all_environments_request_failed({ environment: environment.name }), {
			id: `environment-request-failed-${environment.id}`,
			description: message
		});
	}
	return values;
}

function sumNumbers<C extends object>(values: C[]): C {
	const total: Record<string, unknown> = { ...values[0] };
	for (const value of values.slice(1)) {
		for (const [key, entry] of Object.entries(value)) {
			if (typeof entry === 'number') total[key] = Number(total[key] ?? 0) + entry;
		}
	}
	return total as C;
}

/** Sums numeric count fields across environments; failing environments are reported, not counted. */
export async function sumAcrossEnvironments<C extends object>(
	environments: EnvironmentRef[],
	fetch: (environmentId: string, config: APIRequestConfig) => Promise<C>
): Promise<C & { failedEnvironments: EnvironmentFailure[] }> {
	const { values, failures } = await collectAcrossEnvironments(environments, fetch);
	return { ...sumNumbers(values.map((result) => result.value)), failedEnvironments: failures };
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

/** Orders two sort values, comparing arrays element by element; missing values always sort last, whatever the direction. */
function compareSortValues(left: unknown, right: unknown, sign: number): number {
	const leftMissing = left === undefined || left === null;
	const rightMissing = right === undefined || right === null;
	if (leftMissing || rightMissing) return Number(leftMissing) - Number(rightMissing);
	if (Array.isArray(left) && Array.isArray(right)) {
		return left.reduce<number>((result, value, index) => result || compareSortValues(value, right[index], sign), 0);
	}
	if (typeof left === 'number' && typeof right === 'number') return sign * (left - right);
	// The backend sorts true first when ascending.
	if (typeof left === 'boolean' && typeof right === 'boolean') return sign * (Number(right) - Number(left));
	return sign * String(left).localeCompare(String(right), undefined, { numeric: true });
}

/** Splits units into pages the way the backend pages groups: a unit is never split, so the one crossing the limit finishes its page. */
function pageUnits<T>(units: T[][], limit: number): T[][][] {
	if (limit <= 0) return [units];
	const pages: T[][][] = [[]];
	let count = 0;
	for (const unit of units) {
		pages.at(-1)!.push(unit);
		count += unit.length;
		if (count >= limit) {
			pages.push([]);
			count = 0;
		}
	}
	if (pages.length > 1 && pages.at(-1)!.length === 0) pages.pop();
	return pages;
}

/**
 * Runs the same list request per environment and tags each row with its environment. With one
 * environment the request passes through; with several, every row is fetched, merged, sorted
 * and sliced to the requested page. `sortValue` reads computed sort columns; it defaults to the
 * row field named by the column. Same-named groups merge across environments and are paged whole.
 * An `environment` filter narrows the environments queried.
 */
export async function fetchAcrossEnvironments<
	P extends Page<T>,
	O extends SearchPaginationSortRequest,
	T extends object = P['data'][number]
>(
	environments: EnvironmentRef[],
	options: O,
	fetch: (environmentId: string, options: O, config: APIRequestConfig) => Promise<P>,
	sortValue: (row: T, column: string) => unknown = (row, column) => row[column as keyof T]
): Promise<EnvironmentPage<P>> {
	const { [ENVIRONMENT_FILTER]: environmentFilter, ...filters } = options.filters ?? {};
	// Picks are checked against every shown environment, so picking only ones this page skips shows nothing.
	const picked = environmentFilterIds(environmentFilter);
	const targets = picked.length > 0 ? environments.filter((env) => picked.includes(env.id)) : environments;
	// The environment filter and sort are applied here, never by an environment's API.
	const sortsByEnvironment = options.sort?.column === ENVIRONMENT_FILTER;
	const scoped: O = { ...options, filters: options.filters && filters, sort: sortsByEnvironment ? undefined : options.sort };
	if (targets.length === 0) {
		const pagination = { totalPages: 0, totalItems: 0, currentPage: 1, itemsPerPage: options.pagination?.limit ?? 0 };
		return { data: [], pagination, failedEnvironments: [] } as unknown as EnvironmentPage<P>;
	}
	const merging = targets.length > 1;
	const request = merging ? { ...scoped, pagination: { page: 1, limit: TABLE_PAGE_SIZE_ALL } } : scoped;
	// Filtering to one of several environments still reports its failure the merged way.
	const { values: pages, failures } = await collectAcrossEnvironments(
		targets,
		(id, config) => fetch(id, request, config),
		environments.length > 1
	);
	const tag = <R extends object>(environment: EnvironmentRef, rows: R[]) => rows.map((row) => tagEnvironment(row, environment));
	const { environment: firstEnvironment, value: first } = pages[0]!;
	// Environments without rows omit groups, so any response having them decides. If one with rows has none
	// (an older agent), the merged list stays flat rather than dropping its rows.
	const grouped = pages.some(({ value }) => value.groups) && pages.every(({ value }) => value.groups || value.data.length === 0);
	if (!merging) {
		return {
			...first,
			data: tag(firstEnvironment, first.data),
			groups: first.groups?.map((group) => ({ ...group, items: tag(firstEnvironment, group.items) })),
			failedEnvironments: []
		};
	}

	const { column, direction } = options.sort ?? {};
	const sign = direction === 'desc' ? -1 : 1;
	const rowSortValue = (row: EnvironmentRow<T>, sortColumn: string) =>
		sortsByEnvironment ? row.environmentName : sortValue(row, sortColumn);
	const rows = pages
		.flatMap(({ environment, value }) => tag(environment, value.data))
		.sort(
			(a, b) =>
				(column ? compareSortValues(rowSortValue(a, column), rowSortValue(b, column), sign) : 0) ||
				a.environmentName.localeCompare(b.environmentName) ||
				a.environmentId.localeCompare(b.environmentId)
		);

	// Group items are separate objects from the data rows, so membership is matched by environment and ID.
	const memberKey = (environmentId: string, row: object) =>
		`${environmentId}${KEY_SEPARATOR}${'id' in row ? String(row.id) : ''}`;
	const groupOf = new Map(
		pages.flatMap(({ environment, value }) =>
			(value.groups ?? []).flatMap((group) =>
				group.items.map((item) => [memberKey(environment.id, item), group.groupName] as const)
			)
		)
	);
	// Same-named groups merge across environments and follow the merged row order, like the backend groups its sorted rows.
	const groups = new Map<string, { groupName: string; items: typeof rows }>();
	for (const row of grouped ? rows : []) {
		const groupName = groupOf.get(memberKey(row.environmentId, row));
		if (groupName === undefined) continue;
		const entry = groups.get(groupName) ?? { groupName, items: [] };
		entry.items.push(row);
		groups.set(groupName, entry);
	}

	const limit = options.pagination?.limit ?? TABLE_PAGE_SIZE_ALL;
	const groupList = [...groups.values()];
	const pageList = pageUnits(grouped ? groupList.map((group) => group.items) : rows.map((row) => [row]), limit);
	const page = Math.min(limit > 0 ? (options.pagination?.page ?? 1) : 1, pageList.length);
	const selected = pageList[page - 1]!;
	const totals = sumNumbers(pages.map(({ value }) => value.pagination));
	return {
		...first,
		data: selected.flat(),
		pagination: {
			...totals,
			currentPage: page,
			itemsPerPage: limit > 0 ? limit : totals.totalItems,
			totalPages: pageList.length
		},
		counts: pages.some(({ value }) => value.counts)
			? sumNumbers(pages.flatMap(({ value }) => (value.counts ? [value.counts] : [])))
			: undefined,
		groups: grouped ? groupList.filter((group) => selected.includes(group.items)) : undefined,
		failedEnvironments: failures
	};
}
