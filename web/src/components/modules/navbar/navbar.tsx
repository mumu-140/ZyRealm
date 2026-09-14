"use client"

import { motion } from "motion/react"
import { cn } from "@/lib/utils"
import { useNavStore, type NavItem } from "@/components/modules/navbar"
import { ROUTES } from "@/route/config"
import { usePreload } from "@/route/use-preload"
import { ENTRANCE_VARIANTS } from "@/lib/animations/fluid-transitions"
import { useTranslations } from "next-intl"
import Logo from "@/components/modules/logo"

export function NavBar() {
    const { activeItem, setActiveItem } = useNavStore()
    const { preload } = usePreload()
    const t = useTranslations('navbar')

    return (
        <div className="relative z-50 md:h-full">
            <motion.nav
                aria-label={t('navigation')}
                className={cn(
                    "fixed bottom-2 left-1/2 flex max-w-[calc(100vw-1rem)] -translate-x-1/2 items-center gap-1 overflow-x-auto rounded-3xl border border-sidebar-border/80 bg-sidebar/90 p-2 text-sidebar-foreground backdrop-blur-2xl",
                    "md:sticky md:top-4 md:left-auto md:bottom-auto md:h-[calc(100dvh-2rem)] md:w-[240px] md:max-w-none md:translate-x-0 md:flex-col md:items-stretch md:gap-1.5 md:overflow-visible md:rounded-[28px] md:p-4 md:border-sidebar-border/70 md:bg-sidebar/85",
                    "custom-shadow"
                )}
                variants={ENTRANCE_VARIANTS.navbar}
                initial="initial"
                animate="animate"
            >
                {/* Brand Header */}
                <div className="hidden items-center gap-3 px-2 pb-3 pt-1 md:flex">
                    <div className="relative flex size-11 shrink-0 items-center justify-center rounded-2xl border border-primary/25 bg-gradient-to-b from-primary/15 to-primary/5 shadow-inner">
                        <Logo size={30} />
                        <span className="absolute -top-0.5 -right-0.5 flex size-2.5">
                            <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-400 opacity-75" />
                            <span className="relative inline-flex size-2.5 rounded-full bg-emerald-500" />
                        </span>
                    </div>
                    <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-1.5">
                            <span className="truncate text-[15px] font-bold tracking-tight">ZyRealm</span>
                            <span className="rounded-md bg-primary/10 px-1.5 py-0.5 text-[10px] font-semibold text-primary">Live</span>
                        </div>
                        <div className="truncate text-[11px] text-muted-foreground">自由界</div>
                    </div>
                </div>

                <div className="mb-2 hidden h-px bg-sidebar-border/70 md:block" />

                {/* Navigation Items */}
                <div className="flex items-center gap-1 md:flex-col md:items-stretch md:gap-1">
                    {ROUTES.map((route, index) => {
                        const isActive = activeItem === route.id;
                        const isSectionStart = index === 5; // Log begins Operations section

                        return (
                            <div key={route.id} className="flex flex-col md:w-full">
                                {isSectionStart && (
                                    <div className="my-1.5 hidden h-px bg-sidebar-border/60 md:block" />
                                )}
                                <motion.button
                                    type="button"
                                    onClick={() => setActiveItem(route.id as NavItem)}
                                    onMouseEnter={() => preload(route.id)}
                                    aria-label={t(route.id)}
                                    title={t(route.id)}
                                    aria-current={isActive ? 'page' : undefined}
                                    className={cn(
                                        "relative z-20 flex size-11 shrink-0 items-center justify-center rounded-2xl transition-all",
                                        "md:h-11 md:w-full md:justify-start md:gap-3 md:px-3.5",
                                        isActive
                                            ? "text-sidebar-primary-foreground font-semibold shadow-xs"
                                            : "text-sidebar-foreground/70 hover:bg-sidebar-accent/70 hover:text-sidebar-foreground font-medium"
                                    )}
                                    initial={{ opacity: 0, scale: 0.92 }}
                                    animate={{
                                        opacity: 1,
                                        scale: 1,
                                        transition: {
                                            delay: index * 0.03,
                                            duration: 0.22,
                                        }
                                    }}
                                    whileTap={{ scale: 0.97 }}
                                >
                                    {isActive && (
                                        <motion.div
                                            layoutId="navbar-indicator"
                                            className="absolute inset-0 z-0 rounded-2xl bg-sidebar-primary shadow-sm shadow-primary/20"
                                            transition={{ type: "spring", stiffness: 340, damping: 32 }}
                                        />
                                    )}
                                    <span className="relative z-10 flex items-center">
                                        <route.icon className="size-5" strokeWidth={isActive ? 2.2 : 1.8} />
                                    </span>
                                    <span className="relative z-10 hidden truncate text-sm md:block">
                                        {t(route.id)}
                                    </span>
                                </motion.button>
                            </div>
                        );
                    })}
                </div>

                {/* Bottom Status Widget */}
                <div className="mt-auto hidden md:block">
                    <div className="rounded-2xl border border-sidebar-border/80 bg-background/50 p-3 shadow-2xs backdrop-blur-sm">
                        <div className="flex items-center justify-between">
                            <div className="flex items-center gap-1.5 text-[11px] font-semibold text-foreground">
                                <span className="size-2 rounded-full bg-emerald-500 animate-pulse" />
                                <span>系统在线</span>
                            </div>
                            <span className="font-mono text-[10px] text-muted-foreground">35276</span>
                        </div>
                        <div className="mt-2 text-[10px] font-medium leading-4 text-muted-foreground">
                            Adaptive Routing · Provider / Credential / Capability
                        </div>
                    </div>
                </div>
            </motion.nav>
        </div>
    )
}
