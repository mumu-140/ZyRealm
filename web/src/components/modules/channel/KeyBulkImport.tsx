import { useMemo, useState } from 'react';
import { Button } from '@/components/ui/button';
import {
    parseCredentialLines,
    shouldShowLargeCredentialPoolHint,
    type ImportedCredentialKey,
} from './key-import';

interface KeyBulkImportProps {
    credentialCount: number;
    maxConcurrency: number;
    onImport: (keys: ImportedCredentialKey[]) => void;
}

export function KeyBulkImport({ credentialCount, maxConcurrency, onImport }: KeyBulkImportProps) {
    const [input, setInput] = useState('');
    const parsed = useMemo(() => parseCredentialLines(input), [input]);
    const showConcurrencyHint = shouldShowLargeCredentialPoolHint(credentialCount, maxConcurrency);

    return (
        <div className="space-y-2 rounded-xl border border-border p-3">
            <div className="flex items-center justify-between gap-3">
                <div>
                    <div className="text-sm font-medium text-card-foreground">Bulk import API keys</div>
                    <p className="text-xs text-muted-foreground">One credential per line. Duplicates and blank lines are ignored.</p>
                </div>
                <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    disabled={parsed.validCount === 0}
                    onClick={() => {
                        onImport(parsed.keys);
                        setInput('');
                    }}
                >
                    Import
                </Button>
            </div>
            <textarea
                value={input}
                onChange={(event) => setInput(event.target.value)}
                rows={4}
                autoComplete="off"
                spellCheck={false}
                placeholder="nvapi-..."
                className="w-full resize-y rounded-xl border border-input bg-background px-3 py-2 font-mono text-sm text-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring"
            />
            {input.length > 0 ? (
                <p className="text-xs text-muted-foreground">
                    {parsed.validCount} valid / {parsed.duplicateCount} duplicates / {parsed.blankCount} blank lines ignored
                </p>
            ) : null}
            {showConcurrencyHint ? (
                <p className="text-xs text-amber-600 dark:text-amber-400">
                    This Channel currently allows only {maxConcurrency} concurrent requests. A large key pool is still capped by the Channel concurrency limit; key count does not imply provider quota.
                </p>
            ) : null}
        </div>
    );
}
