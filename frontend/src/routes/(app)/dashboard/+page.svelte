<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';
	import userStore from '#lib/stores/user-store.svelte.js';

	import AllEnvironmentsView from './components/all-environments-view.svelte';

	const currentUser = $derived(userStore.current);

	const greetingBase = $derived.by(() => {
		const hour = Temporal.Now.zonedDateTimeISO().hour;
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

<AllEnvironmentsView heroGreeting={dashboardHeroGreeting} />
