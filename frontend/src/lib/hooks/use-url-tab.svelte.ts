import { page } from '$app/state';
import { onMount, untrack } from 'svelte';

import type { TabItem } from '#lib/components/tab-bar/index.js';
import { goto } from '#lib/utils/base-path.js';
import { tryCatch } from '#lib/utils/try-catch.js';

type UrlTabItem<T extends string> = TabItem & { value: T; visible?: boolean };

type UseUrlTabOptions<T extends string> = {
	defaultTab: () => T;
	ready?: () => boolean;
	aliases?: () => Readonly<Partial<Record<string, T>>>;
} & ({ validTabs: () => readonly T[]; tabs?: never } | { tabs: () => readonly UrlTabItem<T>[]; validTabs?: never });

export function useUrlTab<T extends string>({
	validTabs: tabValues,
	tabs,
	defaultTab,
	ready = () => true,
	aliases = () => ({})
}: UseUrlTabOptions<T>) {
	const items = $derived(tabs?.().filter((tab) => tab.visible !== false) ?? []);
	let pendingUrlUpdate = Promise.resolve();

	function validTabs(): readonly T[] {
		return tabValues?.() ?? items.map((tab) => tab.value);
	}

	function currentUrl() {
		return new URL((page.shallow?.url ?? page.url).href);
	}

	function updateUrl(url: URL) {
		const state = page.state;
		pendingUrlUpdate = tryCatch(pendingUrlUpdate)
			.then((result) => {
				if (result.error !== null) {
					return undefined;
				} else {
					return result.data;
				}
			})
			.then(() =>
				goto(url, {
					replace: true,
					shallow: true,
					reset: false,
					state
				})
			);
	}

	function resolveTab(requested: string | null) {
		const tabs = validTabs();
		const defaultValue = defaultTab();
		const fallback = tabs.includes(defaultValue) ? defaultValue : (tabs[0] ?? defaultValue);

		if (!requested) return fallback;
		const aliased = aliases()[requested] ?? requested;
		return tabs.includes(aliased as T) ? (aliased as T) : fallback;
	}

	let value = $derived(resolveTab(currentUrl().searchParams.get('tab')));
	let mounted = $state(false);

	onMount(() => {
		const timeout = window.setTimeout(() => {
			mounted = true;
		});

		return () => window.clearTimeout(timeout);
	});

	function select(tab: string) {
		if (!validTabs().includes(tab as T)) return;

		const url = currentUrl();
		if (url.searchParams.get('tab') !== tab) {
			url.searchParams.set('tab', tab);
			updateUrl(url);
		}

		value = tab as T;
	}

	$effect(() => {
		const url = currentUrl();
		const selected = resolveTab(url.searchParams.get('tab'));
		if (mounted && ready() && url.searchParams.get('tab') !== selected) {
			url.searchParams.set('tab', selected);
			untrack(() => updateUrl(url));
		}
	});

	return {
		get value() {
			return value;
		},
		get items() {
			return items;
		},
		select
	};
}
