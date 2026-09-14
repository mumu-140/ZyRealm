'use client';

import { motion } from 'motion/react';
import { cn } from '@/lib/utils';

interface LogoProps {
    size?: number | string;
    animate?: boolean;
    className?: string;
}

const LOGO_DRAW_DURATION_S = 0.65;
const LOGO_STAGGER_S = 0.08;
const LOGO_FADE_DURATION_S = 0.45;

// ZyRealm mark: Cyber Shield & Z-Wings Emblem
export const WING_PATHS = [
    // Left Wing (3 geometric feathers)
    { id: 'lw-1', d: 'M 18 20 L 38 40 L 35 50 L 18 32 Z', opacity: 0.95 },
    { id: 'lw-2', d: 'M 22 40 L 40 54 L 37 62 L 23 50 Z', opacity: 0.8 },
    { id: 'lw-3', d: 'M 28 56 L 42 66 L 39 72 L 28 64 Z', opacity: 0.65 },
    // Right Wing (3 geometric feathers, symmetrical)
    { id: 'rw-1', d: 'M 82 20 L 62 40 L 65 50 L 82 32 Z', opacity: 0.95 },
    { id: 'rw-2', d: 'M 78 40 L 60 54 L 63 62 L 77 50 Z', opacity: 0.8 },
    { id: 'rw-3', d: 'M 72 56 L 58 66 L 61 72 L 72 64 Z', opacity: 0.65 },
    // Shield Apex
    { id: 'shield', d: 'M 42 74 L 50 86 L 58 74 L 50 78 Z', opacity: 0.9 },
    // Core Z Routing Spine
    { id: 'core-z', d: 'M 32 32 L 68 32 L 68 39 L 45 61 L 68 61 L 68 68 L 32 68 L 32 61 L 55 39 L 32 39 Z', opacity: 1 },
];

export const LOGO_DRAW_END_MS = Math.round(
    ((WING_PATHS.length - 1) * LOGO_STAGGER_S + LOGO_DRAW_DURATION_S) * 1000
);

export default function Logo({ size = 48, animate = false, className }: LogoProps) {
    const sizeValue = size === '100%' ? '100%' : size;

    if (animate) {
        const drawEndTime = (WING_PATHS.length - 1) * LOGO_STAGGER_S + LOGO_DRAW_DURATION_S;
        const cycleDuration = drawEndTime + LOGO_FADE_DURATION_S;

        return (
            <motion.svg
                viewBox="0 0 100 100"
                fill="none"
                xmlns="http://www.w3.org/2000/svg"
                width={sizeValue}
                height={sizeValue}
                className={cn('text-primary', className)}
                aria-hidden="true"
            >
                <defs>
                    <linearGradient id="zyrealm-anim-core" x1="20" y1="15" x2="80" y2="85" gradientUnits="userSpaceOnUse">
                        <stop stopColor="#38BDF8" />
                        <stop offset="0.5" stopColor="#06B6D4" />
                        <stop offset="1" stopColor="#2563EB" />
                    </linearGradient>
                    <linearGradient id="zyrealm-anim-wings" x1="50" y1="10" x2="50" y2="90" gradientUnits="userSpaceOnUse">
                        <stop stopColor="#38BDF8" />
                        <stop offset="1" stopColor="#1D4ED8" />
                    </linearGradient>
                </defs>
                <motion.g
                    initial={{ opacity: 1 }}
                    animate={{ opacity: [1, 1, 0] }}
                    transition={{
                        duration: cycleDuration,
                        times: [0, drawEndTime / cycleDuration, 1],
                        ease: 'easeInOut',
                        repeat: Infinity,
                    }}
                >
                    {WING_PATHS.map((item, index) => {
                        const startTime = index * LOGO_STAGGER_S;
                        const endTime = startTime + LOGO_DRAW_DURATION_S;
                        const isCore = item.id === 'core-z';
                        const isShield = item.id === 'shield';

                        return (
                            <motion.path
                                key={item.id}
                                d={item.d}
                                fill={isCore ? 'url(#zyrealm-anim-core)' : isShield ? '#06B6D4' : 'url(#zyrealm-anim-wings)'}
                                stroke={isCore ? '#38BDF8' : '#60A5FA'}
                                strokeWidth="1.5"
                                strokeLinecap="round"
                                strokeLinejoin="round"
                                initial={{ pathLength: 0, opacity: 0 }}
                                animate={{
                                    pathLength: [0, 0, 1, 1],
                                    opacity: [0, 0, item.opacity, item.opacity],
                                }}
                                transition={{
                                    pathLength: {
                                        duration: cycleDuration,
                                        times: [0, startTime / cycleDuration, endTime / cycleDuration, 1],
                                        ease: 'easeInOut',
                                        repeat: Infinity,
                                    },
                                    opacity: {
                                        duration: cycleDuration,
                                        times: [0, startTime / cycleDuration, endTime / cycleDuration, 1],
                                        ease: 'easeInOut',
                                        repeat: Infinity,
                                    },
                                }}
                            />
                        );
                    })}
                </motion.g>
            </motion.svg>
        );
    }

    return (
        <svg
            viewBox="0 0 100 100"
            fill="none"
            xmlns="http://www.w3.org/2000/svg"
            width={sizeValue}
            height={sizeValue}
            className={cn('shrink-0', className)}
            aria-hidden="true"
        >
            <defs>
                <linearGradient id="zyrealm-static-core" x1="20" y1="15" x2="80" y2="85" gradientUnits="userSpaceOnUse">
                    <stop stopColor="#2563EB" />
                    <stop offset="0.5" stopColor="#0284C7" />
                    <stop offset="1" stopColor="#06B6D4" />
                </linearGradient>
                <linearGradient id="zyrealm-static-wings" x1="50" y1="10" x2="50" y2="90" gradientUnits="userSpaceOnUse">
                    <stop stopColor="#38BDF8" />
                    <stop offset="1" stopColor="#1D4ED8" />
                </linearGradient>
            </defs>

            {/* Left Wing 3 feathers */}
            <path d="M 18 20 L 38 40 L 35 50 L 18 32 Z" fill="url(#zyrealm-static-wings)" opacity="0.95" />
            <path d="M 22 40 L 40 54 L 37 62 L 23 50 Z" fill="url(#zyrealm-static-wings)" opacity="0.8" />
            <path d="M 28 56 L 42 66 L 39 72 L 28 64 Z" fill="url(#zyrealm-static-wings)" opacity="0.65" />

            {/* Right Wing 3 feathers */}
            <path d="M 82 20 L 62 40 L 65 50 L 82 32 Z" fill="url(#zyrealm-static-wings)" opacity="0.95" />
            <path d="M 78 40 L 60 54 L 63 62 L 77 50 Z" fill="url(#zyrealm-static-wings)" opacity="0.8" />
            <path d="M 72 56 L 58 66 L 61 72 L 72 64 Z" fill="url(#zyrealm-static-wings)" opacity="0.65" />

            {/* Shield Apex */}
            <path d="M 42 74 L 50 86 L 58 74 L 50 78 Z" fill="#06B6D4" />

            {/* Core Z Routing Spine */}
            <path d="M 32 32 L 68 32 L 68 39 L 45 61 L 68 61 L 68 68 L 32 68 L 32 61 L 55 39 L 32 39 Z" fill="url(#zyrealm-static-core)" />
        </svg>
    );
}
