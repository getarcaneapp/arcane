import { tryCatch } from '#lib/utils/try-catch.js';
import { goto } from '$app/navigation';
import { page } from '$app/state';
import { onMount, untrack } from 'svelte';

type UseUrlTabOptions<T extends string> = {
	validTabs: () => readonly T[];
	defaultTab: () => T;
	ready?: () => boolean;
	aliases?: () => Readonly<Partial<Record<string, T>>>;
};

export function useUrlTab<T extends string>({
	validTabs,
	defaultTab,
	ready = () => true,
	aliases = () => ({})
}: UseUrlTabOptions<T>) {
	let pendingUrlUpdate = Promise.resolve();

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
		select
	};
}
