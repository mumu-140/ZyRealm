'use client';

import {
    useStatsDaily,
    useStatsHourly,
    useStatsTotal,
} from '@/api/endpoints/stats';
import { Tabs, TabsList, TabsTrigger } from '@/components/animate-ui/components/animate/tabs';
import { AnimatedNumber } from '@/components/common/AnimatedNumber';
import { buildStatsChartSummary } from '@/components/modules/home/chart-summary';
import { useHomeViewStore, type ChartPeriod } from '@/components/modules/home/store';
import { formatMoneyLabel, type ChartPoint } from '@/components/modules/home/stats-window';
import { ChartContainer, ChartTooltip, ChartTooltipContent } from '@/components/ui/chart';
import { useMemo } from 'react';
import { useTranslations } from 'next-intl';
import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from 'recharts';

import { Activity, Clock, Cpu, DollarSign, TrendingUp } from 'lucide-react';

const PERIOD_KEY: Record<ChartPeriod, 'today' | 'last7Days' | 'last30Days' | 'allTime'> = {
    '1': 'today',
    '7': 'last7Days',
    '30': 'last30Days',
    all: 'allTime',
};

export function StatsChart() {
    const t = useTranslations('home.summary');
    const total = useStatsTotal();
    const daily = useStatsDaily();
    const hourly = useStatsHourly();
    const period = useHomeViewStore((state) => state.chartPeriod);
    const setChartPeriod = useHomeViewStore((state) => state.setChartPeriod);

    const sortedDaily = useMemo(
        () => [...(daily.data ?? [])].sort((left, right) => left.date.localeCompare(right.date)),
        [daily.data],
    );
    const summary = useMemo(
        () => buildStatsChartSummary(period, total.data, hourly.data, sortedDaily),
        [period, total.data, hourly.data, sortedDaily],
    );
    const statsError = total.isError || daily.isError || hourly.isError;
    const statsLoading = total.isLoading || daily.isLoading || hourly.isLoading;
    const suffix = heroUnitSuffix(summary.hero.unit);

    return (
        <section className="space-y-4" aria-busy={statsLoading}>
            {statsError && <StatsLoadError message={t('loadError')} />}

            {/* 4 Modern Overview KPI Metric Cards (New-API / One-API style) */}
            <div className="grid grid-cols-2 lg:grid-cols-4 gap-3 md:gap-4">
                {/* 1. Cost */}
                <div className="relative flex flex-col justify-between overflow-hidden rounded-2xl border border-card-border bg-card p-4 custom-shadow transition-all hover:border-border">
                    <div className="flex items-center justify-between">
                        <span className="text-xs font-medium text-muted-foreground truncate">{t('metrics.cost')}</span>
                        <div className="flex size-8 shrink-0 items-center justify-center rounded-xl bg-emerald-500/10 text-emerald-600 dark:text-emerald-400">
                            <DollarSign className="size-4" />
                        </div>
                    </div>
                    <div className="mt-3 flex items-baseline gap-0.5">
                        <span className="text-base font-semibold text-muted-foreground">$</span>
                        <span className="text-2xl sm:text-3xl font-bold tracking-tight text-foreground tabular-nums">
                            {summary.hero.value ? <AnimatedNumber value={summary.hero.value} /> : '0.00'}
                        </span>
                        {suffix && <span className="ml-1 text-xs text-muted-foreground">{suffix}</span>}
                    </div>
                    <div className="mt-2 flex items-center gap-1.5 text-[11px] text-muted-foreground">
                        <span className="inline-block size-1.5 rounded-full bg-emerald-500" />
                        <span className="truncate">{t(`headline.${PERIOD_KEY[period]}`)}</span>
                    </div>
                </div>

                {/* 2. Requests */}
                <div className="relative flex flex-col justify-between overflow-hidden rounded-2xl border border-card-border bg-card p-4 custom-shadow transition-all hover:border-border">
                    <div className="flex items-center justify-between">
                        <span className="text-xs font-medium text-muted-foreground truncate">{t('metrics.requests')}</span>
                        <div className="flex size-8 shrink-0 items-center justify-center rounded-xl bg-blue-500/10 text-blue-600 dark:text-blue-400">
                            <Activity className="size-4" />
                        </div>
                    </div>
                    <div className="mt-3 flex items-baseline gap-1">
                        <span className="text-2xl sm:text-3xl font-bold tracking-tight text-foreground tabular-nums">
                            <AnimatedNumber value={summary.metrics.requests.value} />
                        </span>
                        {summary.metrics.requests.unit && (
                            <span className="text-xs font-medium text-muted-foreground">{summary.metrics.requests.unit}</span>
                        )}
                    </div>
                    <div className="mt-2 flex items-center gap-1.5 text-[11px] text-muted-foreground">
                        <span className="inline-block size-1.5 rounded-full bg-blue-500" />
                        <span className="truncate">{t(`periods.${PERIOD_KEY[period]}`)}</span>
                    </div>
                </div>

                {/* 3. Tokens */}
                <div className="relative flex flex-col justify-between overflow-hidden rounded-2xl border border-card-border bg-card p-4 custom-shadow transition-all hover:border-border">
                    <div className="flex items-center justify-between">
                        <span className="text-xs font-medium text-muted-foreground truncate">{t('metrics.tokens')}</span>
                        <div className="flex size-8 shrink-0 items-center justify-center rounded-xl bg-purple-500/10 text-purple-600 dark:text-purple-400">
                            <Cpu className="size-4" />
                        </div>
                    </div>
                    <div className="mt-3 flex items-baseline gap-1">
                        <span className="text-2xl sm:text-3xl font-bold tracking-tight text-foreground tabular-nums">
                            <AnimatedNumber value={summary.metrics.tokens.value} />
                        </span>
                        {summary.metrics.tokens.unit && (
                            <span className="text-xs font-medium text-muted-foreground">{summary.metrics.tokens.unit}</span>
                        )}
                    </div>
                    <div className="mt-2 flex items-center gap-1.5 text-[11px] text-muted-foreground">
                        <span className="inline-block size-1.5 rounded-full bg-purple-500" />
                        <span className="truncate">{t(`periods.${PERIOD_KEY[period]}`)}</span>
                    </div>
                </div>

                {/* 4. Latency */}
                <div className="relative flex flex-col justify-between overflow-hidden rounded-2xl border border-card-border bg-card p-4 custom-shadow transition-all hover:border-border">
                    <div className="flex items-center justify-between">
                        <span className="text-xs font-medium text-muted-foreground truncate">{t('metrics.waitTime')}</span>
                        <div className="flex size-8 shrink-0 items-center justify-center rounded-xl bg-amber-500/10 text-amber-600 dark:text-amber-400">
                            <Clock className="size-4" />
                        </div>
                    </div>
                    <div className="mt-3 flex items-baseline gap-1">
                        <span className="text-2xl sm:text-3xl font-bold tracking-tight text-foreground tabular-nums">
                            <AnimatedNumber value={summary.metrics.waitTime.value} />
                        </span>
                        {summary.metrics.waitTime.unit && (
                            <span className="text-xs font-medium text-muted-foreground">{summary.metrics.waitTime.unit}</span>
                        )}
                    </div>
                    <div className="mt-2 flex items-center gap-1.5 text-[11px] text-muted-foreground">
                        <span className="inline-block size-1.5 rounded-full bg-amber-500" />
                        <span className="truncate">{t(`periods.${PERIOD_KEY[period]}`)}</span>
                    </div>
                </div>
            </div>

            {/* Trend Chart Card */}
            <div className="rounded-3xl bg-card border-card-border border text-card-foreground p-5 custom-shadow">
                <header className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between pb-4">
                    <div>
                        <div className="flex items-center gap-2">
                            <TrendingUp className="size-4 text-primary" />
                            <h3 className="text-base font-semibold tracking-tight text-foreground">{t('chartTitle')}</h3>
                        </div>
                        <p className="text-xs text-muted-foreground mt-1">
                            {t(`headline.${PERIOD_KEY[period]}`)} · {t('periodLabel')}
                        </p>
                    </div>
                    <div className="max-w-full overflow-x-auto pb-0.5">
                        <Tabs value={period} onValueChange={(value) => setChartPeriod(value as ChartPeriod)}>
                            <TabsList aria-label={t('periodLabel')}>
                                <TabsTrigger value="1">{t('periods.today')}</TabsTrigger>
                                <TabsTrigger value="7">{t('periods.last7Days')}</TabsTrigger>
                                <TabsTrigger value="30">{t('periods.last30Days')}</TabsTrigger>
                                <TabsTrigger value="all">{t('periods.allTime')}</TabsTrigger>
                            </TabsList>
                        </Tabs>
                    </div>
                </header>
                <CostAreaChart data={summary.chartData} label={t('headline.allTime')} />
            </div>
        </section>
    );
}

