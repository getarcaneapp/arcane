import type { SettingKey, SettingState, Settings, SettingsRuntime } from '../settings';

export type MixedSettingValue = {
	label: string;
	environments: string[];
	apply: () => void;
};

export interface ResolvedSettings {
	values: Settings;
	meta: Partial<Record<SettingKey, SettingState>>;
	runtime: SettingsRuntime;
	readOnly: boolean;
}
