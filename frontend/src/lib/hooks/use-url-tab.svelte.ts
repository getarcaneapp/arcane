import { replaceState } from '$app/navigation';
import { page } from '$app/state';
import { onMount, untrack } from 'svelte';

type UseUrlTabOptions<T extends string> = {
	validTabs: () => readonly T[];
	defaultTab: () => T;
	ready?: () => boolean;
};

export function useUrlTab<T extends string>({ validTabs, defaultTab, ready = () => true }: UseUrlTabOptions<T>) {
	function currentUrl() {
		const reactiveUrl = page.url;
		return typeof window === 'undefined' ? new URL(reactiveUrl.href) : new URL(window.location.href);
	}

	function resolveTab(url = currentUrl()) {
		const tabs = validTabs();
		const defaultValue = defaultTab();
		const fallback = tabs.includes(defaultValue) ? defaultValue : (tabs[0] ?? defaultValue);
		const requested = url.searchParams.get('tab');

		return requested && tabs.includes(requested as T) ? (requested as T) : fallback;
	}

	let value = $state<T>(resolveTab());
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
			replaceState(url, page.state);
		}

		value = tab as T;
	}

	$effect(() => {
		const url = currentUrl();
		const selected = resolveTab(url);
		if (mounted && ready() && url.searchParams.get('tab') !== selected) {
			url.searchParams.set('tab', selected);
			replaceState(url, page.state);
		}
		if (untrack(() => value) !== selected) value = selected;
	});

	return {
		get value() {
			return value;
		},
		select
	};
}
