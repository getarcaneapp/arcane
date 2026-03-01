import { error as kitError } from '@sveltejs/kit';
import { AxiosError } from 'axios';
import { extractApiErrorMessage } from './api.util';

export function extractApiErrorStatus(err: unknown, fallbackStatus = 500): number {
	if (err instanceof AxiosError) {
		return err.response?.status ?? fallbackStatus;
	}

	if (err && typeof err === 'object') {
		const maybeStatus = (err as { status?: unknown }).status;
		if (typeof maybeStatus === 'number' && Number.isFinite(maybeStatus)) {
			return maybeStatus;
		}
		if (typeof maybeStatus === 'string') {
			const parsed = Number.parseInt(maybeStatus, 10);
			if (!Number.isNaN(parsed)) {
				return parsed;
			}
		}
	}

	return fallbackStatus;
}

export function throwPageLoadError(err: unknown, fallbackMessage: string): never {
	const status = extractApiErrorStatus(err);
	const message = extractApiErrorMessage(err) || fallbackMessage;
	kitError(status, message);
}
