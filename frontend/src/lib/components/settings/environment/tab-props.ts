import type { EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
import type { SettingKey, SettingState } from '#lib/types/settings.js';
import type { FormInputs } from '#lib/types/ui/form.js';
import type { MixedSettingValue, ResolvedSettings } from '#lib/types/ui/settings.js';

import type { EnvironmentSettingsValues } from './environment-form-schema';

export type EnvironmentSettingsFormInputs = FormInputs<EnvironmentSettingsValues>;

export interface FormTabProps {
	formInputs: EnvironmentSettingsFormInputs;
	mixed: (key: SettingKey) => MixedSettingValue[] | undefined;
}

export interface LockableFormTabProps extends FormTabProps {
	lock: (key: SettingKey) => SettingState | undefined;
}

export interface BuildsTabProps extends LockableFormTabProps {
	settings: ResolvedSettings;
}

export interface StorageTabProps extends LockableFormTabProps {
	settings: ResolvedSettings;
	environmentId?: string;
}

export interface DockerTabProps extends FormTabProps {
	environmentId: string;
}

export interface AutomationsTabProps extends LockableFormTabProps {
	environments: EnvironmentRef[];
	containerModes: ContainerModeSupport;
}

/** How the selected environments support and use the include/exclude container modes. */
export interface ContainerModeSupport {
	/** Every selected environment supports the modes. */
	every: boolean;
	/** At least one selected environment supports the modes. */
	some: boolean;
	/** At least one selected environment uses include mode for auto-update. */
	autoUpdateInclude: boolean;
	/** At least one selected environment uses include mode for auto-heal. */
	autoHealInclude: boolean;
}

export interface EnvironmentSettingsContent extends LockableFormTabProps {
	settings: ResolvedSettings;
	environmentId: string;
}
