import { m } from '#lib/paraglide/messages.js';
import type { IconCatalog } from '#lib/types/user.js';

export type LabelEntry = {
	key: string;
	value: string;
};

export type LabelValue =
	| { kind: 'text'; text: string }
	| { kind: 'code'; code: string }
	| { kind: 'boolean'; value: boolean }
	| { kind: 'url'; href: string; display: string }
	| { kind: 'datetime'; iso: string }
	| { kind: 'icon'; rawValue: string; previewUrl: string | null }
	| { kind: 'hours'; hours: number };

export type LabelField = {
	label: string;
	rawKey: string;
	value: LabelValue;
};

export type LabelResourceKind = 'router' | 'service' | 'middleware' | 'job';

export type LabelResource = {
	id: string;
	kind: LabelResourceKind;
	name: string;
	meta: string[];
	codeBlock?: {
		kind: 'rule' | 'command';
		value: string;
		rawKey: string;
		links?: Array<{ label: string; href: string | null }>;
	};
	fields: LabelField[];
};

export type LabelGroupId = 'traefik' | 'arcane' | 'oci' | 'ofelia' | 'watchtower' | 'compose';

export type LabelGroup = {
	id: LabelGroupId;
	fields: LabelField[];
	resources: LabelResource[];
};

export type ParsedLabelsResult = {
	groups: LabelGroup[];
	other: LabelEntry[];
};

const STRICT_BOOL_TRUE = new Set(['true']);
const STRICT_BOOL_FALSE = new Set(['false']);
const LOOSE_BOOL_TRUE = new Set(['true', '1', 'yes', 'on']);
const LOOSE_BOOL_FALSE = new Set(['false', '0', 'no', 'off']);
const BOOL_KEYS = new Set([
	'enable',
	'enabled',
	'updater',
	'update-check',
	'oneoff',
	'tls',
	'no-overlap',
	'passhostheader',
	'monitor-only',
	'no-pull',
	'hidden',
	'agent',
	'server',
	'upgrader',
	'recovery',
	'internal.resource',
	'volume-helper',
	'created'
]);
const LIST_KEYS = new Set(['depends-on', 'depends_on', 'entrypoints', 'middlewares', 'config_files', 'environment_file']);
const TEXT_KEYS = new Set(['title', 'description', 'authors', 'vendor']);
const ICON_VARIANTS: Record<string, '' | 'light' | 'dark'> = {
	'com.getarcaneapp.arcane.icon': '',
	'arcane.icon': '',
	'com.getarcaneapp.arcane.icon-light': 'light',
	'arcane.icon-light': 'light',
	'com.getarcaneapp.arcane.icon-dark': 'dark',
	'arcane.icon-dark': 'dark'
};

