import { useQuery } from '@tanstack/react-query';
import { apiClient } from '../client';

export type RoutingDecisionStage = 'candidate' | 'credential' | 'protocol' | 'dispatch' | 'failover';
export type RoutingDecisionOutcome = 'eligible' | 'rejected' | 'selected' | 'attempted' | 'stopped';
export type RoutingExplanationCompleteness = 'complete' | 'mixed_partial' | 'legacy_partial' | 'decision_only';

export interface RoutingDecisionEvent {
    sequence: number;
    stage: RoutingDecisionStage;
    outcome: RoutingDecisionOutcome;
    reason?: string;
    channel_id?: number;
    channel_key_id?: number;
    channel_name?: string;
    model_name?: string;
    protocol?: string;
    expires_at?: number;
}

export interface RoutingRouteSummary {
    attempt_num: number;
    channel_id: number;
    channel_key_id?: number;
    channel_name?: string;
    model_name?: string;
    status: string;
    duration_ms?: number;
    protocol?: string;
}

export interface RoutingAttemptSummary extends RoutingRouteSummary {
    failure_domain?: string;
    failure_scope?: string;
    rule_id?: string;
    retry_directive?: string;
    failover_stop_reason?: string;
    runtime_effect?: string;
    runtime_state?: string;
    cooldown_until?: number;
    circuit_effect?: string;
    outlier_effect?: string;
    replay_safety?: string;
    dispatch_state?: string;
    downstream_committed?: boolean;
    outer_context_state?: string;
    outbound_context_cause?: string;
    provider_attempt?: number;
    wire_attempt?: number;
}

export interface RoutingExplanation {
    version: string;
    completeness: RoutingExplanationCompleteness;
    log_id: number;
    time: number;
    requested_model?: string;
    success: boolean;
    final_route?: RoutingRouteSummary | null;
    decisions: RoutingDecisionEvent[];
    attempts: RoutingAttemptSummary[];
}

export function useRoutingExplanation(logID: number | null | undefined) {
    return useQuery({
        queryKey: ['routing-explanation', logID],
        queryFn: async () => apiClient.get<RoutingExplanation>(
            `/api/v1/log/${encodeURIComponent(String(logID))}/routing`,
        ),
        enabled: typeof logID === 'number' && logID > 0,
        staleTime: 30_000,
        refetchOnWindowFocus: false,
    });
}
