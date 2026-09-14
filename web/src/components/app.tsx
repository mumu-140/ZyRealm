'use client';

import { useState, useEffect, useRef } from 'react';
import { motion, AnimatePresence } from "motion/react"
import { useAuth } from '@/api/endpoints/user';
import { LoginForm } from '@/components/modules/login';
import { APIKeyDashboard } from '@/components/modules/apikey-dashboard';
import { ContentLoader } from '@/route/content-loader';
import { NavBar, useNavStore } from '@/components/modules/navbar';
import { useTranslations } from 'next-intl'
import Logo, { LOGO_DRAW_END_MS } from '@/components/modules/logo';
import { Toolbar } from '@/components/modules/toolbar';
import { ChannelTabSwitcher } from '@/components/modules/channel/TabSwitcher';
import { ProxyPoolDialog } from '@/components/modules/proxy-pool/ProxyPoolDialog';
import { ENTRANCE_VARIANTS } from '@/lib/animations/fluid-transitions';
import { useQueryClient } from '@tanstack/react-query';
import { CONTENT_MAP } from '@/route';
import { apiClient } from '@/api/client';
import { logger } from '@/lib/logger';

const RETURNING_USER_KEY = 'zyrealm_visited';
const RETURNING_LOGO_MS = 300;

export function AppContainer() {
    const { isAuthenticated, isAPIKeyAuth, isLoading: authLoading } = useAuth();
    const { activeItem, direction } = useNavStore();
    const t = useTranslations('navbar');
    const queryClient = useQueryClient();
    const [logoAnimationComplete, setLogoAnimationComplete] = useState(false);
    const bootstrapStartedRef = useRef(false);

    useEffect(() => {
        const el = document.getElementById('initial-loader');
        if (!el) return;
        el.classList.add('zy-hide');
        const timer = setTimeout(() => el.remove(), 220);
        return () => clearTimeout(timer);
    }, []);

    useEffect(() => {
        const isReturning = sessionStorage.getItem(RETURNING_USER_KEY) === '1';
        const duration = isReturning ? RETURNING_LOGO_MS : LOGO_DRAW_END_MS;
        const timer = setTimeout(() => {
            setLogoAnimationComplete(true);
            sessionStorage.setItem(RETURNING_USER_KEY, '1');
        }, duration);
        return () => clearTimeout(timer);
    }, []);

    useEffect(() => {
        if (authLoading || !isAuthenticated || bootstrapStartedRef.current) return;
        bootstrapStartedRef.current = true;

        const prefetches: Array<Promise<unknown>> = [];

        if (isAPIKeyAuth) {
            prefetches.push(queryClient.prefetchQuery({
                queryKey: ['apikey', 'dashboard', 'stats'],
                queryFn: async () => apiClient.get('/api/v1/apikey/stats'),
            }));
        } else {
            const component = CONTENT_MAP[activeItem];
            if (component?.preload) prefetches.push(component.preload());

            switch (activeItem) {
                case 'home':
                    prefetches.push(queryClient.prefetchQuery({ queryKey: ['stats', 'total'], queryFn: async () => apiClient.get('/api/v1/stats/total') }));
                    prefetches.push(queryClient.prefetchQuery({ queryKey: ['stats', 'daily'], queryFn: async () => apiClient.get('/api/v1/stats/daily') }));
                    prefetches.push(queryClient.prefetchQuery({ queryKey: ['stats', 'hourly'], queryFn: async () => apiClient.get('/api/v1/stats/hourly') }));
                    prefetches.push(queryClient.prefetchQuery({ queryKey: ['stats', 'leaderboard', 'channel', '7'], queryFn: async () => apiClient.get('/api/v1/stats/leaderboard', { dimension: 'channel', window: '7' }) }));
                    break;
                case 'site':
                    prefetches.push(queryClient.prefetchQuery({ queryKey: ['sites', 'list'], queryFn: async () => apiClient.get('/api/v1/site/list') }));
                    break;
                case 'channel':
                    prefetches.push(queryClient.prefetchQuery({ queryKey: ['channels', 'list'], queryFn: async () => apiClient.get('/api/v1/channel/list') }));
                    break;
                case 'group':
                    prefetches.push(queryClient.prefetchQuery({ queryKey: ['groups', 'list'], queryFn: async () => apiClient.get('/api/v1/group/list') }));
                    prefetches.push(queryClient.prefetchQuery({ queryKey: ['models', 'channel'], queryFn: async () => apiClient.get('/api/v1/model/channel') }));
                    break;
                case 'model':
                    prefetches.push(queryClient.prefetchQuery({ queryKey: ['models', 'list'], queryFn: async () => apiClient.get('/api/v1/model/list') }));
                    break;
                case 'setting':
                    prefetches.push(queryClient.prefetchQuery({ queryKey: ['apikeys', 'list'], queryFn: async () => apiClient.get('/api/v1/apikey/list') }));
                    break;
                default:
                    break;
            }
        }

        Promise.allSettled(prefetches).catch((e) => logger.warn('bootstrap prefetch failed:', e));
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [authLoading, isAuthenticated]);

    const isLoading = authLoading || !logoAnimationComplete;

    if (isLoading) {
        return (
            <div className="flex min-h-screen items-center justify-center bg-background">
                <div className="flex size-32 items-center justify-center rounded-[32px] border border-border/70 bg-card/70 shadow-xl backdrop-blur-xl">
                    <Logo size={84} animate />
                </div>
            </div>
        );
    }

    if (isAPIKeyAuth) {
        return (
            <AnimatePresence mode="wait">
                <APIKeyDashboard key="apikey-dashboard" />
            </AnimatePresence>
        );
    }

    if (!isAuthenticated) {
        return (
            <AnimatePresence mode="wait">
                <LoginForm key="login" />
            </AnimatePresence>
        );
    }

    return (
        <motion.div
            key="main-app"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            transition={{ duration: 0.3 }}
            className="mx-auto h-dvh w-full max-w-[1920px] overflow-hidden p-3 pb-20 md:grid md:grid-cols-[240px_minmax(0,1fr)] md:gap-5 md:p-4"
        >
            <NavBar />
            <main className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden md:rounded-[28px] md:border md:border-border/70 md:bg-card/45 md:backdrop-blur-xl">
                <header className="flex flex-none items-start gap-x-3 px-2 py-4 md:px-6 md:py-5">
                    <div className="flex size-11 shrink-0 items-center justify-center rounded-2xl border border-primary/20 bg-primary/10 md:hidden">
                        <Logo size={29} />
                    </div>
                    <div className="min-w-0 flex-1 overflow-hidden">
                        <div className="mb-1 text-[10px] font-semibold uppercase tracking-[0.2em] text-primary">ZyRealm Control Plane</div>
                        <AnimatePresence mode="wait" custom={direction}>
                            <motion.div
                                key={activeItem}
                                custom={direction}
                                variants={{
                                    initial: (direction: number) => ({ y: 22 * direction, opacity: 0 }),
                                    animate: { y: 0, opacity: 1 },
                                    exit: (direction: number) => ({ y: -22 * direction, opacity: 0 })
                                }}
                                initial="initial"
                                animate="animate"
                                exit="exit"
                                transition={{ duration: 0.24 }}
                                className="flex flex-col gap-2 sm:flex-row sm:items-baseline sm:gap-6"
                            >
                                <span className="truncate text-2xl font-semibold tracking-tight md:text-3xl">{t(activeItem)}</span>
                                {activeItem === 'channel' && <ChannelTabSwitcher />}
                            </motion.div>
                        </AnimatePresence>
                    </div>
                    <div className="relative ml-auto flex min-h-[36px] items-center gap-3">
                        <Toolbar />
                    </div>
                    <ProxyPoolDialog />
                </header>

                <div className="mx-2 h-px flex-none bg-border/70 md:mx-6" />

                <AnimatePresence mode="wait" initial={false}>
                    <motion.div
                        key={activeItem}
                        variants={ENTRANCE_VARIANTS.content}
                        initial="initial"
                        animate="animate"
                        exit={{ opacity: 0, scale: 0.99 }}
                        transition={{ duration: 0.22 }}
                        className="h-full min-h-0 flex-1 px-0 pt-3 md:px-3 md:pb-3"
                    >
                        <ContentLoader activeRoute={activeItem} />
                    </motion.div>
                </AnimatePresence>
            </main>
        </motion.div>
    );
}
