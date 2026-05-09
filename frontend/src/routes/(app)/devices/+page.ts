import { deviceService } from '$lib/services/device-service';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	await parent();
	const devices = await deviceService.listDevices().catch(() => []);
	return { devices };
};
