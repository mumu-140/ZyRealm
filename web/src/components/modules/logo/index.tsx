'use client';

import { motion } from 'motion/react';

interface LogoProps {
    size?: number | string;
    animate?: boolean;
}

const LOGO_DRAW_DURATION_S = 0.72;
const LOGO_STAGGER_S = 0.12;
const LOGO_FADE_DURATION_S = 0.5;

// ZyRealm mark: an open realm boundary with a Z-shaped routing path.
const paths = [
    'M50 9 L82 27 V73 L50 91 L18 73 V27 Z',
    'M31 34 H69',
    'M68 34 L32 66',
    'M31 66 H69',
];

export const LOGO_DRAW_END_MS = Math.round(
    ((paths.length - 1) * LOGO_STAGGER_S + LOGO_DRAW_DURATION_S) * 1000
);

export default function Logo({ size = 48, animate = false }: LogoProps) {
    const sizeValue = size === '100%' ? '100%' : size;

    if (animate) {
        const drawEndTime = (paths.length - 1) * LOGO_STAGGER_S + LOGO_DRAW_DURATION_S;
        const cycleDuration = drawEndTime + LOGO_FADE_DURATION_S;

        return (
            <motion.svg
                viewBox="0 0 100 100"
                xmlns="http://www.w3.org/2000/svg"
                width={sizeValue}
                height={sizeValue}
                className="text-primary"
                aria-hidden="true"
            >
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
                    {paths.map((d, index) => {
                        const startTime = index * LOGO_STAGGER_S;
                        const endTime = startTime + LOGO_DRAW_DURATION_S;

                        return (
                            <motion.path
                                key={d}
                                d={d}
                                fill="none"
                                stroke="currentColor"
                                strokeWidth="5.5"
                                strokeLinecap="round"
                                strokeLinejoin="round"
                                initial={{ pathLength: 0, opacity: 0 }}
                                animate={{
                                    pathLength: [0, 0, 1, 1],
                                    opacity: [0, 0, 1, 1],
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
                                        ease: 'linear',
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
            xmlns="http://www.w3.org/2000/svg"
            width={sizeValue}
            height={sizeValue}
            className="text-primary"
            aria-hidden="true"
        >
            {paths.map((d) => (
                <path
                    key={d}
                    d={d}
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="5.5"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                />
            ))}
        </svg>
    );
}
