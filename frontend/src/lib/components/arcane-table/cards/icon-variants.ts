export type IconVariant = 'emerald' | 'red' | 'amber' | 'blue' | 'purple' | 'gray' | 'sky' | 'orange';

export const iconBgClass: Record<IconVariant, string> = {
	emerald: 'bg-success/8',
	red: 'bg-destructive/8',
	amber: 'bg-warning/8',
	blue: 'bg-info/8',
	purple: 'bg-purple/8',
	gray: 'bg-muted/40',
	sky: 'bg-sky/8',
	orange: 'bg-orange/8'
};

export const iconTextClass: Record<IconVariant, string> = {
	emerald: 'text-success',
	red: 'text-destructive',
	amber: 'text-warning',
	blue: 'text-info',
	purple: 'text-purple',
	gray: 'text-muted-foreground',
	sky: 'text-sky',
	orange: 'text-orange'
};