const ISO_DATETIME_REGEX = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}/;
const TRAEFIK_RESOURCE_REGEX = /^traefik\.(http|tcp|udp)\.(routers|services|middlewares)\.([^.]+)(?:\.(.+))?$/i;
const TRAEFIK_HOST_CLAUSE_REGEX = /\b(Host|HostSNI)\(([^)]+)\)/g;
const TRAEFIK_QUOTED_ARG_REGEX = /[`'"]([^`'"]+)[`'"]/g;
const HOSTNAME_REGEX = /^(?:localhost|(?:[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,})$/;
const OFELIA_JOB_REGEX = /^ofelia\.(job-exec|job-run|job-local|job-service-run)\.(.+)\.([^.]+)$/i;

type StructuredRule = {
	id: Exclude<LabelGroupId, 'traefik' | 'ofelia'>;
	matches: (lowerKey: string) => boolean;
	prefixes: string[];
};

const STRUCTURED_RULES: StructuredRule[] = [
	{
		id: 'arcane',
		matches: (k) =>
			k.startsWith('com.getarcaneapp.') ||
			k.startsWith('app.getarcane.') ||
			k.startsWith('io.getarcane.') ||
			k === 'com.arcane.created' ||
			k in ICON_VARIANTS,
		prefixes: ['com.getarcaneapp.arcane.', 'com.getarcaneapp.', 'app.getarcane.', 'io.getarcane.', 'arcane.', 'com.arcane.']
	},
	{
		id: 'oci',
		matches: (k) => k.startsWith('org.opencontainers.'),
		prefixes: ['org.opencontainers.image.', 'org.opencontainers.']
	},
	{
		id: 'watchtower',
		matches: (k) => k.startsWith('com.centurylinklabs.watchtower.'),
		prefixes: ['com.centurylinklabs.watchtower.']
	},
	{
		id: 'compose',
		matches: (k) => k.startsWith('com.docker.compose.'),
		prefixes: ['com.docker.compose.']
	}
];

function parseBool(raw: string, loose: boolean): boolean | null {
	const value = raw.trim().toLowerCase();
	if ((loose ? LOOSE_BOOL_TRUE : STRICT_BOOL_TRUE).has(value)) return true;
	if ((loose ? LOOSE_BOOL_FALSE : STRICT_BOOL_FALSE).has(value)) return false;
	return null;
}

function splitCsv(raw: string): string[] {
	return raw
		.split(',')
		.map((item) => item.trim())
		.filter(Boolean);
}

function parseSafeHttpUrl(raw: string): string | null {
	const trimmed = raw.trim();
	if (!trimmed || /\s/.test(trimmed)) return null;
	try {
		const url = new URL(trimmed);
		return url.protocol === 'http:' || url.protocol === 'https:' ? url.href : null;
	} catch {
		return null;
	}
}

function resolveIconPreviewUrl(raw: string, variant: '' | 'light' | 'dark', catalog: IconCatalog): string | null {
	const trimmed = raw.trim();
	if (!trimmed || trimmed.toLowerCase().startsWith('data:')) return null;
	const direct = parseSafeHttpUrl(trimmed);
	if (direct) return direct;
	const slug = encodeURIComponent(trimmed);
	if (catalog === 'dashboard-icons') {
		return `https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons/svg/${slug}.svg`;
	}
	return `https://cdn.jsdelivr.net/gh/selfhst/icons@main/svg/${slug}${variant ? `-${variant}` : ''}.svg`;
}

const KNOWN_FIELD_LABELS: Record<string, () => string> = {
	title: m.title,
	description: m.common_description,
	version: m.common_version,
	revision: m.revision,
	created: m.common_created,
	authors: m.authors,
	vendor: m.vendor,
	licenses: m.licenses,
	source: m.source,
	url: m.url,
	documentation: m.common_documentation,
	'ref.name': m.reference_name,
	'base.name': m.base_image,
	'base.digest': m.base_digest,
	icon: m.icon,
	'icon-light': m.light_icon,
	'icon-dark': m.dark_icon,
	updater: m.auto_update_title,
	'update-check': m.update_checks,
	'updater.strategy': m.editor_compose_updater_strategy_detail,
	'updater.constraint': m.editor_compose_updater_constraint_detail,
	'updater.tag-pattern': m.editor_compose_updater_tag_pattern_detail,
	'updater.delay': m.update_delay,
	'updater.configured-image': m.configured_image,
	'depends-on': m.depends_on,
	depends_on: m.depends_on,
	'stop-signal': m.stop_signal,
	agent: m.agent,
	hidden: m.hidden,
	'internal.resource': m.internal,
	enable: m.common_enabled,
	enabled: m.common_enabled,
	'docker.network': m.resource_network_cap,
	entrypoints: m.entrypoints,
	service: m.swarm_service,
	middlewares: m.middlewares,
	priority: m.priority,
	tls: m.tls,
	'tls.certresolver': m.cert_resolver,
	'loadbalancer.server.scheme': m.scheme,
	'loadbalancer.server.url': m.url,
	'loadbalancer.passhostheader': m.pass_host_header,
	schedule: m.jobs_schedule,
	command: m.common_command,
	container: m.container,
	image: m.common_image,
	user: m.common_user,
	'no-overlap': m.no_overlap,
	'monitor-only': m.monitor_only,
	'no-pull': m.no_pull,
	scope: m.common_scope,
	project: m.project,
	'project.working_dir': m.common_working_directory,
	'project.config_files': m.config_files,
	'project.environment_file': m.environment_file,
	'container-number': m.container_number,
	oneoff: m.one_off,
	'config-hash': m.config_hash
};

function resolveFieldLabel(suffix: string, rawKey: string): string {
	const labelFn = KNOWN_FIELD_LABELS[suffix.toLowerCase()];
	if (labelFn) return labelFn();
	return suffix || rawKey;
}

function formatFieldValue(rawKey: string, suffix: string, rawValue: string, iconCatalog: IconCatalog): LabelValue {
	const lowerKey = rawKey.toLowerCase();
	const lowerSuffix = suffix.toLowerCase();
	const lastSegment = lowerSuffix.split('.').at(-1) ?? lowerSuffix;

	if (lowerKey in ICON_VARIANTS) {
		return { kind: 'icon', rawValue, previewUrl: resolveIconPreviewUrl(rawValue, ICON_VARIANTS[lowerKey]!, iconCatalog) };
	}
	if (lowerKey === 'com.getarcaneapp.arcane.updater.delay' && /^\d+$/.test(rawValue.trim())) {
		return { kind: 'hours', hours: Number.parseInt(rawValue.trim(), 10) };
	}

	const boolVal = parseBool(
		rawValue,
		BOOL_KEYS.has(lowerSuffix) || BOOL_KEYS.has(lastSegment) || lowerKey === 'com.getarcaneapp.arcane'
	);
	if (boolVal !== null) return { kind: 'boolean', value: boolVal };

	const href = parseSafeHttpUrl(rawValue);
	if (href) return { kind: 'url', href, display: rawValue.trim() };

	if (ISO_DATETIME_REGEX.test(rawValue.trim()) && !Number.isNaN(Date.parse(rawValue.trim()))) {
		return { kind: 'datetime', iso: rawValue.trim() };
	}

	if (LIST_KEYS.has(lastSegment)) {
		const items = splitCsv(rawValue).map((item) => {
			if (lowerKey !== 'com.docker.compose.depends_on') return item;
			const [svc, cond] = item.split(':');
			return svc && cond ? `${svc} (${cond})` : item;
		});
		if (items.length > 0) return { kind: 'code', code: items.join(', ') };
	}

	if (TEXT_KEYS.has(lastSegment)) {
		return { kind: 'text', text: rawValue };
	}

	return { kind: 'code', code: rawValue };
}

function stripPrefix(key: string, prefixes: string[]): string {
	const lower = key.toLowerCase();
	const matched = prefixes.find((prefix) => lower.startsWith(prefix));
	return matched ? key.slice(matched.length) : key;
}

function parseStructuredGroup(rule: StructuredRule, entries: LabelEntry[], iconCatalog: IconCatalog): LabelGroup {
	const fields: LabelField[] = entries.map((entry) => {
		const suffix = stripPrefix(entry.key, rule.prefixes);
		return {
			label: resolveFieldLabel(suffix, entry.key),
			rawKey: entry.key,
			value: formatFieldValue(entry.key, suffix, entry.value, iconCatalog)
		};
	});
	return { id: rule.id, fields, resources: [] };
}

function extractTraefikHosts(rule: string, protocol: string, useHttps: boolean): Array<{ label: string; href: string | null }> {
	const seen = new Set<string>();
	const links: Array<{ label: string; href: string | null }> = [];
	for (const match of rule.matchAll(TRAEFIK_HOST_CLAUSE_REGEX)) {
		for (const arg of (match[2] ?? '').matchAll(TRAEFIK_QUOTED_ARG_REGEX)) {
			const host = (arg[1] ?? '').trim();
			if (!host || !HOSTNAME_REGEX.test(host) || seen.has(host.toLowerCase())) continue;
			seen.add(host.toLowerCase());
			if (protocol === 'http' && match[1] === 'Host') {
				const href = `${useHttps ? 'https' : 'http'}://${host}`;
				links.push({ label: href, href });
			} else {
				links.push({ label: host, href: null });
			}
		}
	}
	return links;
}

const TRAEFIK_KIND_TO_SINGULAR: Record<'routers' | 'services' | 'middlewares', LabelResourceKind> = {
	routers: 'router',
	services: 'service',
	middlewares: 'middleware'
};

function parseTraefikGroup(entries: LabelEntry[], iconCatalog: IconCatalog): LabelGroup {
	const fields: LabelField[] = [];
	const buckets: Record<
		'routers' | 'services' | 'middlewares',
		Map<string, { protocol: string; name: string; props: Array<{ prop: string; entry: LabelEntry }> }>
	> = {
		routers: new Map(),
		services: new Map(),
		middlewares: new Map()
	};

	for (const entry of entries) {
		const match = TRAEFIK_RESOURCE_REGEX.exec(entry.key);
		if (!match) {
			const suffix = entry.key.slice('traefik.'.length);
			fields.push({
				label: resolveFieldLabel(suffix, entry.key),
				rawKey: entry.key,
				value: formatFieldValue(entry.key, suffix, entry.value, iconCatalog)
			});
			continue;
		}
		const protocol = (match[1] ?? 'http').toLowerCase();
		const kind = (match[2] ?? 'routers').toLowerCase() as 'routers' | 'services' | 'middlewares';
		const name = match[3] ?? '';
		const id = `${protocol}:${name}`;
		let item = buckets[kind].get(id);
		if (!item) {
			item = { protocol, name, props: [] };
			buckets[kind].set(id, item);
		}
		item.props.push({ prop: match[4] ?? '', entry });
	}

	const buildResources = (kind: 'routers' | 'services' | 'middlewares'): LabelResource[] =>
		Array.from(buckets[kind].values())
			.sort((a, b) => a.protocol.localeCompare(b.protocol) || a.name.localeCompare(b.name))
			.map(({ protocol, name, props }) => {
				const meta: string[] = [protocol.toUpperCase()];
				const resourceFields: LabelField[] = [];
				let codeBlock: LabelResource['codeBlock'];
				let tlsEnabled: boolean | undefined;
				let tlsRawKey: string | undefined;
				let certResolver: string | undefined;
				let certResolverRawKey: string | undefined;
				let hasSecureEntrypoint = false;
				const mwTypes = new Set<string>();

				for (const { prop, entry } of props) {
					const lowerProp = prop.toLowerCase();
					if (kind === 'routers' && lowerProp === 'rule') {
						codeBlock = { kind: 'rule', value: entry.value, rawKey: entry.key };
						continue;
					}
					if (kind === 'routers' && lowerProp === 'tls') {
						const parsedTls = parseBool(entry.value, true);
						if (parsedTls !== null) {
							tlsEnabled = parsedTls;
							tlsRawKey = entry.key;
							continue;
						}
					}
					if (kind === 'routers' && lowerProp === 'tls.certresolver') {
						certResolver = entry.value;
						certResolverRawKey = entry.key;
						if (tlsEnabled === undefined) tlsEnabled = true;
						continue;
					}
					if (kind === 'routers' && lowerProp === 'entrypoints') {
						const eps = splitCsv(entry.value);
						hasSecureEntrypoint = eps.some((ep) => /^(?:https|.*secure)$/i.test(ep));
					}
					if (kind === 'services' && lowerProp === 'loadbalancer.server.port') {
						meta.push(m.port_number({ port: entry.value }));
						continue;
					}
					if (kind === 'middlewares') {
						const dot = prop.indexOf('.');
						const mwType = dot === -1 ? prop : prop.slice(0, dot);
						const nested = dot === -1 ? prop : prop.slice(dot + 1);
						if (mwType) mwTypes.add(mwType);
						resourceFields.push({
							label: nested || mwType || entry.key,
							rawKey: entry.key,
							value: formatFieldValue(entry.key, nested, entry.value, iconCatalog)
						});
						continue;
					}
					resourceFields.push({
						label: resolveFieldLabel(prop, entry.key),
						rawKey: entry.key,
						value: formatFieldValue(entry.key, prop, entry.value, iconCatalog)
					});
				}

				if (tlsEnabled === true) {
					meta.push(certResolver ? `${m.tls()} (${certResolver})` : m.tls());
				} else if (tlsEnabled === false) {
					resourceFields.push({
						label: m.tls(),
						rawKey: tlsRawKey ?? '',
						value: { kind: 'boolean', value: false }
					});
					if (certResolver && certResolverRawKey) {
						resourceFields.push({
							label: m.cert_resolver(),
							rawKey: certResolverRawKey,
							value: { kind: 'code', code: certResolver }
						});
					}
				}
				for (const mwType of mwTypes) {
					meta.push(mwType);
				}
				if (codeBlock) {
					codeBlock.links = extractTraefikHosts(codeBlock.value, protocol, Boolean(tlsEnabled || hasSecureEntrypoint));
				}

				return {
					id: `${kind}:${protocol}:${name}`,
					kind: TRAEFIK_KIND_TO_SINGULAR[kind],
					name,
					meta,
					codeBlock,
					fields: resourceFields
				};
			});

	const resources: LabelResource[] = [
		...buildResources('routers'),
		...buildResources('services'),
		...buildResources('middlewares')
	];

	return { id: 'traefik', fields, resources };
}

function parseOfeliaGroup(entries: LabelEntry[], iconCatalog: IconCatalog): LabelGroup {
	const fields: LabelField[] = [];
	const jobMap = new Map<string, { type: string; name: string; props: Array<{ prop: string; entry: LabelEntry }> }>();

	for (const entry of entries) {
		const match = OFELIA_JOB_REGEX.exec(entry.key);
		if (!match) {
			const suffix = entry.key.slice('ofelia.'.length);
			fields.push({
				label: resolveFieldLabel(suffix, entry.key),
				rawKey: entry.key,
				value: formatFieldValue(entry.key, suffix, entry.value, iconCatalog)
			});
			continue;
		}
		const type = (match[1] ?? 'job-exec').toLowerCase();
		const name = match[2] ?? '';
		const id = `${type}:${name}`;
		let job = jobMap.get(id);
		if (!job) {
			job = { type, name, props: [] };
			jobMap.set(id, job);
		}
		job.props.push({ prop: match[3] ?? '', entry });
	}

	const resources: LabelResource[] = Array.from(jobMap.values())
		.sort((a, b) => a.type.localeCompare(b.type) || a.name.localeCompare(b.name))
		.map(({ type, name, props }) => {
			const meta: string[] = [type];
			const jobFields: LabelField[] = [];
			let codeBlock: LabelResource['codeBlock'];

			for (const { prop, entry } of props) {
				const lowerProp = prop.toLowerCase();
				if (lowerProp === 'command') {
					codeBlock = { kind: 'command', value: entry.value, rawKey: entry.key };
					continue;
				}
				if (lowerProp === 'schedule') {
					meta.push(entry.value);
					continue;
				}
				jobFields.push({
					label: resolveFieldLabel(prop, entry.key),
					rawKey: entry.key,
					value: formatFieldValue(entry.key, prop, entry.value, iconCatalog)
				});
			}

			return { id: `${type}:${name}`, kind: 'job', name, meta, codeBlock, fields: jobFields };
		});

	return { id: 'ofelia', fields, resources };
}

export function parseWellKnownLabels(
	labels: Record<string, string> | null | undefined,
	iconCatalog: IconCatalog = 'selfhst'
): ParsedLabelsResult {
	const raw: LabelEntry[] = Object.entries(labels ?? {})
		.map(([key, value]) => ({ key, value: value == null ? '' : String(value) }))
		.sort((a, b) => a.key.localeCompare(b.key));

	const grouped = new Map<LabelGroupId, LabelEntry[]>();
	const other: LabelEntry[] = [];

	for (const entry of raw) {
		const lower = entry.key.toLowerCase();
		let groupId: LabelGroupId | null = null;
		if (lower.startsWith('traefik.')) {
			groupId = 'traefik';
		} else if (lower.startsWith('ofelia.')) {
			groupId = 'ofelia';
		} else {
			groupId = STRUCTURED_RULES.find((rule) => rule.matches(lower))?.id ?? null;
		}
		if (!groupId) {
			other.push(entry);
			continue;
		}
		const list = grouped.get(groupId) ?? [];
		list.push(entry);
		grouped.set(groupId, list);
	}

	const groups: LabelGroup[] = [];
	const traefikEntries = grouped.get('traefik');
	if (traefikEntries) groups.push(parseTraefikGroup(traefikEntries, iconCatalog));

	for (const rule of STRUCTURED_RULES) {
		const list = grouped.get(rule.id);
		if (list) groups.push(parseStructuredGroup(rule, list, iconCatalog));
	}

	const ofeliaEntries = grouped.get('ofelia');
	if (ofeliaEntries) groups.push(parseOfeliaGroup(ofeliaEntries, iconCatalog));

	return { groups, other };
}
