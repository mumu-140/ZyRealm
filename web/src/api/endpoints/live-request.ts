import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '../client';

export interface LiveRequest {
    request_id: string;
    started_at: number;
    transport: string;
    api_key_id: number;
    requested_model: string;
    channel_id: number;
    channel_key_id: number;
    ingress_protocol: string;
    upstream_protocol: string;
    provider_attempt: number;
    wire_attempt: number;
    dispatch_state: string;
    downstream_committed: boolean;
    phase: string;
    interrupt_requested: boolean;
}

interface LiveRequestListData {
    requests: LiveRequest[];
}

interface InterruptLiveRequestData {
    request_id: string;
    interrupt_requested: boolean;
}

export const liveRequestKeys = {
    all: ['live-requests'] as const,
};

export function useLiveRequests() {
    return useQuery({
        queryKey: liveRequestKeys.all,
        queryFn: () => apiClient.get<LiveRequestListData>('/api/v1/live-request/list'),
        refetchInterval: 1000,
    });
}

export function useInterruptLiveRequest() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (requestID: string) =>
            apiClient.post<InterruptLiveRequestData>(
                `/api/v1/live-request/${encodeURIComponent(requestID)}/interrupt`,
            ),
        onSuccess: async () => {
            await queryClient.invalidateQueries({ queryKey: liveRequestKeys.all });
        },
    });
}
