import type { CompactTablePrefs } from '#lib/components/arcane-table/arcane-table.types.svelte';

export type TablePreferences = Record<string, CompactTablePrefs>;
export type TablePreferencesPatch = Record<string, CompactTablePrefs | null>;

export type UserPreferences = {
	tables: TablePreferences;
};

export type UserPreferencesPatch = {
	tables: TablePreferencesPatch;
};
