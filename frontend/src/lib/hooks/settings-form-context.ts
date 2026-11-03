import { createContext } from 'svelte';

import type { SettingsFormContext } from '#lib/types/ui/settingsform.js';

export const [getSettingsFormContext, setSettingsFormContext, hasSettingsFormContext] = createContext<SettingsFormContext>();
