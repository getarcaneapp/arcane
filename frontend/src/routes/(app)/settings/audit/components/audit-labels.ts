import { m } from '#lib/paraglide/messages.js';
import type { AuditLog } from '#lib/types/auth.js';

export function auditOutcomeLabel(outcome: string): string {
	switch (outcome) {
		case 'success':
			return m.common_success();
		case 'denied':
			return m.audit_outcome_denied();
		case 'error':
			return m.common_error();
		default:
			return outcome;
	}
}

export function auditOutcomeVariant(outcome: string): 'green' | 'amber' | 'red' | 'gray' {
	switch (outcome) {
		case 'success':
			return 'green';
		case 'denied':
			return 'amber';
		case 'error':
			return 'red';
		default:
			return 'gray';
	}
}

export function auditActorLabel(log: AuditLog): string {
	if (log.actorUsername) return log.actorUsername;
	switch (log.actorType) {
		case 'system':
			return m.system();
		case 'agent':
			return m.audit_actor_agent();
		case 'apikey':
			return m.audit_actor_apikey();
		case 'oidc':
			return m.audit_actor_oidc();
		default:
			return m.common_unknown();
	}
}
