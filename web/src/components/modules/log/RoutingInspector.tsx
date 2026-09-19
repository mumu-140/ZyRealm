'use client';

import { useMemo, useState } from 'react';
import { Activity, ChevronDown, ChevronLeft, ChevronRight, Route } from 'lucide-react';
import type { RelayLog } from '@/api/endpoints/log';
import {
    useRoutingExplanation,
    type RoutingAttemptSummary,
    type RoutingDecisionEvent,
} from '@/api/endpoints/routing-inspector';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';

interface RoutingInspectorProps {
    logs: RelayLog[];
}

function completenessLabel(value: string) {
    switch (value) {
        case 'complete':
            return 'Complete';
        case 'mixed_partial':
            return 'Mixed / partial';
        case 'decision_only':
            return 'Decision only';
        case 'legacy_partial':
        default:
            return 'Legacy / partial';
    }
}

function decisionTarget(decision: RoutingDecisionEvent) {
    const parts = [decision.channel_name, decision.model_name, decision.protocol].filter(Boolean);
    if (parts.length > 0) return parts.join(' · ');
    if (decision.channel_id) return `Channel ${decision.channel_id}`;
    return 'Routing candidate';
}

function attemptTarget(attempt: RoutingAttemptSummary) {
    const provider = attempt.channel_name || `Channel ${attempt.channel_id}`;
    return [provider, attempt.model_name, attempt.protocol].filter(Boolean).join(' · ');
}

function TraceValue({ label, value }: { label: string; value?: string | number | boolean | null }) {
    if (value === undefined || value === null || value === '') return null;
    return (
        <span className="rounded-md bg-muted/60 px-2 py-1 text-[11px] text-muted-foreground">
            <span className="font-medium text-foreground/80">{label}</span> {String(value)}
        </span>
    );
}

