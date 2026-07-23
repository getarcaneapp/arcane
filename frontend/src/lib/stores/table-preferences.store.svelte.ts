import { browser } from '$app/env';
import type { CompactTablePrefs } from '#lib/components/arcane-table/arcane-table.types.svelte';
import { preferencesService } from '#lib/services/preferences-service';
import type { TablePreferences, TablePreferencesPatch } from '#lib/types/user-preferences';

const COMPACT_PREFERENCE_FIELDS = new Set(['v', 'f', 'g', 's', 'l', 'm', 'c']);
const WRITE_DEBOUNCE_MS = 500;

class TablePreferencesStore {
	#prefs = $state<TablePreferences>({});
	#readyPromise: Promise<void> | null = null;
	#authenticated = false;
	#generation = 0;
	#pendingPatch: TablePreferencesPatch = {};
	#writeTimer: ReturnType<typeof setTimeout> | null = null;

	ready(): Promise<void> {
		if (!browser) return Promise.resolve();
		if (this.#readyPromise) return this.#readyPromise;

		const generation = this.#generation;
		this.#readyPromise = (async () => {
			try {
				const serverPrefs = (await preferencesService.getMyPreferences()).tables ?? {};
				if (generation !== this.#generation) return;

				this.#authenticated = true;
				this.#prefs = { ...serverPrefs };

				const localPrefs = this.#readLocalPreferences();
				if (Object.keys(serverPrefs).length === 0) {
					this.#prefs = { ...localPrefs };
					if (Object.keys(localPrefs).length > 0) {
						try {
							await preferencesService.updateMyPreferences({ tables: localPrefs });
							if (generation === this.#generation) {
								for (const key of Object.keys(localPrefs)) localStorage.removeItem(key);
							}
						} catch (error) {
							console.debug('Failed to import local table preferences', error);
						}
					}
				} else {
					for (const key of Object.keys(localPrefs)) localStorage.removeItem(key);
				}
			} catch (error) {
				if (generation !== this.#generation) return;
				this.#authenticated = false;
				this.#prefs = {};
				console.debug('Failed to load table preferences', error);
			}
		})();

		return this.#readyPromise;
	}

	reset(): void {
		this.#generation += 1;
		this.#authenticated = false;
		this.#prefs = {};
		this.#readyPromise = null;
		this.#pendingPatch = {};
		if (this.#writeTimer) clearTimeout(this.#writeTimer);
		this.#writeTimer = null;
	}

	get(key: string): CompactTablePrefs | undefined {
		return this.#prefs[key];
	}

	set(key: string, prefs: CompactTablePrefs): void {
		const next = { ...prefs };
		this.#prefs = { ...this.#prefs, [key]: next };

		if (!browser || !this.#authenticated) return;
		this.#pendingPatch = { ...this.#pendingPatch, [key]: next };
		if (this.#writeTimer) clearTimeout(this.#writeTimer);

		const generation = this.#generation;
		this.#writeTimer = setTimeout(() => {
			this.#writeTimer = null;
			if (generation !== this.#generation || !this.#authenticated) return;

			const patch = this.#pendingPatch;
			this.#pendingPatch = {};
			void preferencesService
				.updateMyPreferences({ tables: patch })
				.catch((error) => console.debug('Failed to save table preferences', error));
		}, WRITE_DEBOUNCE_MS);
	}

	update(key: string, patch: Partial<CompactTablePrefs>): void {
		this.set(key, { ...(this.get(key) ?? {}), ...patch });
	}

	#readLocalPreferences(): TablePreferences {
		const prefs: TablePreferences = {};
		for (let index = 0; index < localStorage.length; index += 1) {
			const key = localStorage.key(index);
			if (!key?.startsWith('arcane-') || key.length > 200) continue;

			try {
				const value = JSON.parse(localStorage.getItem(key) ?? 'null');
				if (!value || typeof value !== 'object' || Array.isArray(value)) continue;
				if (!Object.keys(value).every((field) => COMPACT_PREFERENCE_FIELDS.has(field))) continue;
				prefs[key] = value as CompactTablePrefs;
			} catch {
				// Ignore unrelated or malformed local storage entries.
			}
		}
		return prefs;
	}
}

export const tablePreferences = new TablePreferencesStore();
