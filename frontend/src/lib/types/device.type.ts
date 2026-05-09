export type Device = {
	id: string;
	name: string;
	deviceId: string;
	appVersion: string;
	osVersion: string;
	deviceModel: string;
	pairedAt: string;
	lastSeenAt?: string;
};

export type PairingCodeResponse = {
	id: string;
	shortCode: string; // formatted "ABCD-1234"
	shortCodeRaw: string; // unformatted, used for clipboard
	qrPayload: string; // arcane://pair?u=...&c=...
	expiresAt: string;
	expiresInSeconds: number;
	serverInsecure?: boolean;
};

export type PairingCodeStatus = {
	status: 'pending' | 'redeemed' | 'expired';
	expiresAt: string;
	redeemedAt?: string;
	deviceId?: string;
	deviceName?: string;
};

export type RenameDeviceRequest = {
	name: string;
};
