import { loadSwarmKvPage } from '#lib/utils/swarm-kv.js';

import type { PageLoad } from './$types';

export const load: PageLoad = ({ parent }) => loadSwarmKvPage(parent, 'configs');
