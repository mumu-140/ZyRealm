'use client';

import { useState, useMemo, useCallback, useEffect, useRef } from 'react';
import { Trash2, X, Pencil, Pin, PinOff, Sparkles } from 'lucide-react';
import { motion, AnimatePresence } from 'motion/react';
import { type Group, useDeleteGroup, useUpdateGroup, useToggleGroupPin, useGroupAutoAdd } from '@/api/endpoints/group';
import { useLiveRequests } from '@/api/endpoints/live-request';
import type { LLMChannel } from '@/api/endpoints/model';
import { useTranslations } from 'next-intl';
import { cn } from '@/lib/utils';
import { toast } from '@/components/common/Toast';
import { CopyIconButton } from '@/components/common/CopyButton';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/animate-ui/components/animate/tooltip';
import type { SelectedMember } from './ItemList';
import { MemberList } from './ItemList';
import { GroupEditor, type GroupEditorValues } from './Editor';
import { GroupDiagnosticAction } from './health';
import { GroupLogsPanel } from './GroupLogsPanel';
import { matchesGroupName, modelChannelKey, MODE_LABELS } from './utils';
import { compressConfigPayload, GroupMode, type GroupUpdateRequest, normalizeGroupCompressConfig, normalizeGroupProtocolMode, normalizePreferredProtocols } from '@/api/endpoints/group';
import { PresetPopover } from './PresetPopover';
import { ProtocolPolicyPopover } from './ProtocolPolicyPopover';
import { buildGroupMemberChanges } from './groupMemberDiff';
import { GROUP_CARD_HEIGHT } from './layout';
import {
    MorphingDialog,
    MorphingDialogClose,
    MorphingDialogContainer,
    MorphingDialogContent,
    MorphingDialogDescription,
    MorphingDialogTitle,
    MorphingDialogTrigger,
    useMorphingDialog,
} from '@/components/ui/morphing-dialog';

interface EditDialogContentProps {
    group: Group;
    displayMembers: SelectedMember[];
    isSubmitting: boolean;
    onSubmit: (values: GroupEditorValues, onDone?: () => void) => void;
}

interface GroupCardProps {
    group: Group;
    modelChannelByKey: ReadonlyMap<string, LLMChannel>;
}

function EditDialogContent({ group, displayMembers, isSubmitting, onSubmit }: EditDialogContentProps) {
    const { setIsOpen } = useMorphingDialog();
    const t = useTranslations('group');
    const [mobileTab, setMobileTab] = useState<'config' | 'logs'>('config');

    return (
        <>
            <MorphingDialogTitle className="shrink-0">
                <header className="mb-3 flex items-center justify-between border-b border-border/60 pb-3">
                    <div className="flex items-center gap-2.5 min-w-0">
                        <h2 className="text-xl md:text-2xl font-bold text-card-foreground truncate">
                            {group.name}
                        </h2>
                        <span className="hidden sm:inline-block text-xs text-muted-foreground">
                            {t('detail.actions.edit')}
                        </span>
                    </div>

                    <div className="flex items-center gap-2 shrink-0">
                        <div className="flex lg:hidden rounded-lg bg-muted/60 p-0.5 text-xs">
                            <button
                                type="button"
                                onClick={() => setMobileTab('config')}
                                className={cn(
                                    'px-2.5 py-1 rounded-md font-medium transition-colors',
                                    mobileTab === 'config'
                                        ? 'bg-background text-foreground shadow-xs'
                                        : 'text-muted-foreground hover:text-foreground',
                                )}
                            >
                                {t('logs.tabConfig') ?? '配置'}
                            </button>
                            <button
                                type="button"
                                onClick={() => setMobileTab('logs')}
                                className={cn(
                                    'px-2.5 py-1 rounded-md font-medium transition-colors',
                                    mobileTab === 'logs'
                                        ? 'bg-background text-foreground shadow-xs'
                                        : 'text-muted-foreground hover:text-foreground',
                                )}
                            >
                                {t('logs.tabLogs') ?? '日志'}
                            </button>
                        </div>
                        <MorphingDialogClose className="relative right-0 top-0" />
                    </div>
                </header>
            </MorphingDialogTitle>
            <MorphingDialogDescription className="flex-1 min-h-0 overflow-hidden">
                <div className="flex flex-col lg:flex-row gap-5 h-full min-h-0 overflow-hidden">
                    <div
                        className={cn(
                            'flex-1 min-w-0 min-h-0 flex-col overflow-hidden',
                            mobileTab === 'config' ? 'flex' : 'hidden lg:flex',
                        )}
                    >
                        <GroupEditor
                            key={`edit-group-${group.id}`}
                            initial={{
                                name: group.name,
                                match_regex: group.match_regex ?? '',
                                mode: group.mode,
                                first_token_time_out: group.first_token_time_out ?? 0,
                                session_keep_time: group.session_keep_time ?? 0,
                                retry_enabled: group.retry_enabled ?? false,
                                max_retries: group.max_retries ?? 3,
                                protocol_mode: normalizeGroupProtocolMode(group.protocol_mode),
                                preferred_protocols: normalizePreferredProtocols(group.preferred_protocols),
                                compress_config: normalizeGroupCompressConfig(group.compress_config),
                                members: displayMembers,
                            }}
                            submitText={t('detail.actions.save')}
                            submittingText={t('create.submitting')}
                            isSubmitting={isSubmitting}
                            onCancel={() => setIsOpen(false)}
                            onSubmit={(v) => onSubmit(v, () => setIsOpen(false))}
                        />
                    </div>

                    <aside
                        className={cn(
                            'w-full lg:w-[380px] xl:w-[440px] shrink-0 min-h-0 flex-col overflow-hidden rounded-2xl border border-border/50 bg-muted/20 p-3.5',
                            mobileTab === 'logs' ? 'flex' : 'hidden lg:flex',
                        )}
                    >
                        <GroupLogsPanel group={group} onCloseDialog={() => setIsOpen(false)} />
                    </aside>
                </div>
            </MorphingDialogDescription>
        </>
    );
}

