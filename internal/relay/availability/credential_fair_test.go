package availability

import (
	"sync"
	"testing"

	dbmodel "github.com/bestruirui/octopus/internal/model"
)

func fairTestKey(id, revision int) dbmodel.ChannelKey {
	return dbmodel.ChannelKey{ID: id, ChannelID: 91, Enabled: true, ChannelKey: "key", CredentialRevision: revision}
}

func TestCredentialFairSelectionIsProviderLocalAndEven(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	keys := []dbmodel.ChannelKey{fairTestKey(1, 1), fairTestKey(2, 1), fairTestKey(3, 1)}
	counts := map[int]int{}
	for range 600 {
		selected := SelectCredentialFair(91, keys, 0)
		counts[selected.ID]++
	}
	for _, id := range []int{1, 2, 3} {
		if counts[id] != 200 {
			t.Fatalf("distribution=%v, want 200 selections per key", counts)
		}
	}

	// A different provider owns a separate ledger and starts from its own zero.
	other := []dbmodel.ChannelKey{{ID: 1, ChannelID: 92, Enabled: true, ChannelKey: "other", CredentialRevision: 1}}
	if got := SelectCredentialFair(92, other, 0).ID; got != 1 {
		t.Fatalf("provider-local ledger selected key %d", got)
	}
}

func TestCredentialFairSelectionBalancesHundredKeys(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	keys := make([]dbmodel.ChannelKey, 0, 100)
	for id := 1; id <= 100; id++ {
		keys = append(keys, fairTestKey(id, 1))
	}

	counts := make(map[int]int, len(keys))
	for range 1000 {
		selected := SelectCredentialFair(91, keys, 0)
		if selected.ID == 0 {
			t.Fatalf("fair selector returned no credential")
		}
		counts[selected.ID]++
	}

	for id := 1; id <= 100; id++ {
		if counts[id] != 10 {
			t.Fatalf("100-key distribution is not balanced: key=%d count=%d, want 10", id, counts[id])
		}
	}
}

func TestCredentialFairNewMemberUsesCurrentWatermark(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	first := fairTestKey(1, 1)
	for range 1000 {
		SelectCredentialFair(91, []dbmodel.ChannelKey{first}, 0)
	}
	second := fairTestKey(2, 1)
	if got := SelectCredentialFair(91, []dbmodel.ChannelKey{first, second}, 0).ID; got != 2 {
		t.Fatalf("new member first selection=%d, want 2", got)
	}
	if got := SelectCredentialFair(91, []dbmodel.ChannelKey{first, second}, 0).ID; got != 1 {
		t.Fatalf("new member must not enter historical catch-up burst; second selection=%d", got)
	}
}

func TestCredentialFairRevisionReplacementUsesCurrentWatermark(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	keys := []dbmodel.ChannelKey{fairTestKey(1, 1), fairTestKey(2, 1)}
	for range 200 {
		SelectCredentialFair(91, keys, 0)
	}
	keys[1].CredentialRevision = 2
	if got := SelectCredentialFair(91, keys, 0).ID; got != 2 {
		t.Fatalf("replacement identity should receive one normal fair turn, got %d", got)
	}
	if got := SelectCredentialFair(91, keys, 0).ID; got != 1 {
		t.Fatalf("replacement identity must not inherit historical deficit, got %d", got)
	}
}

func TestCredentialFairReentryCalibratesAfterTemporaryIneligibility(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	first := fairTestKey(1, 1)
	second := fairTestKey(2, 1)
	both := []dbmodel.ChannelKey{first, second}
	for range 20 {
		SelectCredentialFair(91, both, 0)
	}
	for range 100 {
		SelectCredentialFair(91, []dbmodel.ChannelKey{first}, 0)
	}
	if got := SelectCredentialFair(91, both, 0).ID; got != 2 {
		t.Fatalf("re-entering key should receive one normal turn, got %d", got)
	}
	if got := SelectCredentialFair(91, both, 0).ID; got != 1 {
		t.Fatalf("re-entering key must not catch up old absence, got %d", got)
	}
}

func TestCredentialFairPreferredKeyIsChargedAndLongCatchupIsBroken(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	keys := []dbmodel.ChannelKey{fairTestKey(1, 1), fairTestKey(2, 1)}
	for range 1000 {
		if got := SelectCredentialFair(91, keys, 1).ID; got != 1 {
			t.Fatalf("preferred key selected %d", got)
		}
	}
	for range credentialMaxConsecutive {
		if got := SelectCredentialFair(91, keys, 0).ID; got != 2 {
			t.Fatalf("lagging key catch-up selected %d before guard", got)
		}
	}
	if got := SelectCredentialFair(91, keys, 0).ID; got != 1 {
		t.Fatalf("continuous-allocation guard selected %d, want temporary alternative 1", got)
	}
	if got := SelectCredentialFair(91, keys, 0).ID; got != 2 {
		t.Fatalf("normal catch-up should resume after guard, got %d", got)
	}
}

func TestCredentialFairConcurrentSelectionsAreAtomic(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	keys := []dbmodel.ChannelKey{fairTestKey(1, 1), fairTestKey(2, 1)}
	results := make(chan int, 600)
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				results <- SelectCredentialFair(91, keys, 0).ID
			}
		}()
	}
	wg.Wait()
	close(results)
	counts := map[int]int{}
	for id := range results {
		counts[id]++
	}
	if counts[1] != 300 || counts[2] != 300 {
		t.Fatalf("concurrent distribution=%v, want 300/300", counts)
	}
}