export function RoutingInspector({ logs }: RoutingInspectorProps) {
    const availableLogs = useMemo(() => logs.filter((item) => item.id > 0), [logs]);
    const [preferredLogID, setPreferredLogID] = useState<number | null>(null);
    const [expanded, setExpanded] = useState(false);
    const selectedLogID = preferredLogID != null && availableLogs.some((item) => item.id === preferredLogID)
        ? preferredLogID
        : availableLogs[0]?.id ?? null;

    const explanationQuery = useRoutingExplanation(expanded ? selectedLogID : null);
    const explanation = explanationQuery.data;

    return (
        <section className="rounded-xl border bg-card/70 shadow-sm">
            <div className="flex flex-col gap-3 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
                <button
                    type="button"
                    className="flex min-w-0 items-center gap-2 text-left"
                    onClick={() => setExpanded((value) => !value)}
                    aria-expanded={expanded}
                >
                    <span className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
                        <Route className="size-4" />
                    </span>
                    <span className="min-w-0">
                        <span className="flex items-center gap-2">
                            <span className="text-sm font-semibold">Routing Inspector</span>
                            {explanation ? (
                                <Badge variant="secondary" className="h-5 px-1.5 text-[10px]">
                                    {completenessLabel(explanation.completeness)}
                                </Badge>
                            ) : null}
                        </span>
                        <span className="block truncate text-xs text-muted-foreground">
                            Historical routing explanation · read-only
                        </span>
                    </span>
                    <ChevronDown className={cn('size-4 shrink-0 text-muted-foreground transition-transform', expanded && 'rotate-180')} />
                </button>

                <select
                    aria-label="Select completed request"
                    className="h-9 min-w-0 rounded-md border bg-background px-2 text-xs text-foreground outline-none focus:ring-2 focus:ring-ring sm:max-w-[360px]"
                    value={selectedLogID ?? ''}
                    onChange={(event) => setPreferredLogID(Number(event.target.value) || null)}
                    disabled={availableLogs.length === 0}
                >
                    {availableLogs.length === 0 ? <option value="">No completed requests</option> : null}
                    {availableLogs.map((item) => (
                        <option key={item.id} value={item.id}>
                            #{item.id} · {item.request_model_name || item.actual_model_name || 'request'}
                        </option>
                    ))}
                </select>
            </div>

            {expanded ? (
                <div className="border-t px-4 py-4">
                    {explanationQuery.isLoading ? (
                        <div className="flex items-center gap-2 py-6 text-sm text-muted-foreground">
                            <Activity className="size-4 animate-pulse" />
                            Loading routing explanation…
                        </div>
                    ) : explanationQuery.isError ? (
                        <div className="rounded-lg border border-destructive/20 bg-destructive/5 px-3 py-3 text-sm text-destructive">
                            Routing explanation is unavailable for this request.
                        </div>
                    ) : !explanation ? (
                        <div className="py-6 text-sm text-muted-foreground">Select a completed request to inspect its route.</div>
                    ) : (
                        <div className="space-y-5">
                            <div className="grid gap-3 md:grid-cols-2">
                                <div className="rounded-lg border bg-background/70 p-3">
                                    <div className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">Requested model</div>
                                    <div className="mt-1 truncate text-sm font-semibold">{explanation.requested_model || '—'}</div>
                                    <div className="mt-2 text-xs text-muted-foreground">
                                        Trace v{explanation.version} · {explanation.success ? 'request succeeded' : 'request did not complete successfully'}
                                    </div>
                                </div>
                                <div className="rounded-lg border bg-background/70 p-3">
                                    <div className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">Final route</div>
                                    {explanation.final_route ? (
                                        <>
                                            <div className="mt-1 truncate text-sm font-semibold">
                                                {explanation.final_route.channel_name || `Channel ${explanation.final_route.channel_id}`}
                                            </div>
                                            <div className="mt-1 text-xs text-muted-foreground">
                                                {[explanation.final_route.model_name, explanation.final_route.protocol, explanation.final_route.status]
                                                    .filter(Boolean)
                                                    .join(' · ')}
                                            </div>
                                        </>
                                    ) : (
                                        <div className="mt-1 text-sm text-muted-foreground">No upstream route was dispatched.</div>
                                    )}
                                </div>
                            </div>

                            <div>
                                <div className="mb-2 flex items-center justify-between gap-2">
                                    <h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Decisions</h3>
                                    <span className="text-[11px] text-muted-foreground">{explanation.decisions.length} observed</span>
                                </div>
                                {explanation.decisions.length === 0 ? (
                                    <div className="rounded-lg border border-dashed px-3 py-4 text-xs text-muted-foreground">
                                        No candidate rejection events were persisted. Legacy traces are intentionally not backfilled with inferred reasons.
                                    </div>
                                ) : (
                                    <div className="space-y-2">
                                        {explanation.decisions.map((decision) => (
                                            <div key={`${decision.sequence}-${decision.stage}-${decision.channel_id ?? 0}-${decision.channel_key_id ?? 0}`} className="rounded-lg border bg-background/70 p-3">
                                                <div className="flex flex-wrap items-center gap-2">
                                                    <Badge variant="outline" className="h-5 px-1.5 text-[10px] tabular-nums">#{decision.sequence}</Badge>
                                                    <span className="text-xs font-semibold">{decision.stage}</span>
                                                    <span className="text-xs text-muted-foreground">{decision.outcome}</span>
                                                    {decision.reason ? <Badge variant="secondary" className="h-5 px-1.5 text-[10px]">{decision.reason}</Badge> : null}
                                                </div>
                                                <div className="mt-2 text-xs text-muted-foreground">{decisionTarget(decision)}</div>
                                                {decision.expires_at ? (
                                                    <div className="mt-1 text-[11px] text-muted-foreground">expires {new Date(decision.expires_at * 1000).toLocaleString()}</div>
                                                ) : null}
                                            </div>
                                        ))}
                                    </div>
                                )}
                            </div>

                            <div>
                                <div className="mb-2 flex items-center justify-between gap-2">
                                    <h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Attempts</h3>
                                    <span className="text-[11px] text-muted-foreground">{explanation.attempts.length} real attempts</span>
                                </div>
                                {explanation.attempts.length === 0 ? (
                                    <div className="rounded-lg border border-dashed px-3 py-4 text-xs text-muted-foreground">
                                        This request was filtered before an upstream attempt was dispatched.
                                    </div>
                                ) : (
                                    <div className="space-y-2">
                                        {explanation.attempts.map((attempt) => (
                                            <div key={`${attempt.attempt_num}-${attempt.channel_id}-${attempt.channel_key_id ?? 0}`} className="rounded-lg border bg-background/70 p-3">
                                                <div className="flex flex-wrap items-center gap-2">
                                                    <Badge variant="outline" className="h-5 px-1.5 text-[10px]">Attempt {attempt.attempt_num}</Badge>
                                                    <span className="min-w-0 truncate text-xs font-semibold">{attemptTarget(attempt)}</span>
                                                    <Badge variant="secondary" className="h-5 px-1.5 text-[10px]">{attempt.status}</Badge>
                                                </div>
                                                <div className="mt-2 flex flex-wrap gap-1.5">
                                                    <TraceValue label="failure" value={attempt.failure_domain || attempt.failure_scope} />
                                                    <TraceValue label="rule" value={attempt.rule_id} />
                                                    <TraceValue label="retry" value={attempt.retry_directive} />
                                                    <TraceValue label="stop" value={attempt.failover_stop_reason} />
                                                    <TraceValue label="runtime" value={attempt.runtime_state || attempt.runtime_effect} />
                                                    <TraceValue label="replay" value={attempt.replay_safety} />
                                                    <TraceValue label="dispatch" value={attempt.dispatch_state} />
                                                    <TraceValue label="provider attempt" value={attempt.provider_attempt} />
                                                    <TraceValue label="wire attempt" value={attempt.wire_attempt} />
                                                    <TraceValue label="committed" value={attempt.downstream_committed || undefined} />
                                                </div>
                                            </div>
                                        ))}
                                    </div>
                                )}
                            </div>
                        </div>
                    )}
                </div>
            ) : null}
        </section>
    );
}

