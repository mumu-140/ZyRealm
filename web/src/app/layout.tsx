import "./globals.css";
import { ThemeProvider } from "@/provider/theme";
import { Toaster } from "@/components/ui/sonner"
import { LocaleProvider } from "@/provider/locale";
import QueryProvider from "@/provider/query";
import { ServiceWorkerRegister } from "@/components/sw-register";
import { TooltipProvider } from "@/components/animate-ui/components/animate/tooltip";

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <html suppressHydrationWarning>
      <head>
        <meta name="theme-color" content="#0b1220" />
        <meta name="application-name" content="ZyRealm" />
        <meta name="description" content="ZyRealm 自由界 — adaptive LLM routing gateway and provider control plane." />
        <meta name="apple-mobile-web-app-capable" content="yes" />
        <meta name="apple-mobile-web-app-status-bar-style" content="black-translucent" />
        <meta name="apple-mobile-web-app-title" content="ZyRealm" />
        <meta name="mobile-web-app-capable" content="yes" />
        <meta name="mobile-web-app-status-bar-style" content="black" />
        <meta name="mobile-web-app-title" content="ZyRealm" />
        <link rel="manifest" href="./manifest.json" />
        <link rel="icon" href="./logo.svg" type="image/svg+xml" />
        <title>ZyRealm · 自由界</title>
        <style
          dangerouslySetInnerHTML={{
            __html: `
              #initial-loader {
                position: fixed;
                inset: 0;
                z-index: 9999;
                display: flex;
                align-items: center;
                justify-content: center;
                background: var(--background);
                color: var(--primary);
                transition: opacity 200ms ease;
              }
              #initial-loader.zy-hide {
                opacity: 0;
                pointer-events: none;
              }
              #initial-loader svg {
                width: 112px;
                height: 112px;
              }
              #initial-loader .zy-group {
                animation: zyFade 1.8s ease-in-out infinite;
              }
              #initial-loader path {
                fill: none;
                stroke: currentColor;
                stroke-width: 5.5;
                stroke-linecap: round;
                stroke-linejoin: round;
                stroke-dasharray: 1;
                stroke-dashoffset: 1;
                opacity: 0;
                animation: zyDraw 1.8s ease-in-out infinite both;
              }
              #initial-loader path:nth-child(1) { animation-delay: 0s; }
              #initial-loader path:nth-child(2) { animation-delay: 0.12s; }
              #initial-loader path:nth-child(3) { animation-delay: 0.24s; }
              #initial-loader path:nth-child(4) { animation-delay: 0.36s; }

              @keyframes zyDraw {
                0%   { stroke-dashoffset: 1; opacity: 0; }
                7%   { opacity: 1; }
                46%  { stroke-dashoffset: 0; opacity: 1; }
                100% { stroke-dashoffset: 0; opacity: 1; }
              }
              @keyframes zyFade {
                0%   { opacity: 1; }
                74%  { opacity: 1; }
                100% { opacity: 0; }
              }

              @media (prefers-reduced-motion: reduce) {
                #initial-loader .zy-group,
                #initial-loader path {
                  animation: none !important;
                  opacity: 1 !important;
                  stroke-dashoffset: 0 !important;
                }
              }
            `,
          }}
        />
      </head>
      <body className="antialiased">
        <div id="initial-loader" role="status" aria-label="Loading ZyRealm">
          <svg viewBox="0 0 100 100" xmlns="http://www.w3.org/2000/svg">
            <g className="zy-group">
              <path pathLength="1" d="M50 9 L82 27 V73 L50 91 L18 73 V27 Z" />
              <path pathLength="1" d="M31 34 H69" />
              <path pathLength="1" d="M68 34 L32 66" />
              <path pathLength="1" d="M31 66 H69" />
            </g>
          </svg>
        </div>
        <ServiceWorkerRegister />
        <ThemeProvider attribute="class" defaultTheme="system" enableSystem>
          <QueryProvider>
            <LocaleProvider>
              <TooltipProvider>
                {children}
                <Toaster />
              </TooltipProvider>
            </LocaleProvider>
          </QueryProvider>
        </ThemeProvider>
      </body>
    </html>
  );
}
