<script lang="ts">
	// Dozzle reference: the compact structured-log summary and expandable details pattern
	// here were informed by amir20/dozzle's ComplexLogItem.vue and LogDetails.vue.
	import * as Collapsible from '$lib/components/ui/collapsible';
	import { ArrowDownIcon, ArrowRightIcon, CheckIcon, CopyIcon } from '$lib/icons';

	interface Props {
		data: Record<string, unknown>;
		rawJson?: string;
	}

	type SummaryField = {
		path: string;
		label: string;
		value: unknown;
	};

	let { data, rawJson }: Props = $props();

	let showDetails = $state(false);
	let copied = $state(false);

	const SUMMARY_LIMIT = 6;

	// Common field names used by various structured logging frameworks
	const LEVEL_KEYS = ['level', 'severity', 'log.level', 'loglevel', 'lvl', 'levelname', 'priority'];
	const MESSAGE_KEYS = ['message', 'msg', 'text', 'body', 'event', 'log', 'content'];
	const TIMESTAMP_KEYS = ['timestamp', 'time', 'ts', '@timestamp', 'datetime', 'date', 'created', 'time_local'];
	const TARGET_KEYS = ['target', 'logger', 'name', 'source', 'component', 'module', 'log.logger', 'logger_name', 'category'];
	const ERROR_KEYS = ['error', 'err', 'exception', 'stack', 'stacktrace', 'error.message'];
	const TRACE_KEYS = ['trace_id', 'traceId', 'correlation_id', 'correlationId', 'request_id', 'requestId', 'spanId', 'span_id'];
	const NESTED_MESSAGE_PATHS = [
		['fields', 'message'],
		['fields', 'msg'],
		['data', 'message']
	];
	const HIDDEN_SUMMARY_KEYS = new Set([...LEVEL_KEYS, ...MESSAGE_KEYS, ...TIMESTAMP_KEYS]);

	function findValue(obj: Record<string, unknown>, keys: string[]): unknown {
		for (const key of keys) {
			if (key in obj) return obj[key];
			if (key.includes('.')) {
				const parts = key.split('.');
				let current: unknown = obj;
				for (const part of parts) {
					if (current && typeof current === 'object' && part in (current as Record<string, unknown>)) {
						current = (current as Record<string, unknown>)[part];
					} else {
						current = undefined;
						break;
					}
				}
				if (current !== undefined) return current;
			}
		}
		return undefined;
	}

	function findNestedMessage(obj: Record<string, unknown>): string | undefined {
		for (const path of NESTED_MESSAGE_PATHS) {
			let current: unknown = obj;
			for (const key of path) {
				if (current && typeof current === 'object' && key in (current as Record<string, unknown>)) {
					current = (current as Record<string, unknown>)[key];
				} else {
					current = undefined;
					break;
				}
			}
			if (typeof current === 'string') return current;
		}
		return undefined;
	}

	function normalizeLevelValue(level: unknown): string | undefined {
		if (typeof level === 'string') return level;
		if (typeof level === 'number') {
			const syslogMap: Record<number, string> = {
				0: 'fatal',
				1: 'fatal',
				2: 'fatal',
				3: 'error',
				4: 'warn',
				5: 'info',
				6: 'info',
				7: 'debug'
			};
			return syslogMap[level] ?? 'info';
		}
		return undefined;
	}

	function extractFields(obj: Record<string, unknown>): {
		level: string | undefined;
		message: string | undefined;
		target: string | undefined;
		error: unknown;
		traceId: string | undefined;
		hasStructure: boolean;
	} {
		const rawLevel = findValue(obj, LEVEL_KEYS);
		let message = findValue(obj, MESSAGE_KEYS);
		if (!message) {
			message = findNestedMessage(obj);
		}
		if (!message && obj['event']) {
			message = String(obj['event']);
		}

		const target = findValue(obj, TARGET_KEYS);
		const error = findValue(obj, ERROR_KEYS);
		const traceId = findValue(obj, TRACE_KEYS);

		return {
			level: normalizeLevelValue(rawLevel),
			message: typeof message === 'string' ? message : undefined,
			target: typeof target === 'string' ? target : undefined,
			error,
			traceId: typeof traceId === 'string' ? traceId : undefined,
			hasStructure: !!(rawLevel || message || target)
		};
	}

	function getLevelDotClass(level: string | undefined): string {
		if (!level) return 'bg-zinc-500';
		const normalizedLevel = level.toLowerCase();
		if (
			normalizedLevel === 'error' ||
			normalizedLevel === 'err' ||
			normalizedLevel === 'fatal' ||
			normalizedLevel === 'critical' ||
			normalizedLevel === 'panic' ||
			normalizedLevel === 'emergency'
		) {
			return 'bg-red-400';
		}
		if (normalizedLevel === 'warn' || normalizedLevel === 'warning' || normalizedLevel === 'alert') {
			return 'bg-orange-400';
		}
		if (normalizedLevel === 'info' || normalizedLevel === 'information' || normalizedLevel === 'notice') {
			return 'bg-emerald-400';
		}
		if (normalizedLevel === 'debug' || normalizedLevel === 'dbg') {
			return 'bg-sky-400';
		}
		if (normalizedLevel === 'trace' || normalizedLevel === 'verbose' || normalizedLevel === 'finest') {
			return 'bg-violet-400';
		}
		return 'bg-zinc-500';
	}

	function safeStringify(value: unknown, indent = 0): string {
		if (typeof value === 'string') {
			return value;
		}
		if (value === null) {
			return 'null';
		}
		if (value === undefined) {
			return '';
		}
		try {
			return JSON.stringify(value, null, indent);
		} catch {
			return String(value);
		}
	}

	function formatInlineValue(value: unknown): string {
		if (value === null) {
			return '<null>';
		}
		if (typeof value === 'string') {
			return value;
		}
		if (typeof value === 'number' || typeof value === 'boolean') {
			return String(value);
		}

		const compact = safeStringify(value);
		if (compact.length <= 72) {
			return compact;
		}

		return `${compact.slice(0, 69)}...`;
	}

	function buildSummaryFields(obj: Record<string, unknown>): SummaryField[] {
		const fields: SummaryField[] = [];

		for (const [key, value] of Object.entries(obj)) {
			if (value === undefined || HIDDEN_SUMMARY_KEYS.has(key)) {
				continue;
			}

			if ((key === 'fields' || key === 'data') && value && typeof value === 'object' && !Array.isArray(value)) {
				for (const [nestedKey, nestedValue] of Object.entries(value as Record<string, unknown>)) {
					if (nestedValue === undefined || MESSAGE_KEYS.includes(nestedKey)) {
						continue;
					}

					fields.push({
						path: `${key}.${nestedKey}`,
						label: nestedKey,
						value: nestedValue
					});
				}
				continue;
			}

			fields.push({
				path: key,
				label: key,
				value
			});
		}

		return fields;
	}

	const extracted = $derived(extractFields(data));
	const summaryFields = $derived(buildSummaryFields(data));
	const visibleSummaryFields = $derived(summaryFields.slice(0, SUMMARY_LIMIT));
	const hiddenSummaryCount = $derived(Math.max(summaryFields.length - visibleSummaryFields.length, 0));
	const jsonString = $derived(rawJson ?? JSON.stringify(data, null, 2));
	const hasDetails = $derived(summaryFields.length > SUMMARY_LIMIT || summaryFields.length > 0 || !!rawJson);

	async function copyToClipboard() {
		try {
			await navigator.clipboard.writeText(jsonString);
			copied = true;
			setTimeout(() => {
				copied = false;
			}, 2000);
		} catch (err) {
			console.error('Failed to copy to clipboard:', err);
		}
	}
