/// <reference no-default-lib="true"/>
/// <reference lib="esnext" />
/// <reference lib="webworker" />
/// <reference types="@sveltejs/kit" />

import { base, files, version } from '$service-worker';

const self = globalThis.self as unknown as ServiceWorkerGlobalScope;

const CACHE = `cache-${version}`;

const API_PREFIX = `${base}/api/`;
const ASSETS = files;
const ASSET_PATHS = new Set(ASSETS.map((asset) => new URL(asset, self.location.origin).pathname));

self.addEventListener('install', (event) => {
	console.log('[ServiceWorker] Install');

	async function addFilesToCache() {
		const cache = await caches.open(CACHE);
		console.log('[ServiceWorker] Caching static assets');
		await cache.addAll(ASSETS);
	}

	event.waitUntil(addFilesToCache());

	// Activate this worker immediately rather than waiting for all tabs to close,
	// so the API-bypass fix takes effect on the next navigation.
	self.skipWaiting();
});

self.addEventListener('activate', (event) => {
	console.log('[ServiceWorker] Activate');

	async function deleteOldCaches() {
		const keyList = await caches.keys();
		await Promise.all(
			keyList.map((key) => {
				if (key !== CACHE) {
					console.log('[ServiceWorker] Removing old cache', key);
					return caches.delete(key);
				}
			})
		);
	}

	// Make both the cache cleanup and taking control of already-open tabs part of
	// the activation lifetime. Returning clients.claim() from the listener isn't
	// enough — the service worker event system ignores the return value, so the
	// claim could lose the race to activation completing and leave existing tabs
	// controlled by the old worker (which still caches /api/ streams) until a full
	// reload, leaving the multi-tab bug unfixed for the very tabs this repairs.
	event.waitUntil(Promise.all([deleteOldCaches(), self.clients.claim()]));
});

self.addEventListener('fetch', (event) => {
	if (event.request.method !== 'GET') return;

	const url = new URL(event.request.url);
	if (url.origin !== self.location.origin) return;

	// Never intercept API requests. Caching the never-ending `/api/.../stream`
	// responses leaked memory and collapsed every tab onto a single cached
	// connection, so only the first tab received live events. Let API requests
	// — including streams — go straight to the network, one per tab.
	if (url.pathname.startsWith(API_PREFIX)) return;

	// Generated application chunks and navigations already carry content-hashed
	// URLs or need the current backend response. Intercept only the static assets
	// installed above so an uncached network failure cannot reject respondWith().
	if (!ASSET_PATHS.has(url.pathname)) return;

	async function respond() {
		const cache = await caches.open(CACHE);
		const cachedResponse = await cache.match(event.request);

		if (cachedResponse) {
			return cachedResponse;
		}

		try {
			return await fetch(event.request);
		} catch {
			return new Response(null, { status: 503 });
		}
	}

	event.respondWith(respond());
});
