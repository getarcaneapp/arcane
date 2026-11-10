import type { Environment } from '#lib/types/environment.js';

import type { Paginated } from './base';
import type { User } from './user';
import type { VersionInfo } from './version';

export interface AuthLogin {
	username: string;
	password: string;
}

export type AuthenticationStatus = 'authenticated' | 'mfa_required';

export type PasskeyLoginAvailability = {
	available: boolean;
};

export type PasskeyChallenge = {
	ceremonyId: string;
	transactionId?: string;
	options: Record<string, unknown>;
	expiresAt: string;
};

export type MobilePasskeyCompletion = {
	transactionId: string;
	expiresAt: string;
};

export type MFAChallenge = {
	transactionId: string;
	method: string;
	options: Record<string, unknown>;
	expiresAt: string;
};

export type AuthenticationResponse = {
	success: boolean;
	status: AuthenticationStatus;
	token?: string;
	expiresAt?: string;
	user?: User;
	mfa?: MFAChallenge;
};

export type Passkey = {
	id: string;
	name: string;
	rpId: string;
	aaguid?: string;
	transports?: string[];
	backupEligible: boolean;
	backupState: boolean;
	cloneWarning: boolean;
	authenticatorAttachment?: string;
	createdAt: string;
	updatedAt?: string;
	lastUsedAt?: string;
};

export type PasskeyCapabilities = {
	passkeyMfaEnabled: boolean;
	passkeyCount: number;
	hasLocalPassword: boolean;
	hasOidcFallback: boolean;
	canEnrollWithActiveSession: boolean;
	canDeleteLastPasskey: boolean;
	requiresStepUp: boolean;
};

export type MFAStatus = {
	enabled: boolean;
	passkeyCount: number;
	recoveryCodesRemaining: number;
};

export type RecoveryCodesResponse = {
	codes: string[];
};

export type StepUpGrant = {
	token: string;
	expiresAt: string;
};

export interface AutoLoginConfig {
	enabled: boolean;
	username: string;
}

export interface OidcStatusInfo {
	mergeAccounts: boolean;
	providerName?: string;
	providerLogoUrl?: string;
}

export type SessionBootstrap = {
	user: User;
	version: VersionInfo;
	environments: Paginated<Environment>;
};
