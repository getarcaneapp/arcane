import type { Environment, EnvironmentStatus } from '#lib/types/environment.js';
import type { FormInputs } from '#lib/types/form.js';
import type { ResolvedSettings, SettingKey, SettingState } from '#lib/types/settings.js';

import type { EnvironmentFormValues } from './environment-form-schema';

export type EnvironmentFormInputs = FormInputs<EnvironmentFormValues>;

export interface ConnectionAgentTabProps {
	environment: Environment;
	currentStatus: EnvironmentStatus;
	showMTLSDownloads: boolean;
	isRegeneratingKey: boolean;
	onRegenerateApiKey: () => void;
}

export interface FormTabProps {
	formInputs: EnvironmentFormInputs;
}

export interface LockableFormTabProps extends FormTabProps {
	lock: (key: SettingKey) => SettingState | undefined;
}

export interface BuildsTabProps extends LockableFormTabProps {
	settings: ResolvedSettings;
}

export interface DockerTabProps {
	formInputs: EnvironmentFormInputs;
	environmentId: string;
	shellSelectValue: string;
	handleShellSelectChange: (value: string) => void;
	shellOptions: { value: string; label: string; description?: string }[];
}

export interface JobsTabProps {
	formInputs: EnvironmentFormInputs;
	environmentId: string;
}
