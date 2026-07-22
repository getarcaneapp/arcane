<script lang="ts">
	import { createSubscriber } from 'svelte/reactivity';
	import { get } from 'svelte/store';
	import DashboardAllEnvironmentsView from './dashboard-all-environments-view.svelte';
	import userStore from '$lib/stores/user-store';
	import { m } from '$lib/paraglide/messages';

	let { data }: PageProps = $props();

	const debugAllGood = $derived(data.debugAllGood ?? false);

	const subscribeToUser = createSubscriber((update) => {
		let initialized = false;
		return userStore.subscribe(() => {
			if (initialized) {
				update();
			}
			initialized = true;
		});
	});
	const currentUser = $derived.by(() => {
		subscribeToUser();
		return get(userStore);
	});

	const greetingBase = $derived.by(() => {
		const hour = new Date().getHours();
		if (hour >= 5 && hour < 12) return m.dashboard_greeting_morning();
		if (hour >= 12 && hour < 18) return m.dashboard_greeting_afternoon();
		if (hour >= 18 && hour < 23) return m.dashboard_greeting_evening();
		return m.welcome_back();
	});
	const greetingUserName = $derived.by(() => currentUser?.displayName?.trim() || currentUser?.username?.trim() || '');
	const dashboardHeroGreeting = $derived.by(() =>
		greetingUserName
			? m.dashboard_greeting_with_name({ greeting: greetingBase, name: greetingUserName })
			: m.dashboard_greeting_without_name({ greeting: greetingBase })
	);
</script>

<DashboardAllEnvironmentsView heroGreeting={dashboardHeroGreeting} {debugAllGood} />