export function GroupCard({ group, modelChannelByKey }: GroupCardProps) {
    const t = useTranslations('group');
    const updateGroup = useUpdateGroup();
    const deleteGroup = useDeleteGroup();
    const togglePin = useToggleGroupPin();
    const autoAdd = useGroupAutoAdd();
    const liveQuery = useLiveRequests();

    const [confirmDelete, setConfirmDelete] = useState(false);
    const [isDragging, setIsDragging] = useState(false);
    const [members, setMembers] = useState<SelectedMember[]>([]);
    const [weightOverrides, setWeightOverrides] = useState<Record<string, number>>({});
    const weightTimerRef = useRef<NodeJS.Timeout | null>(null);
    const membersRef = useRef<SelectedMember[]>([]);

    const activeCount = useMemo(() => {
        const list = liveQuery.data?.requests ?? [];
        const channelIds = new Set((group.items || []).map((i) => i.channel_id));
        return list.filter((r) => matchesGroupName(r.requested_model, group.name, group.match_regex) || (r.channel_id > 0 && channelIds.has(r.channel_id))).length;
    }, [group.items, group.match_regex, group.name, liveQuery.data?.requests]);

    const displayMembers = useMemo((): SelectedMember[] =>
        [...(group.items || [])]
            .sort((a, b) => a.priority - b.priority)
            .map((item) => {
                const key = modelChannelKey(item.channel_id, item.model_name);
                const modelChannel = modelChannelByKey.get(key);
                return {
                    ...modelChannel,
                    id: key,
                    name: item.model_name,
                    enabled: modelChannel?.enabled ?? true,
                    channel_id: item.channel_id,
                    channel_name: modelChannel?.channel_name ?? `Channel ${item.channel_id}`,
                    item_id: item.id,
                    weight: item.weight,
                };
            }),
        [group.items, modelChannelByKey]
    );

    const effectiveDisplayMembers = useMemo(
        () => displayMembers.map((member) => {
            const nextWeight = weightOverrides[member.id];
            return nextWeight === undefined ? member : { ...member, weight: nextWeight };
        }),
        [displayMembers, weightOverrides]
    );

    const renderedMembers = useMemo(
        () => isDragging || updateGroup.isPending ? members : effectiveDisplayMembers,
        [effectiveDisplayMembers, isDragging, updateGroup.isPending, members]
    );

    useEffect(() => {
        membersRef.current = renderedMembers;
    }, [renderedMembers]);

    useEffect(() => {
        return () => { if (weightTimerRef.current) clearTimeout(weightTimerRef.current); };
    }, []);

    const onSuccess = useCallback(() => toast.success(t('toast.updated')), [t]);
    const onError = useCallback((error: Error) => toast.error(t('toast.updateFailed'), { description: error.message }), [t]);

    // Avoid UI flicker: drag-reorder also uses the same mutation, so only "mode switch" should lock mode buttons.
    const isUpdatingMode = (() => {
        if (!updateGroup.isPending) return false;
        const v = updateGroup.variables;
        if (typeof v !== 'object' || v === null) return false;
        return 'mode' in v && typeof (v as { mode?: unknown }).mode === 'number';
    })();

    const priorityByItemId = useMemo(() => {
        const map = new Map<number, number>();
        (group.items || []).forEach((item) => {
            if (item.id !== undefined) map.set(item.id, item.priority);
        });
        return map;
    }, [group.items]);

    const clearWeightOverride = useCallback((id: string) => {
        setWeightOverrides((prev) => {
            if (!(id in prev)) return prev;
            const next = { ...prev };
            delete next[id];
            return next;
        });
    }, []);

    const handleDragStart = useCallback(() => {
        setMembers([...effectiveDisplayMembers]);
        setIsDragging(true);
    }, [effectiveDisplayMembers]);

    const handleDragFinish = useCallback(() => {
        setIsDragging(false);
    }, []);

    const handleDropReorder = useCallback((nextMembers: SelectedMember[]) => {
        const itemsToUpdate = nextMembers
            .map((m, i) => ({ member: m, newPriority: i + 1 }))
            .filter(({ member, newPriority }) => {
                if (!member.item_id) return false;
                const origPriority = priorityByItemId.get(member.item_id);
                return origPriority !== undefined && origPriority !== newPriority;
            })
            .map(({ member, newPriority }) => ({ id: member.item_id!, priority: newPriority, weight: member.weight ?? 1 }));
        if (itemsToUpdate.length > 0) updateGroup.mutate({ id: group.id!, items_to_update: itemsToUpdate }, { onSuccess, onError });
    }, [group.id, priorityByItemId, updateGroup, onSuccess, onError]);

    const handleRemoveMember = useCallback((id: string) => {
        const member = membersRef.current.find((m) => m.id === id);
        clearWeightOverride(id);
        if (member?.item_id !== undefined) updateGroup.mutate({ id: group.id!, items_to_delete: [member.item_id] }, { onSuccess, onError });
    }, [clearWeightOverride, group.id, updateGroup, onSuccess, onError]);

    const handleWeightChange = useCallback((id: string, weight: number) => {
        setWeightOverrides((prev) => ({ ...prev, [id]: weight }));
        if (isDragging) {
            setMembers((prev) => prev.map((m) => m.id === id ? { ...m, weight } : m));
        }
        if (weightTimerRef.current) clearTimeout(weightTimerRef.current);
        weightTimerRef.current = setTimeout(() => {
            const member = membersRef.current.find((m) => m.id === id);
            if (!member?.item_id) return;
            const priority = priorityByItemId.get(member.item_id);
            if (!priority) return;
            updateGroup.mutate(
                { id: group.id!, items_to_update: [{ id: member.item_id, priority, weight }] },
                {
                    onSuccess: () => {
                        clearWeightOverride(id);
                        onSuccess();
                    },
                    onError,
                }
            );
        }, 500);
    }, [clearWeightOverride, group.id, isDragging, priorityByItemId, updateGroup, onSuccess, onError]);

    const handleSubmitEdit = useCallback((values: GroupEditorValues, onDone?: () => void) => {
        if (!group.id) return;

        const { items_to_add, items_to_update, items_to_delete } = buildGroupMemberChanges(
            group.items || [],
            values.members,
        );

        const payload: GroupUpdateRequest = { id: group.id };
        const nextName = values.name.trim();
        const nextRegex = (values.match_regex ?? '').trim();
        const nextFirstTokenTimeOut = values.first_token_time_out ?? 0;
        const nextSessionKeepTime = values.session_keep_time ?? 0;

        if (nextName && nextName !== group.name) payload.name = nextName;
        if (values.mode !== group.mode) payload.mode = values.mode;
        if (nextRegex !== (group.match_regex ?? '')) payload.match_regex = nextRegex;
        if (nextFirstTokenTimeOut !== (group.first_token_time_out ?? 0)) payload.first_token_time_out = nextFirstTokenTimeOut;
        if (nextSessionKeepTime !== (group.session_keep_time ?? 0)) payload.session_keep_time = nextSessionKeepTime;
        if (values.retry_enabled !== (group.retry_enabled ?? false)) payload.retry_enabled = values.retry_enabled;
        if (values.max_retries !== (group.max_retries ?? 3)) payload.max_retries = values.max_retries;
        if (values.protocol_mode !== normalizeGroupProtocolMode(group.protocol_mode)) payload.protocol_mode = values.protocol_mode;
        const nextPreferred = values.preferred_protocols ?? [];
        const prevPreferred = normalizePreferredProtocols(group.preferred_protocols);
        if (JSON.stringify(nextPreferred) !== JSON.stringify(prevPreferred)) payload.preferred_protocols = nextPreferred;
        // 压缩配置:整体替换;关闭(values.compress_config undefined)且曾开启过时显式发 enabled:false。
        const nextCompressPayload = compressConfigPayload(
            normalizeGroupCompressConfig(group.compress_config),
            values.compress_config,
        );
        if (nextCompressPayload !== undefined) payload.compress_config = nextCompressPayload;
        if (items_to_add.length) payload.items_to_add = items_to_add;
        if (items_to_update.length) payload.items_to_update = items_to_update;
        if (items_to_delete.length) payload.items_to_delete = items_to_delete;

        if (Object.keys(payload).length === 1) {
            onDone?.();
            return;
        }

        updateGroup.mutate(payload, {
            onSuccess: () => {
                onSuccess();
                onDone?.();
            },
            onError,
        });
    }, [group.first_token_time_out, group.session_keep_time, group.retry_enabled, group.max_retries, group.protocol_mode, group.preferred_protocols, group.compress_config, group.id, group.items, group.match_regex, group.mode, group.name, onSuccess, onError, updateGroup]);

    return (
        <article
            style={{ height: GROUP_CARD_HEIGHT }}
            className="relative group/card flex min-h-0 flex-col overflow-hidden rounded-2xl border border-border bg-card text-card-foreground p-3 custom-shadow"
        >
            <header className="flex items-start justify-between mb-3 relative overflow-visible rounded-xl -mx-1 px-1 -my-1 py-1">
                <div className="relative flex-1 mr-2 min-w-0 group/title flex items-center gap-1.5">
                    <Tooltip side="top" sideOffset={10} align="center">
                        <TooltipTrigger asChild>
                            <h3 className="text-base font-bold truncate">{group.name}</h3>
                        </TooltipTrigger>
                        <TooltipContent key={group.name}>{group.name}</TooltipContent>
                    </Tooltip>
                    {activeCount > 0 ? (
                        <span className="inline-flex items-center gap-1 rounded-full bg-emerald-500/15 px-1.5 py-0.5 text-[10px] font-semibold text-emerald-600 dark:text-emerald-400 tabular-nums shrink-0">
                            <span className="size-1.5 rounded-full bg-emerald-500 animate-pulse" />
                            {activeCount}
                        </span>
                    ) : null}
                </div>

                <div className="flex items-center gap-1.5 shrink-0">
                    <Tooltip side="top" sideOffset={10} align="center">
                        <TooltipTrigger>
                            <CopyIconButton
                                text={group.name}
                                className="flex size-8 items-center justify-center rounded-lg border border-transparent transition-all hover:border-border hover:bg-muted active:scale-95 text-muted-foreground hover:text-foreground"
                                copyIconClassName="size-4"
                                checkIconClassName="size-4 text-primary"
                            />
                        </TooltipTrigger>
                        <TooltipContent>{t('detail.actions.copyName')}</TooltipContent>
                    </Tooltip>

                    <Tooltip side="top" sideOffset={10} align="center">
                        <TooltipTrigger asChild>
                            <button
                                type="button"
                                aria-label={t('autoAdd.action')}
                                disabled={autoAdd.isPending || !group.id}
                                onClick={() => {
                                    if (!group.id || autoAdd.isPending) return;
                                    autoAdd.mutate(group.id, {
                                        onSuccess: (result) => {
                                            if (result.added > 0) {
                                                toast.success(t('autoAdd.added', { count: result.added }));
                                            } else {
                                                toast.info(t('autoAdd.noNew'));
                                            }
                                        },
                                        onError: (error: Error) => {
                                            toast.error(t('autoAdd.failed'), { description: error.message });
                                        },
                                    });
                                }}
                                className="flex size-8 items-center justify-center rounded-lg border border-transparent transition-all hover:border-border hover:bg-muted active:scale-95 text-muted-foreground hover:text-foreground disabled:pointer-events-none disabled:opacity-50"
                            >
                                <Sparkles className="size-4" />
                            </button>
                        </TooltipTrigger>
                        <TooltipContent>{t('autoAdd.action')}</TooltipContent>
                    </Tooltip>

                    <GroupDiagnosticAction groupId={group.id} />

                    <PresetPopover group={group} />

                    <ProtocolPolicyPopover group={group} />

                    <MorphingDialog>
                        <MorphingDialogTrigger className="flex size-8 items-center justify-center rounded-lg border border-transparent transition-all hover:border-border hover:bg-muted active:scale-95 text-muted-foreground hover:text-foreground">
                            <Tooltip side="top" sideOffset={10} align="center">
                                <TooltipTrigger asChild>
                                    <Pencil className="size-4" />
                                </TooltipTrigger>
                                <TooltipContent>{t('detail.actions.edit')}</TooltipContent>
                            </Tooltip>
                        </MorphingDialogTrigger>

                        <MorphingDialogContainer>
                            <MorphingDialogContent className="relative w-screen max-w-full md:max-w-5xl lg:max-w-7xl xl:max-w-[1440px] bg-card text-card-foreground px-5 md:px-6 py-4 rounded-3xl h-[calc(100vh-2rem)] flex flex-col overflow-hidden">
                                <EditDialogContent
                                    group={group}
                                    displayMembers={displayMembers}
                                    isSubmitting={updateGroup.isPending}
                                    onSubmit={handleSubmitEdit}
                                />
                            </MorphingDialogContent>
                        </MorphingDialogContainer>
                    </MorphingDialog>
                </div>
            </header>

            {/* Mode: quick switch (no need to enter Edit) */}
            {/* Mode: quick switch — flex-wrap 让按钮按内容自适应宽度，自然换行，不溢出不截断。*/}
            <div className="flex flex-wrap gap-1.5 mb-3">
                {([GroupMode.RoundRobin, GroupMode.Random, GroupMode.Failover, GroupMode.Weighted, GroupMode.HealthFirst, GroupMode.LeastUsed, GroupMode.P2C, GroupMode.StrictRandom] as const).map((m) => (
                    <button
                        key={m}
                        type="button"
                        aria-disabled={isUpdatingMode || !group.id}
                        onClick={() => {
                            if (isUpdatingMode || !group.id) return;
                            if (m === group.mode) return;
                            updateGroup.mutate({ id: group.id!, mode: m }, { onSuccess, onError });
                        }}
                        className={cn(
                            'shrink-0 px-2 py-1 text-xs leading-tight whitespace-nowrap text-nowrap rounded-lg transition-colors font-medium',
                            group.mode === m ? 'bg-primary text-primary-foreground shadow-xs' : 'bg-muted/70 hover:bg-muted text-muted-foreground hover:text-foreground',
                            // Keep visuals stable (no opacity/disabled flicker) while still preventing double-submit via onClick guard.
                            (!group.id) && 'cursor-not-allowed opacity-50'
                        )}
                    >
                        <span className="whitespace-nowrap text-nowrap">{t(`mode.${MODE_LABELS[m]}`)}</span>
                    </button>
                ))}
            </div>

            <section className="relative min-h-0 flex-1 overflow-hidden rounded-lg border border-border/50 bg-muted/30">
                <MemberList
                    members={renderedMembers}
                    onReorder={setMembers}
                    onRemove={handleRemoveMember}
                    onWeightChange={handleWeightChange}
                    onDragStart={handleDragStart}
                    onDrop={handleDropReorder}
                    onDragFinish={handleDragFinish}
                    autoScrollOnAdd={false}
                    showWeight={group.mode === GroupMode.Weighted}
                    layoutScope={`card-${group.id ?? 'unknown'}`}
                />
            </section>

            {/* Floating secondary actions: hidden by default, appear on card hover/focus */}
            {!confirmDelete && (
                <div
                    className={cn(
                        'absolute left-2 bottom-2 z-10 flex items-center gap-0.5 rounded-lg bg-card/95 backdrop-blur-sm border border-border/40 shadow-sm p-0.5 transition-opacity duration-200',
                        'opacity-0 pointer-events-none group-hover/card:opacity-100 group-hover/card:pointer-events-auto group-focus-within/card:opacity-100 group-focus-within/card:pointer-events-auto',
                    )}
                >
                    <Tooltip side="top" sideOffset={6} align="center">
                        <TooltipTrigger asChild>
                            <button
                                type="button"
                                disabled={togglePin.isPending || !group.id}
                                onClick={() => {
                                    if (!group.id || togglePin.isPending) return;
                                    togglePin.mutate(
                                        { groupID: group.id, pinned: !group.pinned },
                                        {
                                            onSuccess: () => toast.success(group.pinned ? t('toast.unpinned') : t('toast.pinned')),
                                            onError: (e) => toast.error(t('toast.pinFailed'), { description: e.message }),
                                        },
                                    );
                                }}
                                className={cn(
                                    'flex size-8 items-center justify-center rounded-lg border border-transparent transition-all hover:border-border hover:bg-muted active:scale-95 disabled:opacity-50 disabled:pointer-events-none',
                                    group.pinned ? 'text-primary' : 'text-muted-foreground hover:text-foreground',
                                )}
                            >
                                {group.pinned ? <Pin className="size-4 fill-current" /> : <PinOff className="size-4" />}
                            </button>
                        </TooltipTrigger>
                        <TooltipContent>{group.pinned ? t('pin.unpin') : t('pin.pin')}</TooltipContent>
                    </Tooltip>

                    <Tooltip side="top" sideOffset={6} align="center">
                        <TooltipTrigger asChild>
                            <motion.button
                                layoutId={`delete-btn-group-${group.id}`}
                                type="button"
                                onClick={() => setConfirmDelete(true)}
                                className="flex size-8 items-center justify-center rounded-lg border border-transparent transition-all hover:border-destructive/30 hover:bg-destructive/10 active:scale-95 text-muted-foreground hover:text-destructive"
                            >
                                <Trash2 className="size-4" />
                            </motion.button>
                        </TooltipTrigger>
                        <TooltipContent>{t('detail.actions.delete')}</TooltipContent>
                    </Tooltip>
                </div>
            )}

            <AnimatePresence>
                {confirmDelete && (
                    <motion.div
                        layoutId={`delete-btn-group-${group.id}`}
                        className="absolute left-3 bottom-3 z-10 flex items-center gap-2 bg-destructive p-2 rounded-xl shadow-md"
                        transition={{ type: 'spring', stiffness: 400, damping: 30 }}
                    >
                        <button
                            type="button"
                            onClick={() => setConfirmDelete(false)}
                            className="flex h-7 w-7 items-center justify-center rounded-lg bg-destructive-foreground/20 text-destructive-foreground transition-all hover:bg-destructive-foreground/30 active:scale-95"
                        >
                            <X className="size-4" />
                        </button>
                        <button
                            type="button"
                            onClick={() => group.id && deleteGroup.mutate(group.id, {
                                onSuccess: () => toast.success(t('toast.deleted')),
                                onError: (e) => toast.error(t('toast.deleteFailed'), { description: e.message }),
                            })}
                            disabled={deleteGroup.isPending}
                            className="h-7 px-3 flex items-center justify-center gap-2 rounded-lg bg-destructive-foreground text-destructive text-sm font-semibold transition-all hover:bg-destructive-foreground/90 active:scale-[0.98] disabled:opacity-50 disabled:cursor-not-allowed"
                        >
                            <Trash2 className="size-3.5" />
                            {t('detail.actions.confirmDelete')}
                        </button>
                    </motion.div>
                )}
            </AnimatePresence>
        </article>
    );
}