export interface LogDetailRoutingInspectorProps {
    logId: number;
    initialExpanded?: boolean;
}

export function LogDetailRoutingInspector({ logId, initialExpanded = false }: LogDetailRoutingInspectorProps) {
    const [expanded, setExpanded] = useState(initialExpanded);
    const explanationQuery = useRoutingExplanation(expanded ? logId : null);
    const explanation = explanationQuery.data;

    return (
        <div
            className={cn(
                'rounded-2xl border border-border/60 bg-muted/20 transition-all duration-200 flex flex-col min-h-0 overflow-hidden shrink-0',
                expanded ? 'w-80 md:w-96 lg:w-[380px]' : 'w-10 hover:bg-muted/40 cursor-pointer select-none',
            )}
            onClick={!expanded ? () => setExpanded(true) : undefined}
            role={!expanded ? 'button' : undefined}
            tabIndex={!expanded ? 0 : undefined}
            aria-label={!expanded ? '展开 Routing Inspector' : undefined}
            title={!expanded ? '展开 Routing Inspector' : undefined}
            onKeyDown={!expanded ? (e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault();
                    setExpanded(true);
                }
            } : undefined}
        >
            {!expanded ? (
                <div className="flex flex-col items-center justify-between h-full py-4 px-1">
                    <span className="flex size-7 items-center justify-center rounded-lg bg-primary/10 text-primary shrink-0">
                        <Route className="size-3.5" />
                    </span>
                    <span className="[writing-mode:vertical-lr] text-xs font-medium text-muted-foreground tracking-wider select-none py-2 my-auto">
                        Routing Inspector
                    </span>
                    <ChevronLeft className="size-4 text-muted-foreground/70 shrink-0" />
                </div>
            ) : (
                <div className="flex flex-col h-full min-h-0 overflow-hidden">
                    <div className="flex items-center justify-between gap-2 px-3.5 py-3 border-b border-border/50 bg-muted/40 shrink-0">
                        <div className="flex items-center gap-2 min-w-0">
                            <span className="flex size-7 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
                                <Route className="size-3.5" />
                            </span>
                            <div className="min-w-0">
                                <div className="flex items-center gap-1.5">
                                    <span className="text-xs font-semibold truncate">Routing Inspector</span>
                                    {explanation ? (
                                        <Badge variant="secondary" className="h-4 px-1 text-[9px]">
                                            {completenessLabel(explanation.completeness)}
                                        </Badge>
                                    ) : null}
                                </div>
                                <span className="block truncate text-[10px] text-muted-foreground">
                                    Historical routing explanation · read-only
                                </span>
                            </div>
                        </div>
                        <Button
                            type="button"
                            variant="ghost"
                            size="icon"
                            className="size-6 p-0 text-muted-foreground hover:text-foreground shrink-0"
                            onClick={() => setExpanded(false)}
                            aria-label="折叠 Routing Inspector"
                            title="折叠 Routing Inspector"
                        >
                            <ChevronRight className="size-4" />
                        </Button>
                    </div>

                    <div className="flex-1 overflow-y-auto p-3 space-y-3 min-h-0 text-xs">
                        {explanationQuery.isLoading ? (
                            <div className="flex items-center justify-center gap-2 py-8 text-xs text-muted-foreground">
                                <Activity className="size-4 animate-pulse text-primary" />
                                Loading routing explanation…
                            </div>
                        ) : explanationQuery.isError ? (
                            <div className="rounded-lg border border-destructive/20 bg-destructive/5 p-3 text-xs text-destructive">
                                Routing explanation is unavailable for this request.
                            </div>
                        ) : !explanation ? (
                            <div className="py-8 text-center text-xs text-muted-foreground">
                                No routing explanation available.
                            </div>
                        ) : (
                            <div className="space-y-3">
                                <div className="grid gap-2">
                                    <div className="rounded-xl border bg-background/80 p-2.5 shadow-sm">
                                        <div className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
                                            Requested model
                                        </div>
                                        <div className="mt-0.5 truncate text-xs font-semibold">
                                            {explanation.requested_model || '—'}
                                        </div>
                                        <div className="mt-1 text-[10px] text-muted-foreground">
                                            Trace v{explanation.version} · {explanation.success ? 'succeeded' : 'incomplete'}
                                        </div>
                                    </div>
                                    <div className="rounded-xl border bg-background/80 p-2.5 shadow-sm">
                                        <div className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
                                            Final route
                                        </div>
                                        {explanation.final_route ? (
                                            <>
                                                <div className="mt-0.5 truncate text-xs font-semibold">
                                                    {explanation.final_route.channel_name || `Channel ${explanation.final_route.channel_id}`}
                                                </div>
                                                <div className="mt-0.5 text-[10px] text-muted-foreground truncate">
                                                    {[
                                                        explanation.final_route.model_name,
                                                        explanation.final_route.protocol,
                                                        explanation.final_route.status,
                                                    ]
                                                        .filter(Boolean)
                                                        .join(' · ')}
                                                </div>
                                            </>
                                        ) : (
                                            <div className="mt-0.5 text-xs text-muted-foreground">
                                                No upstream route dispatched.
                                            </div>
                                        )}
                                    </div>
                                </div>

                                <div>
                                    <div className="mb-1.5 flex items-center justify-between gap-2">
                                        <h4 className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
                                            Decisions
                                        </h4>
                                        <span className="text-[10px] tabular-nums text-muted-foreground">
                                            {explanation.decisions.length} observed
                                        </span>
                                    </div>
                                    {explanation.decisions.length === 0 ? (
                                        <div className="rounded-lg border border-dashed px-2.5 py-3 text-[11px] text-muted-foreground">
                                            No candidate rejection events were persisted.
                                        </div>
                                    ) : (
                                        <div className="space-y-1.5">
                                            {explanation.decisions.map((decision) => (
                                                <div
                                                    key={`${decision.sequence}-${decision.stage}-${decision.channel_id ?? 0}-${decision.channel_key_id ?? 0}`}
                                                    className="rounded-lg border bg-background/80 p-2 shadow-sm"
                                                >
                                                    <div className="flex flex-wrap items-center gap-1.5">
                                                        <Badge variant="outline" className="h-4 px-1 text-[9px] tabular-nums">
                                                            #{decision.sequence}
                                                        </Badge>
                                                        <span className="text-[11px] font-semibold">{decision.stage}</span>
                                                        <span className="text-[11px] text-muted-foreground">{decision.outcome}</span>
                                                        {decision.reason ? (
                                                            <Badge variant="secondary" className="h-4 px-1 text-[9px]">
                                                                {decision.reason}
                                                            </Badge>
                                                        ) : null}
                                                    </div>
                                                    <div className="mt-1 text-[10px] text-muted-foreground truncate">
                                                        {decisionTarget(decision)}
                                                    </div>
                                                    {decision.expires_at ? (
                                                        <div className="mt-0.5 text-[9px] text-muted-foreground">
                                                            expires {new Date(decision.expires_at * 1000).toLocaleTimeString()}
                                                        </div>
                                                    ) : null}
                                                </div>
                                            ))}
                                        </div>
                                    )}
                                </div>

                                <div>
                                    <div className="mb-1.5 flex items-center justify-between gap-2">
                                        <h4 className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
                                            Attempts
                                        </h4>
                                        <span className="text-[10px] tabular-nums text-muted-foreground">
                                            {explanation.attempts.length} attempts
                                        </span>
                                    </div>
                                    {explanation.attempts.length === 0 ? (
                                        <div className="rounded-lg border border-dashed px-2.5 py-3 text-[11px] text-muted-foreground">
                                            This request was filtered before an upstream attempt was dispatched.
                                        </div>
                                    ) : (
                                        <div className="space-y-1.5">
                                            {explanation.attempts.map((attempt) => (
                                                <div
                                                    key={`${attempt.attempt_num}-${attempt.channel_id}-${attempt.channel_key_id ?? 0}`}
                                                    className="rounded-lg border bg-background/80 p-2 shadow-sm"
                                                >
                                                    <div className="flex flex-wrap items-center gap-1.5">
                                                        <Badge variant="outline" className="h-4 px-1 text-[9px]">
                                                            #{attempt.attempt_num}
                                                        </Badge>
                                                        <span className="min-w-0 truncate text-[11px] font-semibold">
                                                            {attemptTarget(attempt)}
                                                        </span>
                                                        <Badge variant="secondary" className="h-4 px-1 text-[9px]">
                                                            {attempt.status}
                                                        </Badge>
                                                    </div>
                                                    <div className="mt-1.5 flex flex-wrap gap-1">
                                                        <TraceValue label="fail" value={attempt.failure_domain || attempt.failure_scope} />
                                                        <TraceValue label="rule" value={attempt.rule_id} />
                                                        <TraceValue label="retry" value={attempt.retry_directive} />
                                                        <TraceValue label="stop" value={attempt.failover_stop_reason} />
                                                        <TraceValue label="state" value={attempt.runtime_state || attempt.runtime_effect} />
                                                        <TraceValue label="replay" value={attempt.replay_safety} />
                                                        <TraceValue label="dispatch" value={attempt.dispatch_state} />
                                                        <TraceValue label="wire" value={attempt.wire_attempt} />
                                                        <TraceValue label="committed" value={attempt.downstream_committed || undefined} />
                                                    </div>
                                                </div>
                                            ))}
                                        </div>
                                    )}
                                </div>
                            </div>
                        )}
                    </div>
                </div>
            )}
        </div>
    );
}
