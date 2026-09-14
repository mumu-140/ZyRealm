'use client';

import { useId } from 'react';
import { motion } from 'motion/react';
import { cn } from '@/lib/utils';
import {
    ZYREALM_WINGS_D,
    ZYREALM_FACET_D,
    ZYREALM_CORE_D,
    ZYREALM_MONOCHROME_D,
} from './paths';

export interface LogoProps {
    size?: number | string;
    variant?: 'auto' | 'cyber' | 'black';
    animate?: boolean;
    className?: string;
}

export const LOGO_DRAW_END_MS = 650;

export default function Logo({
    size = 48,
    variant = 'auto',
    animate = false,
    className,
}: LogoProps) {
    const id = useId().replace(/:/g, '');
    const wingGradId = `zy-wing-grad-${id}`;
    const facetGradId = `zy-facet-grad-${id}`;
    const coreGradId = `zy-core-grad-${id}`;
    const glowFilterId = `zy-core-glow-${id}`;

    const sizeValue = size === '100%' ? '100%' : size;

    if (variant === 'black') {
        return (
            <svg
                viewBox="0 0 1254 1254"
                fill="none"
                xmlns="http://www.w3.org/2000/svg"
                width={sizeValue}
                height={sizeValue}
                className={cn('shrink-0 text-foreground', className)}
                aria-hidden="true"
            >
                <path
                    fillRule="evenodd"
                    clipRule="evenodd"
                    d={ZYREALM_MONOCHROME_D}
                    fill="currentColor"
                />
            </svg>
        );
    }

    return (
        <svg
            viewBox="0 0 1254 1254"
            fill="none"
            xmlns="http://www.w3.org/2000/svg"
            width={sizeValue}
            height={sizeValue}
            className={cn('shrink-0', className)}
            aria-hidden="true"
        >
            <defs>
                {/* Wing & Circuit Framework Gradient: Royal Sapphire to Electric Cyan */}
                <linearGradient
                    id={wingGradId}
                    x1="632"
                    y1="70"
                    x2="632"
                    y2="1125"
                    gradientUnits="userSpaceOnUse"
                >
                    <stop offset="0%" stopColor="#1D4ED8" />
                    <stop offset="35%" stopColor="#2563EB" />
                    <stop offset="70%" stopColor="#0284C7" />
                    <stop offset="100%" stopColor="#00F0FF" />
                </linearGradient>

                {/* Facet Ring Gradient: Electric Sky to Neon Cyan */}
                <linearGradient
                    id={facetGradId}
                    x1="632"
                    y1="568"
                    x2="632"
                    y2="850"
                    gradientUnits="userSpaceOnUse"
                >
                    <stop offset="0%" stopColor="#38BDF8" />
                    <stop offset="50%" stopColor="#00F0FF" />
                    <stop offset="100%" stopColor="#0284C7" />
                </linearGradient>

                {/* Quantum Prism Core Gradient: Horizon Cyan-White Glow */}
                <linearGradient
                    id={coreGradId}
                    x1="632"
                    y1="632"
                    x2="632"
                    y2="782"
                    gradientUnits="userSpaceOnUse"
                >
                    <stop offset="0%" stopColor="#FFFFFF" />
                    <stop offset="30%" stopColor="#E0F7FE" />
                    <stop offset="70%" stopColor="#38BDF8" />
                    <stop offset="100%" stopColor="#00F0FF" />
                </linearGradient>

                {/* Luminous Glow Filter for Center Core */}
                <filter
                    id={glowFilterId}
                    x="580"
                    y="600"
                    width="104"
                    height="220"
                    filterUnits="userSpaceOnUse"
                >
                    <feGaussianBlur stdDeviation="6" result="blur" />
                    <feMerge>
                        <feMergeNode in="blur" />
                        <feMergeNode in="SourceGraphic" />
                    </feMerge>
                </filter>
            </defs>

            {/* Wing Framework & Circuit Topology */}
            <path
                fillRule="evenodd"
                clipRule="evenodd"
                d={ZYREALM_WINGS_D}
                fill={`url(#${wingGradId})`}
            />

            {/* Faceted Diamond Ring */}
            <path
                fillRule="evenodd"
                clipRule="evenodd"
                d={ZYREALM_FACET_D}
                fill={`url(#${facetGradId})`}
            />

            {/* Elongated Quantum Prism Core with Horizon Glow */}
            {animate ? (
                <motion.g
                    initial={{ opacity: 0.85 }}
                    animate={{
                        opacity: [0.85, 1, 0.85],
                        scale: [0.98, 1.02, 0.98],
                    }}
                    transition={{
                        duration: 2.2,
                        repeat: Infinity,
                        ease: 'easeInOut',
                    }}
                    style={{ transformOrigin: '632px 707px' }}
                >
                    <path
                        d={ZYREALM_CORE_D}
                        fill={`url(#${coreGradId})`}
                        filter={`url(#${glowFilterId})`}
                    />
                    <path d={ZYREALM_CORE_D} fill="#FFFFFF" opacity={0.9} />
                </motion.g>
            ) : (
                <g>
                    <path
                        d={ZYREALM_CORE_D}
                        fill={`url(#${coreGradId})`}
                        filter={`url(#${glowFilterId})`}
                    />
                    <path d={ZYREALM_CORE_D} fill="#FFFFFF" opacity={0.9} />
                </g>
            )}
        </svg>
    );
}

export {
    ZYREALM_WINGS_D,
    ZYREALM_FACET_D,
    ZYREALM_CORE_D,
    ZYREALM_MONOCHROME_D,
} from './paths';
