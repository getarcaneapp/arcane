import type { FormInputs } from '#lib/types/form.js';
import type { MixedSettingValue, ResolvedSettings, SettingKey, SettingState } from '#lib/types/settings.js';

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

export interface DockerTabProps extends FormTabProps {
	environmentId: string;
}

export interface JobsTabProps extends FormTabProps {
	environmentId: string;
}

export interface EnvironmentSettingsContent extends LockableFormTabProps {
	settings: ResolvedSettings;
	environmentId: string;
}
