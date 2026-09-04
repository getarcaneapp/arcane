import type { AuthenticationResponseJSON, RegistrationResponseJSON } from '@simplewebauthn/browser';
import BaseAPIService from './api-service';
import type {
	AuthenticationResponse,
	MFAChallenge,
	MFAStatus,
	MobilePasskeyCompletion,
	Passkey,
	PasskeyLoginAvailability,
	PasskeyCapabilities,
	PasskeyChallenge,
	RecoveryCodesResponse,
	StepUpGrant
} from '#lib/types/auth';

class PasskeyService extends BaseAPIService {
	async getLoginAvailability(): Promise<PasskeyLoginAvailability> {
		return this.handleResponse(this.api.get('/auth/passkey/login/availability', { cache: 'no-store', timeout: 5000 }));
	}

	async beginLogin(): Promise<PasskeyChallenge> {
		return this.handleResponse(this.api.post('/auth/passkey/login/begin'));
	}

	async finishLogin(ceremonyId: string, credential: AuthenticationResponseJSON): Promise<AuthenticationResponse> {
		return this.handleResponse(
			this.api.post('/auth/passkey/login/finish', {
				ceremonyId,
				credential
			})
		);
	}

	async finishMobileLogin(
		ceremonyId: string,
		credential: AuthenticationResponseJSON,
		codeChallenge: string
	): Promise<MobilePasskeyCompletion> {
		return this.handleResponse(
			this.api.post('/auth/passkey/mobile/finish', {
				ceremonyId,
				credential,
				codeChallenge
			})
		);
	}

	async beginMFA(transactionId: string): Promise<MFAChallenge> {
		return this.handleResponse(this.api.post('/auth/mfa/passkey/begin', { transactionId }));
	}

	async finishMFA(transactionId: string, credential: AuthenticationResponseJSON): Promise<AuthenticationResponse> {
		return this.handleResponse(
			this.api.post('/auth/mfa/passkey/finish', {
				transactionId,
				credential
			})
		);
	}

	async finishRecovery(transactionId: string, code: string): Promise<AuthenticationResponse> {
		return this.handleResponse(this.api.post('/auth/mfa/recovery', { transactionId, code }));
	}

	async listMine(): Promise<Passkey[]> {
		return this.handleResponse(this.api.get('/auth/me/passkeys'));
	}

	async getCapabilities(): Promise<PasskeyCapabilities> {
		return this.handleResponse(this.api.get('/auth/me/passkeys/capabilities'));
	}

	async beginRegistration(stepUpToken?: string): Promise<PasskeyChallenge> {
		return this.handleResponse(this.api.post('/auth/me/passkeys/register/begin', undefined, this.stepUpConfig(stepUpToken)));
	}

	async finishRegistration(ceremonyId: string, credential: RegistrationResponseJSON, name?: string): Promise<Passkey> {
		return this.handleResponse(
			this.api.post('/auth/me/passkeys/register/finish', {
				ceremonyId,
				credential,
				...(name?.trim() ? { name: name.trim() } : {})
			})
		);
	}

	async rename(id: string, name: string, stepUpToken: string): Promise<Passkey> {
		return this.handleResponse(
			this.api.put(`/auth/me/passkeys/${encodeURIComponent(id)}`, { name }, this.stepUpConfig(stepUpToken))
		);
	}

	async deleteMine(id: string, stepUpToken: string): Promise<void> {
		await this.handleResponse(this.api.delete(`/auth/me/passkeys/${encodeURIComponent(id)}`, this.stepUpConfig(stepUpToken)));
	}

	async beginStepUp(): Promise<PasskeyChallenge> {
		return this.handleResponse(this.api.post('/auth/me/passkeys/reauth/begin'));
	}

	async finishStepUp(transactionId: string, credential: AuthenticationResponseJSON): Promise<StepUpGrant> {
		return this.handleResponse(
			this.api.post('/auth/me/passkeys/reauth/finish', {
				transactionId,
				credential
			})
		);
	}

	async passwordStepUp(password: string): Promise<StepUpGrant> {
		return this.handleResponse(this.api.post('/auth/me/passkeys/reauth/password', { password }));
	}

	async getMFAStatus(): Promise<MFAStatus> {
		return this.handleResponse(this.api.get('/auth/me/mfa'));
	}

	async enableMFA(stepUpToken: string): Promise<RecoveryCodesResponse> {
		return this.handleResponse(this.api.post('/auth/me/mfa/enable', undefined, this.stepUpConfig(stepUpToken)));
	}

	async disableMFA(stepUpToken: string): Promise<void> {
		await this.handleResponse(this.api.post('/auth/me/mfa/disable', undefined, this.stepUpConfig(stepUpToken)));
	}

	async regenerateRecoveryCodes(stepUpToken: string): Promise<RecoveryCodesResponse> {
		return this.handleResponse(
			this.api.post('/auth/me/mfa/recovery-codes/regenerate', undefined, this.stepUpConfig(stepUpToken))
		);
	}

	private stepUpConfig(stepUpToken?: string) {
		return stepUpToken ? { headers: { 'X-Step-Up-Token': stepUpToken } } : undefined;
	}
}

export const passkeyService = new PasskeyService();
