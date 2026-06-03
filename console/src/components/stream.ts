// WebSocket helpers for the live audit tail. The console opens one socket to
// Talos's /api/audit-events/stream and multiplexes any number of filtered
// subscriptions over it using the kit's Message envelope: a `subscribe` message
// (its id naming the subscription) opens a feed, and the server pushes
// `message` frames tagged with that id so each panel routes its own events.
//
// Relocated verbatim from the forge host (core/http/stream.ts) so this remote
// module owns its bespoke live-tail with NO host-internal imports. These are
// pure functions — the only runtime dependency (the WebSocket itself) is opened
// by LiveAuditTail via @vueuse/core's useWebSocket.

export interface AuditStreamFilter {
	actorId?: string;
	resourceType?: string;
	resourceId?: string;
	action?: string;
}

// auditStreamURL builds the ws:// (or wss://) URL for the live tail from a
// service apiBase, switching the scheme to match http/https. A relative apiBase
// is resolved against origin. Pure. Filters travel in subscribe messages, not
// the URL.
export function auditStreamURL(apiBase: string, origin = ''): string {
	const base = /^https?:\/\//.test(apiBase)
		? apiBase
		: `${origin.replace(/\/$/, '')}${apiBase.startsWith('/') ? '' : '/'}${apiBase}`;
	const url = new URL(`${base.replace(/\/$/, '')}/api/audit-events/stream`);
	url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
	return url.toString();
}

// Message is the kit WebSocket envelope (matching go-kit's websocket.Message).
export interface Message {
	id?: string;
	type?: string;
	sn?: number;
	topic?: string;
	subject?: string;
	data?: unknown;
}

// subscribeMessage / unsubscribeMessage produce the control frames a panel
// sends to open and close its subscription. A non-empty replayFrom (RFC3339)
// asks the server to replay matching history before the live tail. Pure.
export function subscribeMessage(
	subId: string,
	filter: AuditStreamFilter = {},
	replayFrom = '',
): string {
	const data: Record<string, unknown> = {};
	for (const [key, value] of Object.entries(filter)) {
		if (value) data[key] = value;
	}
	if (replayFrom) data.replayFrom = replayFrom;
	return JSON.stringify({ type: 'subscribe', id: subId, topic: 'audit', data });
}

export function unsubscribeMessage(subId: string): string {
	return JSON.stringify({ type: 'unsubscribe', id: subId });
}

export interface AuditFrame {
	id: string;
	timestamp: string;
	action: string;
	actorId: string;
	actorType: string;
	resourceType: string;
	resourceId: string;
	summary: string;
}

export interface AuditMessage {
	subId: string;
	sn: number;
	frame: AuditFrame;
}

// parseAuditMessage parses one server frame. It returns null for anything that
// isn't an audit event frame (welcome/ack/ping/error or malformed data) so the
// caller only handles real events, routed by subId.
export function parseAuditMessage(data: string): AuditMessage | null {
	let msg: Message;
	try {
		msg = JSON.parse(data) as Message;
	} catch {
		return null;
	}
	if (!msg || msg.type !== 'message' || msg.topic !== 'audit') return null;
	const f = (msg.data ?? {}) as Partial<AuditFrame>;
	if (typeof f.id !== 'string') return null;
	return {
		subId: msg.subject ?? '',
		sn: msg.sn ?? 0,
		frame: {
			id: f.id,
			timestamp: f.timestamp ?? '',
			action: f.action ?? '',
			actorId: f.actorId ?? '',
			actorType: f.actorType ?? '',
			resourceType: f.resourceType ?? '',
			resourceId: f.resourceId ?? '',
			summary: f.summary ?? '',
		},
	};
}
