'use client';

import { useState } from 'react';
import {
    useInterruptLiveRequest,
    useLiveRequests,
    type LiveRequest,
} from '@/api/endpoints/live-request';
import { Button } from '@/components/ui/button';
import { Activity, ChevronDown, Loader2, Square } from 'lucide-react';
import { cn } from '@/lib/utils';

function protocolLabel(request: LiveRequest) {
    return request.upstream_protocol || request.ingress_protocol || '—';
}

function routeLabel(request: LiveRequest) {
    if (request.channel_id <= 0) return 'Routing';
    if (request.channel_key_id <= 0) return `Channel ${request.channel_id}`;
    return `Channel ${request.channel_id} · Key ${request.channel_key_id}`;
}

function startedAtLabel(startedAt: number) {
    if (!Number.isFinite(startedAt) || startedAt <= 0) return '—';
    return new Date(startedAt).toISOString().replace('T', ' ').slice(0, 19) + ' UTC';
}

export function ActiveRequests() {
    const liveQuery = useLiveRequests();
    const interrupt = useInterruptLiveRequest();
    const requests = liveQuery.data?.requests ?? [];
    const [expanded, setExpanded] = useState(true);

    if (requests.length === 0) return null;

    return (
        <section className="shrink-0 rounded-lg border bg-card/60 px-3 py-2.5 transition-all" aria-label="Active Requests">
            <div
                className="flex items-center justify-between gap-3 cursor-pointer select-none"
                onClick={() => setExpanded((prev) => !prev)}
                role="button"
                tabIndex={0}
                onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                        e.preventDefault();
                        setExpanded((prev) => !prev);
                    }
                }}
                aria-expanded={expanded}
            >
                <div className="flex min-w-0 items-center gap-2">
                    <Activity className="size-4 shrink-0 text-emerald-500 animate-pulse" />
                    <h2 className="text-sm font-semibold">Active Requests</h2>
                    <span className="rounded-full bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 px-2 py-0.5 text-xs font-semibold tabular-nums">
                        {requests.length}
                    </span>
                    {!expanded && requests.length > 0 ? (
                        <span className="hidden truncate text-xs text-muted-foreground sm:inline-block max-w-[260px] md:max-w-[420px]">
                            {requests.map((r) => r.requested_model || 'Unknown').join(', ')}
                        </span>
                    ) : null}
                </div>
                <div className="flex items-center gap-2 shrink-0">
                    <span className="text-[11px] text-muted-foreground">1 s refresh</span>
                    <Button
                        type="button"
                        variant="ghost"
                        size="icon"
                        className="size-6 p-0 text-muted-foreground hover:text-foreground"
                        aria-label={expanded ? 'Collapse Active Requests' : 'Expand Active Requests'}
                    >
                        <ChevronDown className={cn('size-4 transition-transform duration-200', expanded && 'rotate-180')} />
                    </Button>
                </div>
            </div>

            {expanded ? (
                <div className="mt-2 max-h-52 divide-y overflow-y-auto border-t pt-1">
                    {requests.map((request) => {
                        const interruptingThis =
                            request.interrupt_requested ||
                            (interrupt.isPending && interrupt.variables === request.request_id);
                        const interruptFailed =
                            interrupt.isError && interrupt.variables === request.request_id;

                        return (
                            <div
                                key={request.request_id}
                                className="grid gap-2 py-2 first:pt-0 last:pb-0 md:grid-cols-[minmax(0,1fr)_auto] md:items-center"
                            >
                                <div className="min-w-0">
                                    <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                                        <span className="truncate text-sm font-medium">
                                            {request.requested_model || 'Unknown model'}
                                        </span>
                                        <span className="rounded bg-muted px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
                                            {request.transport || 'unknown'}
                                        </span>
                                        <span className="text-xs text-muted-foreground">
                                            {request.phase || 'routing'}
                                        </span>
                                        {request.downstream_committed ? (
                                            <span className="text-[11px] text-amber-600 dark:text-amber-400">
                                                downstream committed
                                            </span>
                                        ) : null}
                                    </div>
                                    <div className="mt-1 flex flex-wrap gap-x-3 gap-y-0.5 text-[11px] text-muted-foreground">
                                        <span>{routeLabel(request)}</span>
                                        <span>{protocolLabel(request)}</span>
                                        <span>dispatch {request.dispatch_state || 'not_sent'}</span>
                                        <span>wire {request.wire_attempt}</span>
                                        <span>{startedAtLabel(request.started_at)}</span>
                                    </div>
                                    {interruptFailed ? (
                                        <div className="mt-1 text-[11px] text-destructive">Interrupt failed</div>
                                    ) : null}
                                </div>

                                <Button
                                    type="button"
                                    variant="destructive"
                                    size="sm"
                                    className="justify-self-start md:justify-self-end"
                                    disabled={interruptingThis}
                                    onClick={(e) => {
                                        e.stopPropagation();
                                        interrupt.mutate(request.request_id);
                                    }}
                                >
                                    {interruptingThis ? (
                                        <Loader2 className="animate-spin" />
                                    ) : (
                                        <Square />
                                    )}
                                    {request.interrupt_requested ? 'Interrupting' : 'Interrupt'}
                                </Button>
                            </div>
                        );
                    })}
                </div>
            ) : null}
        </section>
    );
}
