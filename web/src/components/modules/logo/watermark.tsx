import { ZYREALM_MONOCHROME_D } from './paths';

export function BrandWatermark() {
    return (
        <div
            className="pointer-events-none fixed inset-0 -z-10 flex items-center justify-center overflow-hidden"
            aria-hidden="true"
        >
            {/* Ambient cyber illumination mesh — removes plain flat solid color */}
            <div className="absolute inset-0 bg-[radial-gradient(ellipse_80%_60%_at_50%_15%,rgba(37,99,235,0.08),transparent_75%)] dark:bg-[radial-gradient(ellipse_80%_60%_at_50%_15%,rgba(0,210,255,0.09),transparent_75%)]" />
            <div className="absolute inset-0 bg-[radial-gradient(ellipse_70%_50%_at_50%_85%,rgba(6,182,212,0.06),transparent_75%)] dark:bg-[radial-gradient(ellipse_70%_50%_at_50%_85%,rgba(30,64,175,0.14),transparent_75%)]" />

            {/* Ethereal background logo watermark: 隐隐约约有图标，高透明度 */}
            <div className="relative size-[640px] max-w-[85vw] opacity-[0.032] dark:opacity-[0.055] select-none transition-opacity duration-500">
                <svg
                    viewBox="0 0 1254 1254"
                    fill="currentColor"
                    className="size-full text-foreground dark:text-cyan-400"
                >
                    <path
                        fillRule="evenodd"
                        clipRule="evenodd"
                        d={ZYREALM_MONOCHROME_D}
                    />
                </svg>
            </div>
        </div>
    );
}