</script>

<Collapsible.Root bind:open={showDetails}>
	<div class="flex min-w-0 items-start gap-2">
		{#if hasDetails}
			<Collapsible.Trigger
				class="focus-visible:border-ring focus-visible:ring-ring/50 flex min-w-0 flex-1 items-start gap-3 rounded-md border border-transparent px-1 py-0.5 text-left outline-none hover:bg-white/3 focus-visible:ring-[3px]"
			>
				{#if extracted.level}
					<span class="mt-1.5 size-2.5 shrink-0 rounded-full {getLevelDotClass(extracted.level)}"></span>
				{/if}

				<div class="min-w-0 flex-1">
					<div class="flex flex-wrap items-center gap-x-4 gap-y-1">
						{#if extracted.target}
							<span class="truncate text-[11px] font-medium text-cyan-400" title={extracted.target}>
								{extracted.target}
							</span>
						{/if}

						{#if extracted.message}
							<span class="min-w-0 text-gray-100">{extracted.message}</span>
						{:else if !extracted.hasStructure}
							<span class="min-w-0 text-gray-300">{safeStringify(data)}</span>
						{/if}

						{#each visibleSummaryFields as field (field.path)}
							<span class="inline-flex max-w-full min-w-0 items-baseline gap-0.5 text-gray-300">
								<span class="shrink-0 text-zinc-500">{field.label}=</span>
								<span class="truncate font-semibold text-zinc-100" title={safeStringify(field.value)}>
									{formatInlineValue(field.value)}
								</span>
							</span>
						{/each}

						{#if hiddenSummaryCount > 0}
							<span class="text-[11px] font-medium text-zinc-500">+{hiddenSummaryCount}</span>
						{/if}
					</div>
				</div>

				{#if showDetails}
					<ArrowDownIcon class="mt-0.5 size-4 shrink-0 text-zinc-500" />
				{:else}
					<ArrowRightIcon class="mt-0.5 size-4 shrink-0 text-zinc-500" />
				{/if}
			</Collapsible.Trigger>
		{:else}
			<div class="flex min-w-0 flex-1 items-start gap-3 px-1 py-0.5">
				{#if extracted.level}
					<span class="mt-1.5 size-2.5 shrink-0 rounded-full {getLevelDotClass(extracted.level)}"></span>
				{/if}

				<div class="min-w-0 flex-1">
					<div class="flex flex-wrap items-center gap-x-4 gap-y-1">
						{#if extracted.target}
							<span class="truncate text-[11px] font-medium text-cyan-400" title={extracted.target}>
								{extracted.target}
							</span>
						{/if}

						{#if extracted.message}
							<span class="min-w-0 text-gray-100">{extracted.message}</span>
						{:else if !extracted.hasStructure}
							<span class="min-w-0 text-gray-300">{safeStringify(data)}</span>
						{/if}

						{#each visibleSummaryFields as field (field.path)}
							<span class="inline-flex max-w-full min-w-0 items-baseline gap-0.5 text-gray-300">
								<span class="shrink-0 text-zinc-500">{field.label}=</span>
								<span class="truncate font-semibold text-zinc-100" title={safeStringify(field.value)}>
									{formatInlineValue(field.value)}
								</span>
							</span>
						{/each}
					</div>
				</div>
			</div>
		{/if}

		<button
			type="button"
			onclick={copyToClipboard}
			class="shrink-0 rounded p-1 text-zinc-500 transition-colors hover:bg-white/5 hover:text-zinc-100"
			title={copied ? 'Copied' : 'Copy JSON'}
		>
			{#if copied}
				<CheckIcon class="size-3.5 text-green-400" />
			{:else}
				<CopyIcon class="size-3.5" />
			{/if}
		</button>
	</div>

	{#if hasDetails}
		<Collapsible.Content>
			<div class="mt-2 rounded-md border border-zinc-800 bg-zinc-950/75 p-3">
				{#if summaryFields.length > 0}
					<div class="grid gap-x-4 gap-y-2 md:grid-cols-[minmax(140px,180px)_1fr]">
						{#each summaryFields as field (field.path)}
							<div class="font-mono text-[11px] text-zinc-500">{field.path}</div>
							<pre class="overflow-x-auto text-xs break-all whitespace-pre-wrap text-zinc-100">{safeStringify(
									field.value,
									2
								)}</pre>
						{/each}
					</div>
				{/if}

				<div class="mt-3 border-t border-zinc-800 pt-3">
					<pre class="overflow-x-auto text-xs break-all whitespace-pre-wrap text-zinc-300">{jsonString}</pre>
				</div>
			</div>
		</Collapsible.Content>
	{/if}
</Collapsible.Root>
