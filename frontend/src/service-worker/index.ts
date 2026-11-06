import { version } from '$app/env';
import { assets } from '$app/manifest';
import { asset } from '$app/paths';
import { self } from '$app/service-worker';

import { tryCatch } from '#lib/utils/try-catch.js';

const CACHE = `cache-${version}`;

const API_PREFIX = new URL('api/', self.registration.scope).pathname;
const ASSETS = assets.map(({ path }) => asset(path));
const ASSET_PATHS = new Set(ASSETS.map((assetPath) => new URL(assetPath, self.location.origin).pathname));

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
	void self.skipWaiting();
});

self.addEventListener('activate', (event) => {
	console.log('[ServiceWorker] Activate');

	async function deleteOldCaches() {
		const keyList = await caches.keys();
		await Promise.all(
			keyList
				.filter((key) => key !== CACHE)
				.map((key) => {
					console.log('[ServiceWorker] Removing old cache', key);
					return caches.delete(key);
				})
		);
	}

	event.waitUntil(Promise.all([deleteOldCaches(), self.clients.claim()]));
});

self.addEventListener('fetch', (event) => {
	if (event.request.method !== 'GET') return;

	const url = new URL(event.request.url);
	if (url.origin !== self.location.origin) return;

	if (url.pathname.startsWith(API_PREFIX)) return;

	if (!ASSET_PATHS.has(url.pathname)) return;

	async function respond() {
		const cache = await caches.open(CACHE);
		const cachedResponse = await cache.match(event.request);

		if (cachedResponse) {
			return cachedResponse;
		}

		const result = await tryCatch(fetch(event.request));
		return result.data ?? new Response(null, { status: 503 });
	}

	event.respondWith(respond());
});
