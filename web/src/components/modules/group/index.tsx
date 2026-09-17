'use client';

import { useMemo } from 'react';
import { GroupCard } from './Card';
import { useGroupList } from '@/api/endpoints/group';
import { useSearchStore, useToolbarViewOptionsStore } from '@/components/modules/toolbar';
import { VirtualizedGrid } from '@/components/common/VirtualizedGrid';
import { GROUP_CARD_HEIGHT, GROUP_GRID_GAP } from './layout';

// 分组卡目标宽度: 一行 3-4 个 (较窄紧凑卡片)
function resolveGroupColumns(width: number): number {
    if (width >= 1260) return 4;
    if (width >= 920) return 3;
    if (width >= 580) return 2;
    return 1;
}

export function Group() {
    const { data: groups } = useGroupList();
    const pageKey = 'group' as const;
    const searchTerm = useSearchStore((s) => s.getSearchTerm(pageKey));
    const sortField = useToolbarViewOptionsStore((s) => s.getSortField(pageKey));
    const sortOrder = useToolbarViewOptionsStore((s) => s.getSortOrder(pageKey));

    const sortedGroups = useMemo(() => {
        if (!groups) return [];
        return [...groups].sort((a, b) => {
            // 置顶优先：pinned 组排在前面，组内按 pinned_at desc
            if (!!a.pinned !== !!b.pinned) return a.pinned ? -1 : 1;
            if (a.pinned && b.pinned) {
                const ta = a.pinned_at ? new Date(a.pinned_at).getTime() : 0;
                const tb = b.pinned_at ? new Date(b.pinned_at).getTime() : 0;
                if (ta !== tb) return tb - ta;
            }
            const diff = sortField === 'name'
                ? a.name.localeCompare(b.name)
                : (a.id || 0) - (b.id || 0);
            return sortOrder === 'asc' ? diff : -diff;
        });
    }, [groups, sortField, sortOrder]);

    const visibleGroups = useMemo(() => {
        const term = searchTerm.toLowerCase().trim();
        return !term ? sortedGroups : sortedGroups.filter((g) => g.name.toLowerCase().includes(term));
    }, [sortedGroups, searchTerm]);

    return (
        <VirtualizedGrid
            items={visibleGroups}
            columns={resolveGroupColumns}
            estimateItemHeight={GROUP_CARD_HEIGHT}
            gap={GROUP_GRID_GAP}
            measureRows={false}
            positionMode="transform"
            getItemKey={(group, index) => group.id ?? `group-${index}`}
            renderItem={(group) => <GroupCard group={group} />}
        />
    );
}
