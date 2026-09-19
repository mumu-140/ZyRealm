'use client';

import { useCallback, useEffect, useMemo, useState } from 'react';
import { Activity, AlertCircle, Check, ChevronRight, ExternalLink, Loader2, RefreshCw, Square } from 'lucide-react';
import { useTranslations } from 'next-intl';
import type { Group } from '@/api/endpoints/group';
import { useInterruptLiveRequest, useLiveRequests, type LiveRequest } from '@/api/endpoints/live-request';
import { useLogPage, type RelayLog } from '@/api/endpoints/log';
import { useJumpStore } from '@/stores/jump';
import { matchesGroupName } from './utils';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/animate-ui/components/animate/tooltip';
import { cn } from '@/lib/utils';
import { toast } from '@/components/common/Toast';

interface GroupLogsPanelProps {
    group: Group;
    onCloseDialog?: () => void;
}

function formatElapsedTime(startedAt: number, now: number): string {
    if (!startedAt || startedAt <= 0) return '—';
    const diff = Math.max(0, now - startedAt);
    if (diff < 1000) return `${diff}ms`;
    const sec = diff / 1000;
    if (sec < 60) return `${sec.toFixed(1)}s`;
    const min = Math.floor(sec / 60);
    const remainingSec = Math.floor(sec % 60);
    return `${min}m ${remainingSec}s`;
}

function formatLogTime(timestamp: number): string {
    const date = new Date(timestamp * 1000);
    return date.toLocaleTimeString('zh-CN', {
        hour12: false,
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
    });
}

function formatDuration(ms: number): string {
    if (ms < 1000) return `${ms}ms`;
    return `${(ms / 1000).toFixed(2)}s`;
}

function sanitizeErrorMessage(raw: string | undefined | null): string {
    if (!raw) return '';
    const cleaned = raw.replace(/^upstream error:\s*(\d+):\s*/i, (_m, code) => `[HTTP ${code}] `);
    return cleaned.length > 100 ? `${cleaned.slice(0, 100)}…` : cleaned;
}

