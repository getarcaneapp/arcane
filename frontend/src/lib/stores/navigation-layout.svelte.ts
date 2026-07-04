export type NavigationLayout = 'sidebar' | 'header';

export const navigationLayoutStore = $state<{ current: NavigationLayout }>({ current: 'sidebar' });