function CostAreaChart({ data, label }: { data: ChartPoint[]; label: string }) {
    const config = { total_cost: { label } };
    return (
        <ChartContainer config={config} className="h-48 w-full">
            <AreaChart accessibilityLayer data={data}>
                <defs>
                    <linearGradient id="fillCost" x1="0" y1="0" x2="0" y2="1">
                        <stop offset="5%" stopColor="var(--chart-1)" stopOpacity={0.35} />
                        <stop offset="95%" stopColor="var(--chart-1)" stopOpacity={0.05} />
                    </linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" vertical={false} />
                <XAxis dataKey="date" tickLine={false} axisLine={false} />
                <YAxis tickLine={false} axisLine={false} tickFormatter={formatMoneyLabel} />
                <ChartTooltip cursor={false} content={<ChartTooltipContent indicator="line" />} />
                <Area
                    type="monotone"
                    dataKey="total_cost"
                    stroke="var(--chart-1)"
                    fill="url(#fillCost)"
                />
            </AreaChart>
        </ChartContainer>
    );
}

function StatsLoadError({ message }: { message: string }) {
    return (
        <div className="rounded-xl bg-destructive/10 px-3 py-2 text-xs text-destructive" role="status">
            {message}
        </div>
    );
}

function heroUnitSuffix(unit: string): string {
    if (!unit || unit === '$') {
        return '';
    }
    return unit.replace(/\$$/, '');
}

