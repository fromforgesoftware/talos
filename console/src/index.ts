import { ScrollText } from '@lucide/vue';
import type { ForgeConsolePlugin } from '@fromforgesoftware/forge-console-plugin';
import { ResourceListView } from '@fromforgesoftware/forge-console-plugin/ui';
import LiveAuditTail from './components/LiveAuditTail.vue';

// PluginContext is what the forge-console-plugin loader passes to a remote
// module's default-export factory. apiBase is resolved at RUNTIME from the
// backend /apps descriptor (descriptor.apiBase), not at build time — this
// module.js is built once, before any deployment knows its gateway base.
export interface PluginContext {
	apiBase: string;
}

// talosPlugin builds the ForgeConsolePlugin for a given apiBase: the audit
// timeline over Talos's read-only JSON:API (the generic ResourceListView from
// /ui), plus the bespoke live tail over Talos's WebSocket stream
// (/api/audit-events/stream).
//
// In the forge host this used to call apiBaseFor('talos') at construction; in
// the remote module the apiBase is injected by the loader via the factory below.
export function talosPlugin(apiBase: string): ForgeConsolePlugin {
	return {
		serviceId: 'talos',
		type: 'app',
		title: 'Talos',
		basePath: '/talos',
		apiBase,
		icon: ScrollText,
		order: 2,
		pages: [
			{
				path: 'audit-events',
				name: 'Audit timeline',
				component: ResourceListView,
				props: {
					apiBase,
					type: 'audit-events',
					title: 'Audit timeline',
					columns: ['timestamp', 'action', 'actorId', 'resourceType', 'resourceId'],
				},
			},
			{
				path: 'live',
				name: 'Live tail',
				component: LiveAuditTail,
				props: { apiBase, title: 'Live audit tail' },
			},
		],
	};
}

// Default export: the apiBase-injection FACTORY. A remote module.js is built
// once, before apiBase is known — apiBase only exists at runtime, from the
// backend /apps descriptor. The forge-console-plugin loader calls this factory
// with `{ apiBase: descriptor.apiBase }` and registers the returned plugin.
//
// The factory is also tolerant of being called with no context (the loader's
// zero-arg path) — it falls back to the gateway proxy base the host uses by
// convention so the descriptor fallback in loadConsolePlugins still applies.
export default function createPlugin(ctx?: PluginContext): ForgeConsolePlugin {
	return talosPlugin(ctx?.apiBase ?? '/api/proxy/talos');
}
