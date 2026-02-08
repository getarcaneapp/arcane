<script lang="ts">
	import { onMount } from 'svelte';
	import { z } from 'zod/v4';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import * as Tabs from '$lib/components/ui/tabs/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { m } from '$lib/paraglide/messages';
	import settingsStore from '$lib/stores/config-store';
	import { createForm } from '$lib/utils/form.utils';
	import { toast } from 'svelte-sonner';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { ResourceDetailLayout } from '$lib/layouts/index.js';
	import TabbedPageLayout from '$lib/layouts/tabbed-page-layout.svelte';
	import { sanitizeLogText } from '$lib/utils/log-text';
	import {
		CodeIcon,
		TerminalIcon,
		ArrowDownIcon,
		ClockIcon,
		TagIcon,
		SettingsIcon,
		InfoIcon,
		EllipsisIcon,
		RedeployIcon
	} from '$lib/icons';
	import * as Card from '$lib/components/ui/card';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import type { ColumnSpec, MobileFieldVisibility } from '$lib/components/arcane-table';
	import { UniversalMobileCard } from '$lib/components/arcane-table';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { ResponsiveDialog } from '$lib/components/ui/responsive-dialog/index.js';
	import {
		type LayerProgress,
		calculateOverallProgress,
		areAllLayersComplete,
		updateLayerFromStreamData,
		extractErrorMessage,
		getLayerStats,
		isIndeterminatePhase,
		getAggregateStatus
	} from '$lib/utils/pull-progress';
	import ResizableSplit from '$lib/components/resizable-split.svelte';
	import BuildControls from './components/build-controls.svelte';
	import BuildWorkspacePanel from './components/build-workspace-panel.svelte';
	import BuildConfigPanel from './components/build-config-panel.svelte';
	import BuildOutputPanel from './components/build-output-panel.svelte';
	import type { BuildProviderOption } from './components/build-form.types';
	import { imageService } from '$lib/services/image-service';
	import type { ImageBuildRecord, ImageBuildStatus } from '$lib/types/image.type';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
	import { queryKeys } from '$lib/query/query-keys';
	import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { format } from 'date-fns';

	const buildsRoot = $derived((($settingsStore?.buildsDirectory ?? '/builds') as string).trim() || '/builds');
	const buildsRootLabel = $derived.by(() => {
		const raw = buildsRoot.trim();
		if (!raw) return '/builds';
		if (raw.length <= 36) return raw;
		const parts = raw.split('/').filter(Boolean);
		if (parts.length === 0) return raw;
		const tail = parts.slice(-2).join('/');
		return `…/${tail}`;
	});

	const depotAvailable = $derived.by(() => {
		const projectId = ($settingsStore?.depotProjectId ?? '').trim();
		const token = ($settingsStore?.depotToken ?? '').trim();
		return Boolean($settingsStore?.depotConfigured) || (Boolean(projectId) && Boolean(token));
	});

	const providerOptions = $derived.by<BuildProviderOption[]>(() => {
		const options: BuildProviderOption[] = [
			{ label: m.local_docker(), value: 'local', description: m.local_docker_description() }
		];
		if (depotAvailable) {
			options.push({ label: m.depot(), value: 'depot', description: m.depot_description() });
		}
		return options;
	});

	let selectedContextPath = $state('/');

	const contextDir = $derived.by(() => {
		const root = buildsRoot.endsWith('/') ? buildsRoot.slice(0, -1) : buildsRoot;
		if (selectedContextPath === '/' || selectedContextPath === '') return root;
		return `${root}${selectedContextPath.startsWith('/') ? '' : '/'}${selectedContextPath}`;
	});

	const formSchema = z.object({
		dockerfile: z.string().optional().default(''),
		tags: z.string().min(1, m.image_tags_required()),
		target: z.string().optional().default(''),
		buildArgs: z.string().optional().default(''),
		labels: z.string().optional().default(''),
		cacheFrom: z.string().optional().default(''),
		cacheTo: z.string().optional().default(''),
		network: z.string().optional().default(''),
		isolation: z.string().optional().default(''),
		shmSize: z.string().optional().default(''),
		ulimits: z.string().optional().default(''),
		entitlements: z.string().optional().default(''),
		privileged: z.boolean().default(false),
		extraHosts: z.string().optional().default(''),
		platforms: z.string().optional().default(''),
		noCache: z.boolean().default(false),
		pull: z.boolean().default(false),
		provider: z.enum(['local', 'depot']).default('local'),
		push: z.boolean().default(false),
		load: z.boolean().default(true)
	});

	const { inputs, ...form } = createForm<typeof formSchema>(formSchema, {
		dockerfile: '',
		tags: '',
		target: '',
		buildArgs: '',
		labels: '',
		cacheFrom: '',
		cacheTo: '',
		network: '',
		isolation: '',
		shmSize: '',
		ulimits: '',
		entitlements: '',
		privileged: false,
		extraHosts: '',
		platforms: '',
		noCache: false,
		pull: false,
		provider: ($settingsStore?.buildProvider as 'local' | 'depot') ?? 'local',
		push: false,
		load: true
	});

	let isBuilding = $state(false);
	let isDesktop = $state(true);
	let buildProgress = $state(0);
	let buildStatusText = $state('');
	let buildError = $state('');
	let layerProgress = $state<Record<string, LayerProgress>>({});
	let hasReachedComplete = $state(false);
	let logLines = $state<string[]>([]);
	let autoScroll = $state(true);
	let mainTab = $state<'build' | 'history'>('build');
	let buildTab = $state('workspace');
	let rightPanelTab = $state<'config' | 'output'>('config');
	let showAdvanced = $state(false);
	let buildHistoryItems = $state<Paginated<ImageBuildRecord>>({
		data: [],
		pagination: { totalPages: 1, totalItems: 0, currentPage: 1, itemsPerPage: 20 }
	});
	let buildHistoryRequestOptions = $state<SearchPaginationSortRequest>({
		pagination: { page: 1, limit: 20 },
		sort: { column: 'createdAt', direction: 'desc' }
	});
	let buildHistorySelectedIds = $state<string[]>([]);
	let buildHistoryMobileFieldVisibility = $state<Record<string, boolean>>({});
	let buildHistorySelected = $state<ImageBuildRecord | null>(null);
	let buildHistorySelectedId = $state<string | null>(null);
	let buildHistoryDetailsOpen = $state(false);

	const selectedEnvId = $derived(environmentStore.selected?.id || '0');
	const queryClient = useQueryClient();

	const buildHistoryQuery = createQuery(() => ({
		queryKey: queryKeys.images.buildsList(selectedEnvId, buildHistoryRequestOptions),
		queryFn: () => imageService.getImageBuilds(buildHistoryRequestOptions),
		onSuccess: (data: Paginated<ImageBuildRecord>) => {
			buildHistoryItems = data;
		},
		onError: (err: unknown) => {
			const anyErr = err as any;
			const message = anyErr?.message ? String(anyErr.message) : m.common_error();
			if (message && message !== buildHistoryQueryLastError) {
				buildHistoryQueryLastError = message;
				toast.error(message);
			}
		}
	}));

	let buildHistoryQueryLastError = $state<string | null>(null);

	const buildHistoryDetailQuery = createQuery(() => ({
		queryKey: buildHistorySelectedId
			? queryKeys.images.buildRecord(selectedEnvId, buildHistorySelectedId)
			: (['images', selectedEnvId, 'builds', 'none'] as const),
		queryFn: () => imageService.getImageBuild(buildHistorySelectedId!),
		enabled: !!buildHistorySelectedId && buildHistoryDetailsOpen,
		onSuccess: (data: ImageBuildRecord) => {
			buildHistorySelected = data;
		},
		onError: (err: unknown) => {
			const anyErr = err as any;
			const message = anyErr?.message ? String(anyErr.message) : m.common_error();
			if (message && message !== buildHistoryDetailLastError) {
				buildHistoryDetailLastError = message;
				toast.error(message);
			}
		}
	}));

	let buildHistoryDetailLastError = $state<string | null>(null);
	const buildHistoryDetailsLoading = $derived(buildHistoryDetailQuery.isPending || buildHistoryDetailQuery.isFetching);

	type ImageBuildRequest = {
		envId: string;
		contextDir: string;
		dockerfile?: string;
		tags: string[];
		target?: string;
		buildArgs?: Record<string, string>;
		labels?: Record<string, string>;
		cacheFrom?: string[];
		cacheTo?: string[];
		noCache?: boolean;
		pull?: boolean;
		network?: string;
		isolation?: string;
		shmSize?: number;
		ulimits?: Record<string, string>;
		entitlements?: string[];
		privileged?: boolean;
		extraHosts?: string[];
		platforms?: string[];
		provider: 'local' | 'depot';
		push: boolean;
		load: boolean;
	};

	const buildMutation = createMutation(() => ({
		mutationKey: queryKeys.images.buildRun(selectedEnvId),
		mutationFn: async (request: ImageBuildRequest) => {
			const { envId, ...payload } = request;
			const response = await fetch(`/api/environments/${envId}/images/build`, {
				method: 'POST',
				headers: {
					'Content-Type': 'application/json'
				},
				body: JSON.stringify(payload)
			});

			if (!response.ok || !response.body) {
				const errorData = await response.json().catch(() => ({
					data: { message: m.build_request_failed() }
				}));
				const errorMessage =
					errorData.data?.message ||
					errorData.error ||
					errorData.message ||
					m.build_request_failed_http({ status: response.status });
				throw new Error(String(errorMessage));
			}

			const reader = response.body.getReader();
			const decoder = new TextDecoder();
			let buffer = '';

			while (true) {
				const { done, value } = await reader.read();
				if (done) {
					buildStatusText = m.finalizing_build();
					appendLog(m.finalizing_build());
					break;
				}

				buffer += decoder.decode(value, { stream: true });
				const lines = buffer.split('\n');
				buffer = lines.pop() || '';

				for (const line of lines) {
					if (line.trim() === '') continue;
					try {
						const event = JSON.parse(line);
						const errorMsg = extractErrorMessage(event, m.build_failed());
						if (errorMsg) {
							const cleanErrorMsg = sanitizeLogText(errorMsg);
							buildError = cleanErrorMsg;
							buildStatusText = cleanErrorMsg.toLowerCase().startsWith(m.build_failed().toLowerCase())
								? cleanErrorMsg
								: m.build_failed_with_error({ error: cleanErrorMsg });
							appendLog(m.build_error_log({ error: cleanErrorMsg }));
							continue;
						}

						if (event.status) {
							const idSuffix = event.id ? ` ${event.id}` : '';
							appendLog(`${sanitizeLogText(String(event.status))}${idSuffix}`);
							buildStatusText = sanitizeLogText(String(event.status));
						}

						layerProgress = updateLayerFromStreamData(layerProgress, event);
						updateProgress();
					} catch {
						appendLog(line);
					}
				}
			}

			updateProgress();
			if (!buildError && buildProgress < 100 && areAllLayersComplete(layerProgress)) {
				buildProgress = 100;
			}

			if (buildError) {
				throw new Error(buildError);
			}

			hasReachedComplete = true;
			buildProgress = 100;
			buildStatusText = m.build_completed();
			appendLog(m.build_completed());
		},
		onSettled: async (_data, _error, variables) => {
			await queryClient.invalidateQueries({ queryKey: queryKeys.images.builds(variables.envId) });
		}
	}));

	const layerStats = $derived(getLayerStats(layerProgress, hasReachedComplete));
	const aggregateStatus = $derived.by(() => {
		const status = getAggregateStatus(layerProgress, buildStatusText, hasReachedComplete);
		if (!isBuilding) return status;
		if (!status) return m.progress_building();
		const normalized = status.toLowerCase();
		if (normalized === 'pulling' || normalized === 'preparing') return m.progress_building();
		return status;
	});
	const isIndeterminate = $derived(isIndeterminatePhase(layerProgress, buildProgress));
	const progressValue = $derived(Math.round(hasReachedComplete ? 100 : buildProgress));
	const selectedProviderLabel = $derived.by(
		() => providerOptions.find((option) => option.value === $inputs.provider.value)?.label ?? m.local_docker()
	);
	const statusLabel = $derived.by(() => {
		if (buildError) return m.common_error();
		if (hasReachedComplete) return m.build_completed();
		if (isBuilding) return m.common_live();
		return m.idle();
	});
	const buildMobileTabItems = $derived([
		{ value: 'workspace', label: m.build_workspace_files() },
		{ value: 'configuration', label: m.build_configuration() },
		{ value: 'output', label: m.build_output() }
	]);

	const mainTabItems = $derived([
		{ value: 'build', label: m.manual_build_workspace() },
		{ value: 'history', label: m.builds() }
	]);

	type BuildOutputEntry = {
		raw: string;
		isJson: boolean;
		type?: string;
		status?: string;
		id?: string;
		phase?: string;
		error?: string;
		progress?: {
			current?: number;
			total?: number;
		};
	};

	const buildHistoryOutputEntries = $derived.by(() => parseBuildOutput(buildHistorySelected?.output ?? ''));

	function getBuildTitle(build: ImageBuildRecord) {
		return build.tags?.[0] || build.contextDir.split('/').pop() || build.contextDir;
	}

	const buildHistoryColumns = [
		{
			accessorKey: 'status',
			title: m.common_status(),
			sortable: true,
			cell: BuildHistoryStatusCell
		},
		{
			id: 'tags',
			title: m.common_tags(),
			cell: BuildHistoryTagsCell
		},
		{
			accessorKey: 'provider',
			title: m.build_provider(),
			sortable: true,
			cell: BuildHistoryProviderCell
		},
		{
			accessorKey: 'createdAt',
			title: m.common_created(),
			sortable: true,
			cell: BuildHistoryTimeCell
		},
		{
			accessorKey: 'durationMs',
			title: m.build_duration_label(),
			cell: BuildHistoryDurationCell
		}
	] satisfies ColumnSpec<ImageBuildRecord>[];

	const buildHistoryMobileFields = [
		{ id: 'status', label: m.common_status(), defaultVisible: true },
		{ id: 'tags', label: m.common_tags(), defaultVisible: true },
		{ id: 'context', label: m.build_context(), defaultVisible: true },
		{ id: 'provider', label: m.build_provider(), defaultVisible: true },
		{ id: 'createdAt', label: m.common_created(), defaultVisible: true },
		{ id: 'durationMs', label: m.build_duration_label(), defaultVisible: false }
	];

	onMount(() => {
		const mq = window.matchMedia('(min-width: 1024px)');
		const update = () => {
			isDesktop = mq.matches;
		};

		update();

		if ('addEventListener' in mq) {
			mq.addEventListener('change', update);
			return () => mq.removeEventListener('change', update);
		}

		// @ts-expect-error legacy MediaQueryList API
		mq.addListener(update);
		return () => {
			// @ts-expect-error legacy MediaQueryList API
			mq.removeListener(update);
		};
	});

	$effect(() => {
		if (!depotAvailable && $inputs.provider.value === 'depot') {
			$inputs.provider.value = 'local';
		}
	});

	function resetState() {
		isBuilding = false;
		buildProgress = 0;
		buildStatusText = '';
		buildError = '';
		layerProgress = {};
		hasReachedComplete = false;
		logLines = [];
	}

	function appendLog(line: string) {
		const sanitized = sanitizeLogText(line);
		// Some tools output standalone reset codes (which become empty after sanitizing).
		if (sanitized.trim() === '') return;
		logLines = [...logLines, sanitized];
	}

	function updateProgress() {
		buildProgress = calculateOverallProgress(layerProgress);
	}

	function parseTags(raw: string): string[] {
		return raw
			.split(/[\n,]/)
			.map((t) => t.trim())
			.filter(Boolean);
	}

	function parsePlatforms(raw: string): string[] {
		return raw
			.split(/[\n,]/)
			.map((t) => t.trim())
			.filter(Boolean);
	}

	function parseBuildArgs(raw: string): Record<string, string> {
		const result: Record<string, string> = {};
		for (const line of raw.split('\n')) {
			const trimmed = line.trim();
			if (!trimmed) continue;
			const idx = trimmed.indexOf('=');
			if (idx === -1) continue;
			const key = trimmed.slice(0, idx).trim();
			const value = trimmed.slice(idx + 1).trim();
			if (!key) continue;
			result[key] = value;
		}
		return result;
	}

	function parseList(raw: string): string[] {
		return raw
			.split(/[\n,]/)
			.map((value) => value.trim())
			.filter(Boolean);
	}

	function parseOptionalBytes(raw: string): number | undefined {
		const trimmed = raw.trim();
		if (!trimmed) return undefined;
		const parsed = Number.parseInt(trimmed, 10);
		if (!Number.isFinite(parsed) || parsed <= 0) return undefined;
		return parsed;
	}

	function validateProviderCompatibility(
		provider: 'local' | 'depot',
		values: {
			cacheTo: string[];
			entitlements: string[];
			privileged: boolean;
			platforms: string[];
			network?: string;
			isolation?: string;
			shmSize?: number;
			ulimits: Record<string, string>;
			extraHosts: string[];
		}
	): string | null {
		if (provider === 'local') {
			const unsupported: string[] = [];
			if (values.cacheTo.length > 0) unsupported.push('cacheTo');
			if (values.entitlements.length > 0) unsupported.push('entitlements');
			if (values.privileged) unsupported.push('privileged');
			if (values.platforms.length > 1) unsupported.push('platforms');
			if (unsupported.length > 0) {
				return m.build_provider_unsupported_local({ fields: unsupported.sort().join(', ') });
			}
			return null;
		}

		const unsupported: string[] = [];
		if (values.network) unsupported.push('network');
		if (values.isolation) unsupported.push('isolation');
		if (values.shmSize) unsupported.push('shmSize');
		if (Object.keys(values.ulimits).length > 0) unsupported.push('ulimits');
		if (values.extraHosts.length > 0) unsupported.push('extraHosts');
		if (unsupported.length > 0) {
			return m.build_provider_unsupported_depot({ fields: unsupported.sort().join(', ') });
		}
		return null;
	}

	function formatKeyValueMap(values?: Record<string, string>) {
		if (!values || Object.keys(values).length === 0) return '';
		return Object.entries(values)
			.map(([key, value]) => `${key}=${value}`)
			.join('\n');
	}

	function formatStringList(values?: string[]) {
		if (!values || values.length === 0) return '';
		return values.join('\n');
	}

	function formatTimestamp(value?: string) {
		if (!value) return '-';
		try {
			return format(new Date(value), 'PP p');
		} catch {
			return value;
		}
	}

	function formatDuration(ms?: number) {
		if (!ms || ms <= 0) return '-';
		const totalSeconds = Math.round(ms / 1000);
		const minutes = Math.floor(totalSeconds / 60);
		const seconds = totalSeconds % 60;
		if (minutes > 0) return `${minutes}m ${seconds}s`;
		return `${seconds}s`;
	}

	function formatBytes(value: number) {
		if (!Number.isFinite(value) || value <= 0) return '0 B';
		const units = ['B', 'KB', 'MB', 'GB', 'TB'];
		let size = value;
		let unitIndex = 0;
		while (size >= 1024 && unitIndex < units.length - 1) {
			size /= 1024;
			unitIndex += 1;
		}
		const precision = size >= 10 || unitIndex === 0 ? 0 : 1;
		return `${size.toFixed(precision)} ${units[unitIndex]}`;
	}

	function getProgressPercent(current?: number, total?: number) {
		if (current === undefined || total === undefined || total <= 0) return 0;
		return Math.min(100, Math.max(0, Math.round((current / total) * 100)));
	}

	function formatProgress(current?: number, total?: number) {
		if (current === undefined || total === undefined || total <= 0) return '-';
		return `${formatBytes(current)} / ${formatBytes(total)}`;
	}

	function formatBuildArgs(buildArgs?: Record<string, string>) {
		return formatKeyValueMap(buildArgs);
	}

	function getContextPathFromBuild(build: ImageBuildRecord) {
		const root = buildsRoot.endsWith('/') ? buildsRoot.slice(0, -1) : buildsRoot;
		if (!build.contextDir || build.contextDir === root) return '/';
		if (build.contextDir.startsWith(`${root}/`)) {
			return build.contextDir.slice(root.length);
		}
		return '/';
	}

	function parseBuildOutput(output: string): BuildOutputEntry[] {
		if (!output) return [];
		return output
			.split('\n')
			.map((line) => line.trim())
			.filter(Boolean)
			.map((line) => {
				try {
					const data = JSON.parse(line) as Record<string, unknown>;
					if (!data || typeof data !== 'object') {
						return { raw: sanitizeLogText(line), isJson: false } satisfies BuildOutputEntry;
					}
					const progressDetail = data.progressDetail as Record<string, unknown> | undefined;
					const progress = progressDetail
						? {
								current: typeof progressDetail.current === 'number' ? progressDetail.current : undefined,
								total: typeof progressDetail.total === 'number' ? progressDetail.total : undefined
							}
						: undefined;
					return {
						raw: sanitizeLogText(line),
						isJson: true,
						type: typeof data.type === 'string' ? data.type : undefined,
						status: data.status ? sanitizeLogText(String(data.status)) : undefined,
						id: data.id ? String(data.id) : undefined,
						phase: typeof data.phase === 'string' ? data.phase : undefined,
						error: data.error ? sanitizeLogText(String(data.error)) : undefined,
						progress
					} satisfies BuildOutputEntry;
				} catch {
					return { raw: sanitizeLogText(line), isJson: false } satisfies BuildOutputEntry;
				}
			});
	}

	function buildHistoryStatusLabel(status?: ImageBuildStatus) {
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

	function getStatusBadgeVariant(status?: ImageBuildStatus) {
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

	async function loadBuildHistory(options: SearchPaginationSortRequest = buildHistoryRequestOptions) {
		buildHistoryRequestOptions = options;
		const result = await buildHistoryQuery.refetch();
		return result.data ?? buildHistoryItems;
	}

	function openBuildDetails(build: ImageBuildRecord) {
		buildHistorySelectedId = build.id;
		buildHistorySelected = build;
		buildHistoryDetailsOpen = true;
	}

	function applyBuildConfig(build: ImageBuildRecord) {
		form.setValue('tags', build.tags?.join(', ') ?? '');
		form.setValue('dockerfile', build.dockerfile ?? '');
		form.setValue('target', build.target ?? '');
		form.setValue('platforms', build.platforms?.join(', ') ?? '');
		form.setValue('buildArgs', formatBuildArgs(build.buildArgs));
		form.setValue('labels', formatKeyValueMap(build.labels));
		form.setValue('cacheFrom', formatStringList(build.cacheFrom));
		form.setValue('cacheTo', formatStringList(build.cacheTo));
		form.setValue('network', build.network ?? '');
		form.setValue('isolation', build.isolation ?? '');
		form.setValue('shmSize', build.shmSize ? String(build.shmSize) : '');
		form.setValue('ulimits', formatKeyValueMap(build.ulimits));
		form.setValue('entitlements', formatStringList(build.entitlements));
		form.setValue('privileged', build.privileged ?? false);
		form.setValue('extraHosts', formatStringList(build.extraHosts));
		form.setValue('noCache', build.noCache ?? false);
		form.setValue('pull', build.pull ?? false);
		form.setValue('provider', (build.provider as 'local' | 'depot') ?? 'local');
		form.setValue('push', build.push ?? false);
		form.setValue('load', build.load ?? true);

		showAdvanced = Boolean(
			build.dockerfile ||
			build.target ||
			(build.platforms && build.platforms.length > 0) ||
			(build.buildArgs && Object.keys(build.buildArgs).length > 0) ||
			(build.labels && Object.keys(build.labels).length > 0) ||
			(build.cacheFrom && build.cacheFrom.length > 0) ||
			(build.cacheTo && build.cacheTo.length > 0) ||
			build.network ||
			build.isolation ||
			(build.shmSize && build.shmSize > 0) ||
			(build.ulimits && Object.keys(build.ulimits).length > 0) ||
			(build.entitlements && build.entitlements.length > 0) ||
			build.privileged ||
			(build.extraHosts && build.extraHosts.length > 0) ||
			build.noCache ||
			build.pull
		);
		selectedContextPath = getContextPathFromBuild(build);
		mainTab = 'build';
		rightPanelTab = 'config';
		buildTab = 'configuration';
		buildHistoryDetailsOpen = false;
	}

	async function handleSubmit() {
		const data = form.validate();
		if (!data) return;

		if (!contextDir || contextDir.trim() === '') {
			toast.error(m.build_context_required());
			return;
		}

		resetState();
		// Prefer showing progress immediately once a build begins.
		rightPanelTab = 'output';
		buildTab = 'output';
		isBuilding = true;
		buildStatusText = m.starting_build();
		appendLog(m.using_context({ context: contextDir }));

		const tags = parseTags(data.tags);
		const parsedCacheTo = parseList(data.cacheTo || '');
		const parsedEntitlements = parseList(data.entitlements || '');
		const parsedPlatforms = parsePlatforms(data.platforms || '');
		const parsedExtraHosts = parseList(data.extraHosts || '');
		const parsedUlimits = parseBuildArgs(data.ulimits || '');
		const parsedShmSize = parseOptionalBytes(data.shmSize || '');

		const resolvedProvider = depotAvailable ? data.provider : 'local';
		const push = resolvedProvider === 'depot' ? true : data.push;
		const load = resolvedProvider === 'depot' ? false : data.load;
		const network = data.network?.trim() || undefined;
		const isolation = data.isolation?.trim() || undefined;

		const providerValidationError = validateProviderCompatibility(resolvedProvider, {
			cacheTo: parsedCacheTo,
			entitlements: parsedEntitlements,
			privileged: data.privileged,
			platforms: parsedPlatforms,
			network,
			isolation,
			shmSize: parsedShmSize,
			ulimits: parsedUlimits,
			extraHosts: parsedExtraHosts
		});
		if (providerValidationError) {
			toast.error(providerValidationError);
			isBuilding = false;
			return;
		}

		const payload = {
			contextDir: contextDir.trim(),
			dockerfile: data.dockerfile?.trim() || undefined,
			tags,
			target: data.target?.trim() || undefined,
			buildArgs: parseBuildArgs(data.buildArgs || ''),
			labels: parseBuildArgs(data.labels || ''),
			cacheFrom: parseList(data.cacheFrom || ''),
			cacheTo: parsedCacheTo,
			noCache: data.noCache,
			pull: data.pull,
			network,
			isolation,
			shmSize: parsedShmSize,
			ulimits: parsedUlimits,
			entitlements: parsedEntitlements,
			privileged: data.privileged,
			extraHosts: parsedExtraHosts,
			platforms: parsedPlatforms,
			provider: resolvedProvider,
			push,
			load
		};

		try {
			const resolvedEnvId = await environmentStore.getCurrentEnvironmentId();
			await buildMutation.mutateAsync({ envId: resolvedEnvId, ...payload });
			toast.success(m.build_completed());
		} catch (error: any) {
			const message = sanitizeLogText(String(error?.message || m.build_failed()));
			buildError = message;
			buildStatusText = message.toLowerCase().startsWith(m.build_failed().toLowerCase())
				? message
				: m.build_failed_with_error({ error: message });
			appendLog(m.build_error_log({ error: message }));
			toast.error(message);
		} finally {
			isBuilding = false;
		}
	}

	function onBuildTabChange(value: string) {
		buildTab = value;
	}

	function onMainTabChange(value: string) {
		mainTab = value as 'build' | 'history';
	}
</script>

{#snippet BuildHistoryStatusCell({ value }: { value: unknown })}
	<StatusBadge
		variant={getStatusBadgeVariant(value as ImageBuildStatus)}
		text={buildHistoryStatusLabel(value as ImageBuildStatus)}
		size="sm"
	/>
{/snippet}

{#snippet BuildHistoryTagsCell({ item }: { item: ImageBuildRecord })}
	<div class="space-y-1">
		<div class="text-sm font-medium">{getBuildTitle(item)}</div>
		<div class="text-muted-foreground truncate text-xs">{item.contextDir}</div>
	</div>
{/snippet}

{#snippet BuildHistoryProviderCell({ value }: { value: unknown })}
	<span class="text-sm">{String(value ?? '-') || '-'}</span>
{/snippet}

{#snippet BuildHistoryTimeCell({ value }: { value: unknown })}
	<span class="text-sm">{formatTimestamp(String(value ?? ''))}</span>
{/snippet}

{#snippet BuildHistoryDurationCell({ value }: { value: unknown })}
	<span class="text-sm">{formatDuration(Number(value ?? 0))}</span>
{/snippet}

{#snippet BuildHistoryRowActions({ item }: { item: ImageBuildRecord })}
	<DropdownMenu.Root>
		<DropdownMenu.Trigger data-row-select-ignore>
			<ArcaneButton action="inspect" tone="ghost" size="icon" showLabel={false} icon={EllipsisIcon} />
		</DropdownMenu.Trigger>
		<DropdownMenu.Content align="end">
			<DropdownMenu.Item onclick={() => openBuildDetails(item)}>
				<InfoIcon class="size-4" />
				{m.common_view_details()}
			</DropdownMenu.Item>
		</DropdownMenu.Content>
	</DropdownMenu.Root>
{/snippet}

{#snippet BuildHistoryMobileCard({
	item,
	mobileFieldVisibility
}: {
	item: ImageBuildRecord;
	mobileFieldVisibility: MobileFieldVisibility;
})}
	<UniversalMobileCard
		{item}
		icon={(item: ImageBuildRecord) => ({
			component: TerminalIcon,
			variant:
				item.status === 'success' ? 'emerald' : item.status === 'failed' ? 'red' : item.status === 'running' ? 'blue' : 'gray'
		})}
		title={(item: ImageBuildRecord) => getBuildTitle(item)}
		subtitle={(item: ImageBuildRecord) => ((mobileFieldVisibility.context ?? true) ? item.contextDir : null)}
		badges={[
			(item: ImageBuildRecord) => ({
				variant: getStatusBadgeVariant(item.status),
				text: buildHistoryStatusLabel(item.status)
			})
		]}
		fields={[
			{
				label: m.common_tags(),
				getValue: (item: ImageBuildRecord) => item.tags?.join(', ') || '-',
				icon: TagIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility.tags ?? true
			},
			{
				label: m.build_provider(),
				getValue: (item: ImageBuildRecord) => item.provider || '-',
				icon: SettingsIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility.provider ?? true
			},
			{
				label: m.common_created(),
				getValue: (item: ImageBuildRecord) => formatTimestamp(item.createdAt),
				icon: ClockIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility.createdAt ?? true
			},
			{
				label: m.build_duration_label(),
				getValue: (item: ImageBuildRecord) => formatDuration(item.durationMs ?? 0),
				icon: ArrowDownIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility.durationMs ?? false
			}
		]}
		onclick={(item: ImageBuildRecord) => openBuildDetails(item)}
	/>
{/snippet}

{#snippet historyContent()}
	<div class="flex h-full flex-col p-6">
		<ArcaneTable
			persistKey="arcane-build-history-table"
			items={buildHistoryItems}
			bind:requestOptions={buildHistoryRequestOptions}
			bind:selectedIds={buildHistorySelectedIds}
			bind:mobileFieldVisibility={buildHistoryMobileFieldVisibility}
			onRefresh={loadBuildHistory}
			selectionDisabled={true}
			columns={buildHistoryColumns}
			mobileFields={buildHistoryMobileFields}
			rowActions={BuildHistoryRowActions}
			mobileCard={BuildHistoryMobileCard}
		/>

		<ResponsiveDialog
			bind:open={buildHistoryDetailsOpen}
			title={buildHistorySelected ? (buildHistorySelected.tags?.[0] ?? m.build_output()) : m.build_output()}
			description={buildHistorySelected ? buildHistorySelected.contextDir : undefined}
			contentClass="sm:max-w-[1100px]"
			class="min-h-0 lg:overflow-hidden"
		>
			<div class="space-y-4 pb-4">
				{#if buildHistorySelected}
					<div class="flex flex-wrap items-center justify-between gap-3 text-sm">
						<div class="flex flex-wrap items-center gap-2">
							<StatusBadge
								variant={getStatusBadgeVariant(buildHistorySelected.status)}
								text={buildHistoryStatusLabel(buildHistorySelected.status)}
								size="sm"
							/>
							{#if buildHistorySelected.provider}
								<span class="text-muted-foreground">{buildHistorySelected.provider}</span>
							{/if}
							{#if buildHistorySelected.durationMs}
								<span class="text-muted-foreground">{formatDuration(buildHistorySelected.durationMs)}</span>
							{/if}
							<span class="text-muted-foreground">{formatTimestamp(buildHistorySelected.createdAt)}</span>
						</div>
						<ArcaneButton
							action="base"
							tone="outline"
							size="sm"
							icon={RedeployIcon}
							customLabel={m.build_rebuild()}
							onclick={() => buildHistorySelected && applyBuildConfig(buildHistorySelected)}
						/>
					</div>
					{#if buildHistorySelected.errorMessage}
						<div class="border-destructive/20 bg-destructive/10 text-destructive rounded-lg border p-3 text-sm">
							{buildHistorySelected.errorMessage}
						</div>
					{/if}
				{/if}

				<div class="grid gap-4 lg:h-[70vh] lg:grid-cols-[360px_minmax(0,1fr)] lg:items-stretch">
					<div class="min-h-0 space-y-3 lg:overflow-auto lg:overscroll-contain lg:pr-1">
						{#if buildHistorySelected}
							<div class="grid gap-3">
								<div class="border-border/60 rounded-lg border bg-zinc-950/40 p-3">
									<div class="text-muted-foreground text-[10px] font-semibold tracking-[0.12em] uppercase">
										{m.build_context()}
									</div>
									<div class="mt-2 font-mono text-xs break-all">{buildHistorySelected.contextDir}</div>
								</div>
								<div class="border-border/60 rounded-lg border bg-zinc-950/40 p-3">
									<div class="text-muted-foreground text-[10px] font-semibold tracking-[0.12em] uppercase">
										{m.common_tags()}
									</div>
									<div class="mt-2 text-xs">
										{buildHistorySelected.tags?.join(', ') || '-'}
									</div>
								</div>
								<div class="border-border/60 rounded-lg border bg-zinc-950/40 p-3">
									<div class="text-muted-foreground text-[10px] font-semibold tracking-[0.12em] uppercase">
										{m.dockerfile()}
									</div>
									<div class="mt-2 text-xs">{buildHistorySelected.dockerfile || '-'}</div>
								</div>
								<div class="border-border/60 rounded-lg border bg-zinc-950/40 p-3">
									<div class="text-muted-foreground text-[10px] font-semibold tracking-[0.12em] uppercase">
										{m.target_label()}
									</div>
									<div class="mt-2 text-xs">{buildHistorySelected.target || '-'}</div>
								</div>
								<div class="border-border/60 rounded-lg border bg-zinc-950/40 p-3">
									<div class="text-muted-foreground text-[10px] font-semibold tracking-[0.12em] uppercase">
										{m.platforms_label()}
									</div>
									<div class="mt-2 text-xs">{buildHistorySelected.platforms?.join(', ') || '-'}</div>
								</div>
								<div class="border-border/60 rounded-lg border bg-zinc-950/40 p-3">
									<div class="text-muted-foreground text-[10px] font-semibold tracking-[0.12em] uppercase">
										{m.build_provider()}
									</div>
									<div class="mt-2 text-xs">{buildHistorySelected.provider || '-'}</div>
								</div>
								<div class="border-border/60 rounded-lg border bg-zinc-950/40 p-3">
									<div class="text-muted-foreground text-[10px] font-semibold tracking-[0.12em] uppercase">
										{m.push()} / {m.load()}
									</div>
									<div class="mt-2 text-xs">
										{buildHistorySelected.push ? m.common_yes() : m.common_no()} / {buildHistorySelected.load
											? m.common_yes()
											: m.common_no()}
									</div>
								</div>
								<div class="border-border/60 rounded-lg border bg-zinc-950/40 p-3">
									<div class="text-muted-foreground text-[10px] font-semibold tracking-[0.12em] uppercase">
										{m.build_no_cache_pull_base_label()}
									</div>
									<div class="mt-2 text-xs">
										{buildHistorySelected.noCache ? m.common_yes() : m.common_no()} / {buildHistorySelected.pull
											? m.common_yes()
											: m.common_no()}
									</div>
								</div>
								<div class="border-border/60 rounded-lg border bg-zinc-950/40 p-3">
									<div class="text-muted-foreground text-[10px] font-semibold tracking-[0.12em] uppercase">
										{m.build_network_label()}
									</div>
									<div class="mt-2 text-xs">{buildHistorySelected.network || '-'}</div>
								</div>
								<div class="border-border/60 rounded-lg border bg-zinc-950/40 p-3">
									<div class="text-muted-foreground text-[10px] font-semibold tracking-[0.12em] uppercase">
										{m.build_args()}
									</div>
									{#if buildHistorySelected.buildArgs && Object.keys(buildHistorySelected.buildArgs).length > 0}
										<pre class="mt-2 font-mono text-[11px] break-words whitespace-pre-wrap">
												{formatBuildArgs(buildHistorySelected.buildArgs)}
											</pre>
									{:else}
										<div class="mt-2 text-xs">-</div>
									{/if}
								</div>
								<div class="border-border/60 rounded-lg border bg-zinc-950/40 p-3">
									<div class="text-muted-foreground text-[10px] font-semibold tracking-[0.12em] uppercase">
										{m.common_labels()}
									</div>
									{#if buildHistorySelected.labels && Object.keys(buildHistorySelected.labels).length > 0}
										<pre class="mt-2 font-mono text-[11px] break-words whitespace-pre-wrap">
											{formatKeyValueMap(buildHistorySelected.labels)}
										</pre>
									{:else}
										<div class="mt-2 text-xs">-</div>
									{/if}
								</div>
								<div class="border-border/60 rounded-lg border bg-zinc-950/40 p-3">
									<div class="text-muted-foreground text-[10px] font-semibold tracking-[0.12em] uppercase">
										{m.build_cache_from_label()}
									</div>
									<div class="mt-2 text-xs">{buildHistorySelected.cacheFrom?.join(', ') || '-'}</div>
								</div>
								<div class="border-border/60 rounded-lg border bg-zinc-950/40 p-3">
									<div class="text-muted-foreground text-[10px] font-semibold tracking-[0.12em] uppercase">
										{m.build_cache_to_label()}
									</div>
									<div class="mt-2 text-xs">{buildHistorySelected.cacheTo?.join(', ') || '-'}</div>
								</div>
								<div class="border-border/60 rounded-lg border bg-zinc-950/40 p-3">
									<div class="text-muted-foreground text-[10px] font-semibold tracking-[0.12em] uppercase">
										{m.build_isolation_label()}
									</div>
									<div class="mt-2 text-xs">{buildHistorySelected.isolation || '-'}</div>
								</div>
								<div class="border-border/60 rounded-lg border bg-zinc-950/40 p-3">
									<div class="text-muted-foreground text-[10px] font-semibold tracking-[0.12em] uppercase">
										{m.build_shm_size_short_label()}
									</div>
									<div class="mt-2 text-xs">
										{buildHistorySelected.shmSize ? m.build_bytes({ size: String(buildHistorySelected.shmSize) }) : '-'}
									</div>
								</div>
								<div class="border-border/60 rounded-lg border bg-zinc-950/40 p-3">
									<div class="text-muted-foreground text-[10px] font-semibold tracking-[0.12em] uppercase">
										{m.build_ulimits_label()}
									</div>
									{#if buildHistorySelected.ulimits && Object.keys(buildHistorySelected.ulimits).length > 0}
										<pre class="mt-2 font-mono text-[11px] break-words whitespace-pre-wrap">
											{formatKeyValueMap(buildHistorySelected.ulimits)}
										</pre>
									{:else}
										<div class="mt-2 text-xs">-</div>
									{/if}
								</div>
								<div class="border-border/60 rounded-lg border bg-zinc-950/40 p-3">
									<div class="text-muted-foreground text-[10px] font-semibold tracking-[0.12em] uppercase">
										{m.build_entitlements_label()}
									</div>
									<div class="mt-2 text-xs">{buildHistorySelected.entitlements?.join(', ') || '-'}</div>
								</div>
								<div class="border-border/60 rounded-lg border bg-zinc-950/40 p-3">
									<div class="text-muted-foreground text-[10px] font-semibold tracking-[0.12em] uppercase">
										{m.build_privileged_heading_label()}
									</div>
									<div class="mt-2 text-xs">{buildHistorySelected.privileged ? m.common_yes() : m.common_no()}</div>
								</div>
								<div class="border-border/60 rounded-lg border bg-zinc-950/40 p-3">
									<div class="text-muted-foreground text-[10px] font-semibold tracking-[0.12em] uppercase">
										{m.build_extra_hosts_label()}
									</div>
									<div class="mt-2 text-xs">{buildHistorySelected.extraHosts?.join(', ') || '-'}</div>
								</div>
							</div>
						{/if}
					</div>
					<div class="border-border/60 bg-card/30 flex min-h-0 flex-col overflow-hidden rounded-xl border">
						<div class="border-border/60 flex items-center justify-between border-b px-4 py-3">
							<div class="text-sm font-medium">{m.build_output()}</div>
							{#if buildHistorySelected?.outputTruncated}
								<span class="text-xs text-amber-400">{m.build_output_truncated()}</span>
							{/if}
						</div>
						<div class="max-h-[60vh] min-h-[260px] overflow-auto overscroll-contain p-4 lg:max-h-none lg:min-h-0 lg:flex-1">
							{#if buildHistoryDetailsLoading}
								<div class="flex h-full items-center justify-center">
									<Spinner class="text-muted-foreground size-6" />
								</div>
							{:else if buildHistorySelected?.output}
								{#if buildHistoryOutputEntries.length > 0}
									<div class="space-y-2">
										{#each buildHistoryOutputEntries as entry, entryIndex (entry.raw + entryIndex)}
											<div
												class={`border-border/50 rounded-lg border px-3 py-2 ${
													entry.error ? 'bg-destructive/10 border-destructive/40' : 'bg-zinc-950/40'
												}`}
											>
												<div class="flex items-start justify-between gap-3">
													<div class="min-w-0">
														<div
															class="text-muted-foreground flex flex-wrap items-center gap-2 text-[10px] tracking-wide uppercase"
														>
															{#if entry.type}
																<span class="rounded bg-zinc-800/60 px-1.5 py-0.5">{entry.type}</span>
															{/if}
															{#if entry.phase}
																<span class="rounded bg-zinc-800/60 px-1.5 py-0.5">{entry.phase}</span>
															{/if}
															{#if entry.id}
																<span class="font-mono text-[10px] break-all">{entry.id}</span>
															{/if}
														</div>
														<div class={`mt-1 text-xs break-all ${entry.error ? 'text-destructive' : 'text-foreground'}`}>
															{entry.error ?? entry.status ?? entry.raw}
														</div>
													</div>
													{#if entry.progress?.total}
														<div class="flex shrink-0 flex-col items-end gap-1">
															<span class="text-muted-foreground text-[10px] tabular-nums">
																{formatProgress(entry.progress.current, entry.progress.total)}
															</span>
															<div class="h-1 w-24 overflow-hidden rounded-full bg-zinc-800/70">
																<div
																	class="h-full rounded-full bg-emerald-400"
																	style={`width: ${getProgressPercent(entry.progress.current, entry.progress.total)}%`}
																></div>
															</div>
														</div>
													{/if}
												</div>
											</div>
										{/each}
									</div>
								{:else}
									<div class="text-muted-foreground text-sm">{m.build_output_placeholder()}</div>
								{/if}
							{:else}
								<div class="text-muted-foreground text-sm">{m.build_output_placeholder()}</div>
							{/if}
						</div>
					</div>
				</div>
			</div>
		</ResponsiveDialog>
	</div>
{/snippet}

<!-- Right panel with tabs (config + output) -->
{#snippet rightPanel()}
	<div class="flex h-full flex-col">
		<Tabs.Root bind:value={rightPanelTab} class="flex h-full flex-col">
			<!-- Tabs header with refined styling -->
			<div class="flex shrink-0 items-center justify-between border-b border-zinc-800/50 bg-zinc-950/40 px-3 py-2">
				<Tabs.List class="flex items-center gap-1 rounded-lg border border-zinc-800/60 bg-zinc-950/60 p-1">
					<Tabs.Trigger
						value="config"
						class="data-[state=active]:text-foreground text-muted-foreground hover:text-foreground rounded-md px-3 py-1.5 text-xs font-medium transition-colors data-[state=active]:bg-blue-500/10"
					>
						<CodeIcon class="mr-2 size-3.5" />
						{m.build_configuration()}
					</Tabs.Trigger>
					<Tabs.Trigger
						value="output"
						class="data-[state=active]:text-foreground text-muted-foreground hover:text-foreground rounded-md px-3 py-1.5 text-xs font-medium transition-colors data-[state=active]:bg-emerald-500/10"
					>
						<TerminalIcon class="mr-2 size-3.5" />
						{m.build_output()}
						{#if logLines.length > 0}
							<span
								class="ml-1.5 rounded-full bg-emerald-500/15 px-1.5 py-0.5 text-[10px] font-semibold text-emerald-300 ring-1 ring-emerald-400/20"
							>
								{logLines.length}
							</span>
						{/if}
					</Tabs.Trigger>
				</Tabs.List>

				<div class="flex items-center gap-3 pr-2">
					<BuildControls {inputs} {providerOptions} {selectedProviderLabel} {isBuilding} onBuild={handleSubmit} />
					<div class="bg-border hidden h-4 w-px xl:block"></div>
					<div class="flex items-center gap-2">
						<div class="relative flex items-center">
							<div
								class={`size-2 rounded-full transition-all ${
									buildError
										? 'bg-red-500 shadow-lg shadow-red-500/50'
										: hasReachedComplete
											? 'bg-green-500 shadow-lg shadow-green-500/50'
											: isBuilding
												? 'animate-pulse bg-blue-500 shadow-lg shadow-blue-500/50'
												: 'bg-zinc-600'
								}`}
							></div>
							{#if isBuilding}
								<div class="absolute inset-0 size-2 animate-ping rounded-full bg-blue-500 opacity-75"></div>
							{/if}
						</div>
						<span class="text-muted-foreground text-xs font-medium">{statusLabel}</span>
					</div>
				</div>
			</div>

			<Tabs.Content value="config" class="mt-0 min-h-0 flex-1 overflow-auto">
				<BuildConfigPanel {inputs} provider={$inputs.provider.value} bind:showAdvanced onSubmit={handleSubmit} />
			</Tabs.Content>
			<Tabs.Content value="output" class="mt-0 min-h-0 flex-1 overflow-hidden">
				<BuildOutputPanel
					{logLines}
					{layerStats}
					{aggregateStatus}
					{progressValue}
					{isIndeterminate}
					{hasReachedComplete}
					{buildError}
					{isBuilding}
					bind:autoScroll
					onReset={resetState}
				/>
			</Tabs.Content>
		</Tabs.Root>
	</div>
{/snippet}

{#if isDesktop}
	<ResourceDetailLayout
		backUrl="/images"
		backLabel={m.images_title()}
		title={m.manual_build_workspace()}
		subtitle={m.manual_build_workspace_subtitle()}
	>
		<Tabs.Root bind:value={mainTab} class="flex h-[calc(100vh-12rem)] flex-col">
			<Tabs.List class="mb-3 flex w-fit gap-2 rounded-lg border border-zinc-800/60 bg-zinc-950/60 p-1">
				<Tabs.Trigger
					value="build"
					class="data-[state=active]:text-foreground text-muted-foreground hover:text-foreground rounded-md px-3 py-1.5 text-sm font-medium data-[state=active]:bg-blue-500/10"
				>
					{m.manual_build_workspace()}
				</Tabs.Trigger>
				<Tabs.Trigger
					value="history"
					class="data-[state=active]:text-foreground text-muted-foreground hover:text-foreground rounded-md px-3 py-1.5 text-sm font-medium data-[state=active]:bg-amber-500/10"
				>
					{m.build_history()}
				</Tabs.Trigger>
			</Tabs.List>

			<Tabs.Content value="build" class="min-h-0 flex-1">
				<div class="relative flex h-full">
					<ResizableSplit
						class="flex h-full w-full gap-3"
						firstClass="h-full"
						secondClass="h-full"
						minSize={300}
						minSecondSize={520}
						defaultRatio={0.28}
						handleSize={10}
						handleClass="bg-zinc-950/50 rounded-full"
						allowCollapse={true}
						persistKey="arcane.build.workspace.split"
					>
						{#snippet first()}
							<Card.Root class="flex h-full flex-col overflow-hidden">
								<BuildWorkspacePanel
									rootLabel={buildsRootLabel}
									rootPath={buildsRoot}
									{contextDir}
									onSelectContext={(path: string) => (selectedContextPath = path)}
								/>
							</Card.Root>
						{/snippet}
						{#snippet second()}
							<Card.Root class="flex h-full flex-col overflow-hidden">
								{@render rightPanel()}
							</Card.Root>
						{/snippet}
					</ResizableSplit>
				</div>
			</Tabs.Content>

			<Tabs.Content value="history" class="min-h-0 flex-1">
				<Card.Root class="flex h-full flex-col overflow-hidden">
					{@render historyContent()}
				</Card.Root>
			</Tabs.Content>
		</Tabs.Root>
	</ResourceDetailLayout>
{:else}
	<TabbedPageLayout
		backUrl="/images"
		backLabel={m.images_title()}
		tabItems={mainTabItems}
		selectedTab={mainTab}
		onTabChange={onMainTabChange}
		class="min-h-[calc(100vh-10rem)]"
	>
		{#snippet headerInfo()}
			<div class="flex flex-col gap-1">
				{#if mainTab === 'history'}
					<h1 class="text-2xl font-bold tracking-tight">{m.builds()}</h1>
					<p class="text-muted-foreground text-sm">{m.build_output()}</p>
				{:else}
					<h1 class="text-2xl font-bold tracking-tight">{m.manual_build_workspace()}</h1>
					<p class="text-muted-foreground text-sm">{m.manual_build_workspace_subtitle()}</p>
				{/if}
			</div>
		{/snippet}

		{#snippet headerActions()}
			{#if mainTab === 'build'}
				<BuildControls {inputs} {providerOptions} {selectedProviderLabel} {isBuilding} onBuild={handleSubmit} />
			{/if}
		{/snippet}

		{#snippet tabContent(tab)}
			<div class="min-h-[60vh]">
				{#if tab === 'build'}
					<TabbedPageLayout
						backUrl="/images"
						backLabel={m.images_title()}
						tabItems={buildMobileTabItems}
						selectedTab={buildTab}
						onTabChange={onBuildTabChange}
						class="min-h-[60vh]"
					>
						{#snippet headerInfo()}
							<div class="flex flex-col gap-1">
								<h2 class="text-lg font-semibold">{m.manual_build_workspace()}</h2>
								<p class="text-muted-foreground text-xs">{m.manual_build_workspace_subtitle()}</p>
							</div>
						{/snippet}
						{#snippet headerActions()}
							<BuildControls {inputs} {providerOptions} {selectedProviderLabel} {isBuilding} onBuild={handleSubmit} />
						{/snippet}
						{#snippet tabContent(buildTabValue)}
							{#if buildTabValue === 'workspace'}
								<Card.Root class="flex h-full flex-col overflow-hidden">
									<BuildWorkspacePanel
										rootLabel={buildsRootLabel}
										rootPath={buildsRoot}
										{contextDir}
										onSelectContext={(path: string) => (selectedContextPath = path)}
									/>
								</Card.Root>
							{:else if buildTabValue === 'configuration'}
								<Card.Root class="overflow-hidden">
									<BuildConfigPanel {inputs} provider={$inputs.provider.value} bind:showAdvanced onSubmit={handleSubmit} />
								</Card.Root>
							{:else}
								<Card.Root class="flex h-full min-h-[500px] flex-col overflow-hidden">
									<BuildOutputPanel
										{logLines}
										{layerStats}
										{aggregateStatus}
										{progressValue}
										{isIndeterminate}
										{hasReachedComplete}
										{buildError}
										{isBuilding}
										bind:autoScroll
										onReset={resetState}
									/>
								</Card.Root>
							{/if}
						{/snippet}
					</TabbedPageLayout>
				{:else}
					<Card.Root class="flex h-full min-h-[500px] flex-col overflow-hidden">
						{@render historyContent()}
					</Card.Root>
				{/if}
			</div>
		{/snippet}
	</TabbedPageLayout>
{/if}
