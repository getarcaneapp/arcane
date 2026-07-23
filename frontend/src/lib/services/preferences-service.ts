import BaseAPIService from './api-service';
import type { UserPreferences, UserPreferencesPatch } from '#lib/types/user-preferences';

class PreferencesService extends BaseAPIService {
	async getMyPreferences(): Promise<UserPreferences> {
		return this.handleResponse(this.api.get('/auth/me/prefs'));
	}

	async updateMyPreferences(prefs: UserPreferencesPatch): Promise<void> {
		await this.handleResponse(this.api.patch('/auth/me/prefs', prefs));
	}
}

export const preferencesService = new PreferencesService();
