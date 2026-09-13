import { createContext } from 'svelte';
import type { SettingsFormContext } from '#lib/types/settings-form.js';

export const [getSettingsFormContext, setSettingsFormContext, hasSettingsFormContext] = createContext<SettingsFormContext>();
