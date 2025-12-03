import { redirect } from '@sveltejs/kit';
import type { PageLoad } from './$types';
import { authService } from '$lib/services/auth-service';

export const load: PageLoad = async ({ fetch }) => {
	try {
		await fetch('/api/auth/logout', {
			method: 'POST',
			credentials: 'include'
		});
	} catch (error) {
		console.error('Logout error:', error);
	}

	authService.logout();

	throw redirect(302, '/login');
};
