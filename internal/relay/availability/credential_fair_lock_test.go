package availability

import (
	"testing"
	"time"

	dbmodel "github.com/bestruirui/octopus/internal/model"
)

func TestCredentialFairSelectionUsesProviderLocalLocks(t *testing.T) {
	Reset()
	t.Cleanup(Reset)

	blockedLedger := credentialFairLedgerFor(81)
	blockedLedger.mu.Lock()
	defer blockedLedger.mu.Unlock()

	done := make(chan struct{})
	go func() {
		SelectCredentialFair(82, []dbmodel.ChannelKey{{
			ID: 820, ChannelID: 82, Enabled: true, ChannelKey: "key", CredentialRevision: 1,
		}}, 0)
		close(done)
	}()

	select {
	case <-done:
		// Provider 82 must not wait on Provider 81's ledger lock.
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("credential scheduling for another provider blocked on unrelated ledger")
	}
}
