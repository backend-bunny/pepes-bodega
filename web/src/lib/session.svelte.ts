import { api, Unauthorized, type User } from './api.ts';

// Shared auth state. `user === null` with `checked` means "not logged in".
export const session = $state<{ user: User | null; checked: boolean }>({ user: null, checked: false });

export async function loadSession() {
	try {
		session.user = await api.me();
	} catch (e) {
		if (!(e instanceof Unauthorized)) console.error(e);
		session.user = null;
	} finally {
		session.checked = true;
	}
}
