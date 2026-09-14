'use client';

import { useState } from "react"
import { motion } from "motion/react"
import { useTranslations } from 'next-intl'
import { Button } from "@/components/ui/button"
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { useLogin } from "@/api/endpoints/user"
import { useAPIKeyLogin } from "@/api/endpoints/apikey"
import Logo from "@/components/modules/logo"
import { KeyRound, Network, Route, ShieldCheck, User } from "lucide-react"
import {
  Tabs,
  TabsList,
  TabsHighlight,
  TabsHighlightItem,
  TabsTrigger,
  TabsContents,
  TabsContent,
} from "@/components/animate-ui/primitives/animate/tabs"

type LoginMode = 'user' | 'apikey';

const capabilities = [
  { icon: Route, label: 'Adaptive failover' },
  { icon: Network, label: 'Multi-provider fabric' },
  { icon: ShieldCheck, label: 'Replay-safe routing' },
]

export function LoginForm({ onLoginSuccess }: { onLoginSuccess?: () => void }) {
  const t = useTranslations('login')
  const [mode, setMode] = useState<LoginMode>('user')
  const [username, setUsername] = useState("")
  const [password, setPassword] = useState("")
  const [apiKey, setApiKey] = useState("")
  const [error, setError] = useState<string | null>(null)

  const loginMutation = useLogin()
  const apiKeyLoginMutation = useAPIKeyLogin()

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)

    try {
      if (mode === 'user') {
        await loginMutation.mutateAsync({ username, password, expire: 86400 })
      } else {
        await apiKeyLoginMutation.mutateAsync(apiKey)
      }
      onLoginSuccess?.()
    } catch (err: unknown) {
      const errorMessage = err instanceof Error ? err.message : t('error.generic')
      setError(errorMessage)
    }
  }

  const isPending = loginMutation.isPending || apiKeyLoginMutation.isPending

  const handleModeChange = (value: string) => {
    setMode(value as LoginMode)
    setError(null)
  }

  return (
    <motion.div
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      exit={{ opacity: 0 }}
      transition={{ duration: 0.3 }}
      className="relative min-h-screen overflow-hidden px-5 py-6 text-foreground sm:px-8 lg:grid lg:grid-cols-[minmax(0,1.1fr)_minmax(420px,0.9fr)] lg:gap-8 lg:p-8"
    >
      <div className="pointer-events-none absolute inset-0 zy-grid" />

      <section className="relative hidden min-h-[calc(100dvh-4rem)] flex-col justify-between overflow-hidden rounded-[32px] border border-border/70 bg-card/55 p-10 backdrop-blur-xl lg:flex">
        <div className="absolute -right-32 -top-32 size-80 rounded-full bg-primary/15 blur-3xl" />
        <div className="absolute -bottom-24 left-16 size-72 rounded-full bg-accent/10 blur-3xl" />

        <div className="relative flex items-center gap-3">
          <div className="flex size-12 items-center justify-center rounded-2xl border border-primary/20 bg-primary/10">
            <Logo size={34} />
          </div>
          <div>
            <div className="text-lg font-semibold tracking-tight">ZyRealm</div>
            <div className="text-xs text-muted-foreground">自由界 · Adaptive LLM Routing Gateway</div>
          </div>
        </div>

        <div className="relative max-w-2xl">
          <div className="mb-5 text-xs font-semibold uppercase tracking-[0.22em] text-primary">Route beyond boundaries</div>
          <h1 className="max-w-xl text-5xl font-semibold leading-[1.05] tracking-[-0.04em] xl:text-6xl">
            自由选择，<br />稳定抵达。
          </h1>
          <p className="mt-6 max-w-xl text-base leading-7 text-muted-foreground">
            One control plane for providers, credentials, capabilities, recovery and routing decisions.
            Keep client protocols stable while the upstream path adapts underneath.
          </p>

          <div className="mt-8 grid max-w-xl gap-3 sm:grid-cols-3">
            {capabilities.map(({ icon: Icon, label }) => (
              <div key={label} className="rounded-2xl border border-border/70 bg-background/45 p-4 backdrop-blur">
                <Icon className="size-4 text-primary" />
                <div className="mt-3 text-xs font-medium leading-5">{label}</div>
              </div>
            ))}
          </div>
        </div>

        <div className="relative flex items-center justify-between text-xs text-muted-foreground">
          <span>Provider → Credential → Capability → Recovery → Decision</span>
          <span>自由界</span>
        </div>
      </section>

      <section className="relative flex min-h-[calc(100dvh-3rem)] items-center justify-center lg:min-h-0">
        <div className="w-full max-w-[440px] rounded-[28px] border border-border/75 bg-card/80 p-6 shadow-xl backdrop-blur-2xl sm:p-8">
          <header className="mb-8">
            <div className="mb-5 flex items-center gap-3 lg:hidden">
              <div className="flex size-11 items-center justify-center rounded-2xl border border-primary/20 bg-primary/10">
                <Logo size={30} />
              </div>
              <div>
                <div className="font-semibold tracking-tight">ZyRealm</div>
                <div className="text-xs text-muted-foreground">自由界</div>
              </div>
            </div>
            <div className="text-xs font-semibold uppercase tracking-[0.2em] text-primary">Control plane access</div>
            <h2 className="mt-2 text-3xl font-semibold tracking-tight">Welcome back</h2>
            <p className="mt-2 text-sm leading-6 text-muted-foreground">Authenticate to manage routes, providers and runtime policy.</p>
          </header>

          <Tabs value={mode} onValueChange={handleModeChange}>
            <TabsList className="flex rounded-2xl bg-muted p-1">
              <TabsHighlight className="rounded-xl bg-background shadow-sm">
                <TabsHighlightItem value="user" className="flex-1">
                  <TabsTrigger value="user" className="flex w-full items-center justify-center gap-2 rounded-xl px-4 py-2.5 text-sm font-medium transition-colors data-[state=active]:text-foreground data-[state=inactive]:text-muted-foreground">
                    <User className="size-4" />
                    {t('mode.user')}
                  </TabsTrigger>
                </TabsHighlightItem>
                <TabsHighlightItem value="apikey" className="flex-1">
                  <TabsTrigger value="apikey" className="flex w-full items-center justify-center gap-2 rounded-xl px-4 py-2.5 text-sm font-medium transition-colors data-[state=active]:text-foreground data-[state=inactive]:text-muted-foreground">
                    <KeyRound className="size-4" />
                    {t('mode.apikey')}
                  </TabsTrigger>
                </TabsHighlightItem>
              </TabsHighlight>
            </TabsList>

            <form onSubmit={handleSubmit} className="space-y-5 pt-2">
              <TabsContents className="-mx-3 p-3 py-5">
                <TabsContent value="user" className="space-y-5">
                  <Field>
                    <FieldLabel htmlFor="username">{t('username')}</FieldLabel>
                    <Input id="username" type="text" placeholder={t('usernamePlaceholder')} value={username} onChange={(e) => setUsername(e.target.value)} required={mode === 'user'} disabled={isPending} />
                  </Field>
                  <Field>
                    <FieldLabel htmlFor="password">{t('password')}</FieldLabel>
                    <Input id="password" type="password" placeholder={t('passwordPlaceholder')} value={password} onChange={(e) => setPassword(e.target.value)} required={mode === 'user'} disabled={isPending} />
                  </Field>
                </TabsContent>
                <TabsContent value="apikey">
                  <Field>
                    <FieldLabel htmlFor="apikey">{t('apikey')}</FieldLabel>
                    <Input id="apikey" type="password" placeholder={t('apikeyPlaceholder')} value={apiKey} onChange={(e) => setApiKey(e.target.value)} required={mode === 'apikey'} disabled={isPending} />
                  </Field>
                </TabsContent>
              </TabsContents>

              {error && <FieldDescription className="text-destructive">{error}</FieldDescription>}

              <Button type="submit" disabled={isPending} className="h-11 w-full rounded-xl">
                {isPending ? t('button.loading') : t('button.submit')}
              </Button>
            </form>
          </Tabs>
        </div>
      </section>
    </motion.div>
  )
}
