import { LockIcon, TemplateIcon, type IconType } from '#lib/icons/index.js';
import { m } from '#lib/paraglide/messages.js';
import { PERMISSIONS, type Permission } from '#lib/permissions.generated.js';
import type { SwarmKvKind } from '#lib/types/swarm.js';

export type SwarmKvResource = {
	kind: SwarmKvKind;
	persistKey: string;
	permission: Permission;
	icon: IconType;
	resource: () => string;
	title: () => string;
	subtitle: () => string;
	createLabel: () => string;
	editTitle: () => string;
	namePlaceholder: () => string;
	nameLocked: () => string;
	nameRequired: () => string;
	immutableNotice: () => string;
	createSuccess: (name: string) => string;
	createFailed: () => string;
	deleteConfirm: (name: string) => string;
	deleteSuccess: (name: string) => string;
	deleteFailed: (name: string) => string;
};

export const swarmKvResources: Record<SwarmKvKind, SwarmKvResource> = {
	secrets: {
		kind: 'secrets',
		persistKey: 'arcane-swarm-secrets-table',
		permission: PERMISSIONS.SWARM_SECRETS,
		icon: LockIcon,
		resource: m.swarm_secret,
		title: m.swarm_secrets_title,
		subtitle: m.swarm_secrets_subtitle,
		createLabel: m.create_secret,
		editTitle: m.swarm_secrets_edit_title,
		namePlaceholder: m.swarm_secrets_name_placeholder,
		nameLocked: m.swarm_secrets_name_locked,
		nameRequired: m.swarm_secrets_name_required,
		immutableNotice: m.swarm_secrets_immutable_notice,
		createSuccess: (name) => m.swarm_secrets_create_success({ name }),
		createFailed: m.swarm_secrets_create_failed,
		deleteConfirm: (name) => m.swarm_secrets_delete_confirm({ name }),
		deleteSuccess: (name) => m.swarm_secrets_delete_success({ name }),
		deleteFailed: (name) => m.swarm_secrets_delete_failed({ name })
	},
	configs: {
		kind: 'configs',
		persistKey: 'arcane-swarm-configs-table',
		permission: PERMISSIONS.SWARM_CONFIGS,
		icon: TemplateIcon,
		resource: m.swarm_config,
		title: m.swarm_configs_title,
		subtitle: m.swarm_configs_subtitle,
		createLabel: m.create_config,
		editTitle: m.swarm_configs_edit_title,
		namePlaceholder: m.swarm_configs_name_placeholder,
		nameLocked: m.swarm_configs_name_locked,
		nameRequired: m.swarm_configs_name_required,
		immutableNotice: m.swarm_configs_immutable_notice,
		createSuccess: (name) => m.swarm_configs_create_success({ name }),
		createFailed: m.swarm_configs_create_failed,
		deleteConfirm: (name) => m.swarm_configs_delete_confirm({ name }),
		deleteSuccess: (name) => m.swarm_configs_delete_success({ name }),
		deleteFailed: (name) => m.swarm_configs_delete_failed({ name })
	}
};

export function decodeBase64ToText(base64Value: string): string {
	try {
		const binary = atob(base64Value);
		const bytes = Uint8Array.from(binary, (char) => char.charCodeAt(0));
		return new TextDecoder().decode(bytes);
	} catch {
		return '';
	}
}

export function encodeTextToBase64(value: string): string {
	const bytes = new TextEncoder().encode(value);
	let binary = '';
	for (const byte of bytes) {
		binary += String.fromCharCode(byte);
	}
	return btoa(binary);
}
