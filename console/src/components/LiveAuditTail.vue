<script setup lang="ts">
import { ref, computed, watch } from 'vue';
import { useWebSocket } from '@vueuse/core';
import {
	Badge,
	Button,
	DatePicker,
	Table,
	TableHeader,
	TableBody,
	TableRow,
	TableHead,
	TableCell,
} from '@fromforgesoftware/vue-kit';
import { type ForgeDate } from '@fromforgesoftware/ts-kit';
import {
	auditStreamURL,
	subscribeMessage,
	unsubscribeMessage,
	parseAuditMessage,
	type AuditFrame,
} from './stream';

// Live audit tail backed by Talos's multiplexed WebSocket stream. One socket
// (vue-kit's @vueuse useWebSocket, with auto-reconnect) carries a named
// subscription; the server tags frames with that id so this panel only renders
// its own events. Frames are prepended newest-first and capped.
//
// Bespoke to the Talos console: there is no generic stream renderer in
// forge-console-plugin/ui, so this panel and its ./stream helper are relocated
// into this remote module. apiBase is injected by the plugin factory (from the
// /apps descriptor); the host gateway authenticates via the session cookie.
const props = defineProps<{
	apiBase: string;
	action?: string;
	resourceType?: string;
	title?: string;
}>();

const MAX_ROWS = 200;
const SUB_ID = 'tail';
const rows = ref<AuditFrame[]>([]);
// replayFrom asks the server to replay matching history (RFC3339) before the
// live tail; null = live only.
const replayFrom = ref<ForgeDate | null>(null);
const replayFromISO = computed(() => replayFrom.value?.toISO() ?? '');

const url = computed(() => auditStreamURL(props.apiBase, window.location.origin));
const filter = computed(() => ({ action: props.action, resourceType: props.resourceType }));

const { status, send } = useWebSocket(url, {
	autoReconnect: { retries: -1, delay: 2000 },
	onConnected() {
		send(subscribeMessage(SUB_ID, filter.value, replayFromISO.value));
	},
	onMessage(_ws, event) {
		const msg = parseAuditMessage(typeof event.data === 'string' ? event.data : '');
		if (!msg || msg.subId !== SUB_ID) return;
		rows.value = [msg.frame, ...rows.value].slice(0, MAX_ROWS);
	},
});

const connected = computed(() => status.value === 'OPEN');

function resubscribe() {
	rows.value = [];
	if (status.value !== 'OPEN') return;
	send(unsubscribeMessage(SUB_ID));
	send(subscribeMessage(SUB_ID, filter.value, replayFromISO.value));
}

// Re-subscribe when the panel's filter changes.
watch(filter, resubscribe);
</script>

<template>
	<section class="space-y-4">
		<header class="flex items-center gap-3">
			<h1 class="text-xl font-semibold">{{ title ?? 'Live audit tail' }}</h1>
			<Badge :variant="connected ? 'soft-success' : 'secondary'">
				<span
					class="mr-1 inline-block h-2 w-2 rounded-full"
					:class="connected ? 'animate-pulse bg-success' : 'bg-muted-foreground'"
				/>
				{{ connected ? 'live' : status.toLowerCase() }}
			</Badge>
			<div class="ml-auto flex items-center gap-2">
				<label class="text-sm text-muted-foreground">Replay from</label>
				<DatePicker v-model="replayFrom" granularity="second" placeholder="Live only" />
				<Button variant="outline" @click="resubscribe">Apply</Button>
			</div>
		</header>

		<p v-if="rows.length === 0" class="text-sm text-muted-foreground">Waiting for events…</p>
		<Table v-else>
			<TableHeader>
				<TableRow>
					<TableHead>Timestamp</TableHead>
					<TableHead>Action</TableHead>
					<TableHead>Actor</TableHead>
					<TableHead>Resource</TableHead>
				</TableRow>
			</TableHeader>
			<TableBody>
				<TableRow v-for="row in rows" :key="row.id">
					<TableCell class="font-mono text-xs text-muted-foreground">{{ row.timestamp }}</TableCell>
					<TableCell>{{ row.action }}</TableCell>
					<TableCell class="text-muted-foreground">{{ row.actorId }}</TableCell>
					<TableCell class="text-muted-foreground"
						>{{ row.resourceType }}/{{ row.resourceId }}</TableCell
					>
				</TableRow>
			</TableBody>
		</Table>
	</section>
</template>