export function GroupLogsPanel({ group, onCloseDialog }: GroupLogsPanelProps) {
    const t = useTranslations('group');
    const liveQuery = useLiveRequests();
    const interrupt = useInterruptLiveRequest();
    const [now, setNow] = useState(() => Date.now());

    useEffect(() => {
        const timer = setInterval(() => setNow(Date.now()), 1000);
        return () => clearInterval(timer);
    }, []);

    const channelIds = useMemo(
        () => Array.from(new Set((group.items || []).map((i) => i.channel_id).filter((id) => id > 0))),
        [group.items],
    );

    const channelIdSet = useMemo(() => new Set(channelIds), [channelIds]);

    // Active requests matching this group (by name/regex match on requested_model, or channel match)
    const activeRequests = useMemo(() => {
        const list = liveQuery.data?.requests ?? [];
        return list.filter((req) => {
            if (matchesGroupName(req.requested_model, group.name, group.match_regex)) return true;
            if (req.channel_id > 0 && channelIdSet.has(req.channel_id)) return true;
            return false;
        });
    }, [channelIdSet, group.match_regex, group.name, liveQuery.data?.requests]);

    // History logs query for this group
    const historyQuery = useLogPage({
        channel_ids: channelIds.length > 0 ? channelIds : undefined,
        keyword: group.name,
        page_size: 15,
        with_total: false,
        include_content: false,
    });

    const historyLogs = useMemo(() => {
        const list = historyQuery.data?.logs ?? [];
        return list
            .filter((log) => {
                if (matchesGroupName(log.request_model_name, group.name, group.match_regex)) return true;
                if (log.channel > 0 && channelIdSet.has(log.channel)) return true;
                return false;
            })
            .slice(0, 10);
    }, [channelIdSet, group.match_regex, group.name, historyQuery.data?.logs]);

    const handleJumpToLogDetail = useCallback((logId: number) => {
        onCloseDialog?.();
        useJumpStore.getState().requestJump({ kind: 'log-detail', logId });
    }, [onCloseDialog]);

    const handleJumpToGroupLogs = useCallback(() => {
        onCloseDialog?.();
        useJumpStore.getState().requestJump({ kind: 'log-group', groupName: group.name });
    }, [group.name, onCloseDialog]);

    const handleInterrupt = useCallback((e: React.MouseEvent, req: LiveRequest) => {
        e.stopPropagation();
        interrupt.mutate(req.request_id, {
            onSuccess: () => toast.success(t('logs.interrupted') ?? '已请求中断'),
            onError: (err: Error) => toast.error(err.message),
        });
    }, [interrupt, t]);

    return (
        <TooltipProvider>
            <div className="flex flex-col h-full min-h-0 overflow-hidden select-none">
                {/* Header */}
                <div className="flex items-center justify-between pb-3 border-b border-border/60 shrink-0">
                    <div className="flex items-center gap-2 min-w-0">
                        <Activity className="size-4 shrink-0 text-primary" />
                        <h3 className="text-sm font-semibold text-foreground truncate">
                            {t('logs.title') ?? '运行日志'}
                        </h3>
                        {activeRequests.length > 0 ? (
                            <span className="flex items-center gap-1 rounded-full bg-emerald-500/15 px-2 py-0.5 text-xs font-semibold text-emerald-600 dark:text-emerald-400 tabular-nums">
                                <span className="size-1.5 rounded-full bg-emerald-500 animate-pulse" />
                                {activeRequests.length}
                            </span>
                        ) : null}
                    </div>
                    <div className="flex items-center gap-1 shrink-0">
                        <Tooltip>
                            <TooltipTrigger asChild>
                                <Button
                                    type="button"
                                    variant="ghost"
                                    size="icon"
                                    className="size-7 text-muted-foreground hover:text-foreground"
                                    onClick={() => {
                                        void liveQuery.refetch();
                                        void historyQuery.refetch();
                                    }}
                                >
                                    <RefreshCw className={cn('size-3.5', historyQuery.isFetching && 'animate-spin')} />
                                </Button>
                            </TooltipTrigger>
                            <TooltipContent>{t('logs.refresh') ?? '刷新日志'}</TooltipContent>
                        </Tooltip>

                        <Tooltip>
                            <TooltipTrigger asChild>
                                <Button
                                    type="button"
                                    variant="ghost"
                                    size="icon"
                                    className="size-7 text-muted-foreground hover:text-foreground"
                                    onClick={handleJumpToGroupLogs}
                                >
                                    <ExternalLink className="size-3.5" />
                                </Button>
                            </TooltipTrigger>
                            <TooltipContent>{t('logs.viewAll') ?? '在日志页查看全部'}</TooltipContent>
                        </Tooltip>
                    </div>
                </div>

                {/* Content Body: Active + History */}
                <div className="flex-1 min-h-0 overflow-y-auto space-y-4 pt-3 pr-1">
                    {/* Active Requests Section */}
                    <section>
                        <div className="flex items-center justify-between mb-2">
                            <span className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                                {t('logs.activeTitle') ?? '活跃请求'} ({activeRequests.length})
                            </span>
                            {activeRequests.length > 0 ? (
                                <span className="text-[10px] text-emerald-600 dark:text-emerald-400 font-medium">
                                    {t('logs.live') ?? '实时'}
                                </span>
                            ) : null}
                        </div>

                        {activeRequests.length === 0 ? (
                            <div className="rounded-xl border border-dashed border-border/60 bg-muted/20 px-3 py-3 text-center text-xs text-muted-foreground">
                                {t('logs.noActive') ?? '当前无进行中的请求'}
                            </div>
                        ) : (
                            <div className="space-y-2">
                                {activeRequests.map((req) => {
                                    const isPendingInterrupt =
                                        req.interrupt_requested ||
                                        (interrupt.isPending && interrupt.variables === req.request_id);

                                    return (
                                        <div
                                            key={req.request_id}
                                            onClick={handleJumpToGroupLogs}
                                            role="button"
                                            tabIndex={0}
                                            onKeyDown={(e) => {
                                                if (e.key === 'Enter' || e.key === ' ') {
                                                    e.preventDefault();
                                                    handleJumpToGroupLogs();
                                                }
                                            }}
                                            className="group relative flex flex-col gap-1.5 rounded-xl border border-emerald-500/30 bg-emerald-500/5 p-2.5 text-xs transition-colors hover:bg-emerald-500/10 cursor-pointer"
                                        >
                                            <div className="flex items-center justify-between gap-2 min-w-0">
                                                <div className="flex items-center gap-1.5 min-w-0">
                                                    <span className="size-2 rounded-full bg-emerald-500 animate-pulse shrink-0" />
                                                    <span className="font-semibold text-foreground truncate">
                                                        {req.requested_model || group.name}
                                                    </span>
                                                    <Badge variant="outline" className="h-4 px-1 text-[9px] uppercase font-mono">
                                                        {req.transport || 'http'}
                                                    </Badge>
                                                </div>
                                                <div className="flex items-center gap-1 shrink-0">
                                                    <span className="text-[11px] font-mono text-emerald-700 dark:text-emerald-300 font-medium">
                                                        {formatElapsedTime(req.started_at, now)}
                                                    </span>
                                                    <Tooltip>
                                                        <TooltipTrigger asChild>
                                                            <Button
                                                                type="button"
                                                                variant="ghost"
                                                                size="icon"
                                                                disabled={isPendingInterrupt}
                                                                className="size-6 text-destructive hover:bg-destructive/10"
                                                                onClick={(e) => handleInterrupt(e, req)}
                                                            >
                                                                {isPendingInterrupt ? (
                                                                    <Loader2 className="size-3 animate-spin" />
                                                                ) : (
                                                                    <Square className="size-3 fill-current" />
                                                                )}
                                                            </Button>
                                                        </TooltipTrigger>
                                                        <TooltipContent>{t('logs.interrupt') ?? '中断请求'}</TooltipContent>
                                                    </Tooltip>
                                                </div>
                                            </div>

                                            <div className="flex items-center justify-between text-[11px] text-muted-foreground">
                                                <span className="truncate">
                                                    {req.channel_id > 0 ? `Channel ${req.channel_id}` : (req.phase || 'routing')}
                                                    {req.wire_attempt > 1 ? ` · try ${req.wire_attempt}` : ''}
                                                </span>
                                                <span className="text-[10px] uppercase font-mono">
                                                    {req.upstream_protocol || req.ingress_protocol || ''}
                                                </span>
                                            </div>
                                        </div>
                                    );
                                })}
                            </div>
                        )}
                    </section>

                    {/* Recent History Section */}
                    <section>
                        <div className="flex items-center justify-between mb-2">
                            <span className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                                {t('logs.historyTitle') ?? '最近请求'} ({historyLogs.length})
                            </span>
                            <button
                                type="button"
                                onClick={handleJumpToGroupLogs}
                                className="text-[11px] text-primary hover:underline"
                            >
                                {t('logs.viewAll') ?? '查看全部'}
                            </button>
                        </div>

                        {historyQuery.isLoading && historyLogs.length === 0 ? (
                            <div className="flex justify-center py-6 text-muted-foreground">
                                <Loader2 className="size-5 animate-spin" />
                            </div>
                        ) : historyLogs.length === 0 ? (
                            <div className="rounded-xl border border-dashed border-border/60 bg-muted/20 px-3 py-4 text-center text-xs text-muted-foreground">
                                {t('logs.noHistory') ?? '暂无最近请求记录'}
                            </div>
                        ) : (
                            <div className="space-y-1.5">
                                {historyLogs.map((log: RelayLog) => {
                                    const hasError = Boolean(log.error);
                                    return (
                                        <div
                                            key={log.id}
                                            onClick={() => handleJumpToLogDetail(log.id)}
                                            role="button"
                                            tabIndex={0}
                                            onKeyDown={(e) => {
                                                if (e.key === 'Enter' || e.key === ' ') {
                                                    e.preventDefault();
                                                    handleJumpToLogDetail(log.id);
                                                }
                                            }}
                                            className={cn(
                                                'group flex flex-col gap-1 rounded-xl border p-2 text-xs transition-all cursor-pointer select-none',
                                                hasError
                                                    ? 'border-destructive/30 bg-destructive/5 hover:bg-destructive/10'
                                                    : 'border-border/60 bg-card hover:border-primary/40 hover:bg-accent/40',
                                            )}
                                        >
                                            <div className="flex items-center justify-between gap-1.5 min-w-0">
                                                <div className="flex items-center gap-1.5 min-w-0">
                                                    {hasError ? (
                                                        <Badge variant="destructive" className="h-4 px-1 text-[9px] gap-0.5 shrink-0 font-medium">
                                                            <AlertCircle className="size-2.5" />
                                                            {t('logs.failed') ?? '失败'}
                                                        </Badge>
                                                    ) : (
                                                        <Badge className="h-4 px-1 text-[9px] gap-0.5 shrink-0 bg-primary/15 text-primary border-0 font-medium">
                                                            <Check className="size-2.5" />
                                                            {t('logs.success') ?? '成功'}
                                                        </Badge>
                                                    )}
                                                    <span className="font-medium text-foreground truncate max-w-[130px]">
                                                        {log.actual_model_name || log.request_model_name}
                                                    </span>
                                                </div>
                                                <div className="flex items-center gap-1 text-[11px] tabular-nums text-muted-foreground shrink-0">
                                                    <span>{formatDuration(log.use_time)}</span>
                                                    <ChevronRight className="size-3 text-muted-foreground/40 group-hover:text-foreground transition-transform group-hover:translate-x-0.5" />
                                                </div>
                                            </div>

                                            <div className="flex items-center justify-between text-[10px] text-muted-foreground">
                                                <span className="truncate max-w-[150px]">
                                                    {log.channel_name || `Channel ${log.channel}`}
                                                </span>
                                                <span className="tabular-nums">
                                                    {formatLogTime(log.time)}
                                                </span>
                                            </div>

                                            {hasError ? (
                                                <div className="mt-0.5 text-[10px] text-destructive truncate">
                                                    {sanitizeErrorMessage(log.error)}
                                                </div>
                                            ) : null}
                                        </div>
                                    );
                                })}
                            </div>
                        )}
                    </section>
                </div>
            </div>
        </TooltipProvider>
    );
}
