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
                    "fixed bottom-2 left-1/2 flex max-w-[calc(100vw-1rem)] -translate-x-1/2 items-center gap-1 overflow-x-auto rounded-3xl border border-sidebar-border bg-sidebar/90 p-2 text-sidebar-foreground backdrop-blur-xl",
                    "md:sticky md:top-4 md:left-auto md:bottom-auto md:h-[calc(100dvh-2rem)] md:w-[240px] md:max-w-none md:translate-x-0 md:flex-col md:items-stretch md:gap-1 md:overflow-visible md:rounded-[28px] md:p-4",
                    "custom-shadow"
                )}
                variants={ENTRANCE_VARIANTS.navbar}
                initial="initial"
                animate="animate"
            >
                <div className="hidden items-center gap-3 px-2 pb-4 pt-1 md:flex">
                    <div className="flex size-11 shrink-0 items-center justify-center rounded-2xl border border-primary/20 bg-primary/10">
                        <Logo size={30} />
                    </div>
                    <div className="min-w-0">
                        <div className="truncate text-[15px] font-semibold tracking-tight">ZyRealm</div>
                        <div className="truncate text-[11px] text-muted-foreground">自由界 · Routing Fabric</div>
                    </div>
                </div>

                <div className="mb-3 hidden h-px bg-sidebar-border/80 md:block" />

                <div className="flex items-center gap-1 md:flex-col md:items-stretch">
                    {ROUTES.map((route, index) => {
                        const isActive = activeItem === route.id
                        return (
                            <motion.button
                                key={route.id}
                                type="button"
                                onClick={() => setActiveItem(route.id as NavItem)}
                                onMouseEnter={() => preload(route.id)}
                                aria-label={t(route.id)}
                                title={t(route.id)}
                                aria-current={isActive ? 'page' : undefined}
                                className={cn(
                                    "relative z-20 flex size-11 shrink-0 items-center justify-center rounded-2xl transition-colors",
                                    "md:h-11 md:w-full md:justify-start md:gap-3 md:px-3",
                                    isActive
                                        ? "text-sidebar-primary-foreground"
                                        : "text-sidebar-foreground/65 hover:bg-sidebar-accent hover:text-sidebar-foreground"
                                )}
                                initial={{ opacity: 0, scale: 0.92 }}
                                animate={{
                                    opacity: 1,
                                    scale: 1,
                                    transition: {
                                        delay: index * 0.035,
                                        duration: 0.25,
                                    }
                                }}
                                whileTap={{ scale: 0.97 }}
                            >
                                {isActive && (
                                    <motion.div
                                        layoutId="navbar-indicator"
                                        className="absolute inset-0 z-0 rounded-2xl bg-sidebar-primary"
                                        transition={{ type: "spring", stiffness: 320, damping: 32 }}
                                    />
                                )}
                                <span className="relative z-10 flex items-center">
                                    <route.icon className="size-5" strokeWidth={1.9} />
                                </span>
                                <span className="relative z-10 hidden truncate text-sm font-medium md:block">
                                    {t(route.id)}
                                </span>
                            </motion.button>
                        )
                    })}
                </div>

                <div className="mt-auto hidden md:block">
                    <div className="rounded-2xl border border-sidebar-border/80 bg-background/40 p-3">
                        <div className="text-[10px] font-semibold uppercase tracking-[0.18em] text-primary">Adaptive routing</div>
                        <div className="mt-2 text-xs leading-5 text-muted-foreground">
                            Provider → Credential → Capability → Recovery → Decision
                        </div>
                    </div>
                </div>
            </motion.nav>
        </div>
    )
}
