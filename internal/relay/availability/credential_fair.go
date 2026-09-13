package availability

import (
	"sync"

	dbmodel "github.com/bestruirui/octopus/internal/model"
)

const credentialMaxConsecutive = 100

type credentialFairMember struct {
	revision     int
	progress     uint64
	lastSelected uint64
	eligible     bool
}

type credentialFairLedger struct {
	mu          sync.Mutex
	members     map[int]*credentialFairMember
	watermark   uint64
	sequence    uint64
	lastMember  int
	consecutive uint64
}

var credentialFairRuntime = struct {
	mu      sync.Mutex
	ledgers map[int]*credentialFairLedger
}{ledgers: make(map[int]*credentialFairLedger)}

func credentialFairLedgerFor(channelID int) *credentialFairLedger {
	credentialFairRuntime.mu.Lock()
	defer credentialFairRuntime.mu.Unlock()
	ledger := credentialFairRuntime.ledgers[channelID]
	if ledger == nil {
		ledger = &credentialFairLedger{members: make(map[int]*credentialFairMember)}
		credentialFairRuntime.ledgers[channelID] = ledger
	}
	return ledger
}

func credentialFairLess(ledger *credentialFairLedger, leftID, rightID int) bool {
	left := ledger.members[leftID]
	right := ledger.members[rightID]
	if left.progress != right.progress {
		return left.progress < right.progress
	}
	if left.lastSelected != right.lastSelected {
		return left.lastSelected < right.lastSelected
	}
	return leftID < rightID
}

// SelectCredentialFair performs equal-weight fair scheduling inside one
// provider/channel only. New credentials and new CredentialRevision identities
// enter at the current provider watermark instead of progress zero, preventing
// catch-up bursts after adding or replacing a key. A preferred sticky key wins
// when present, but the allocation is still charged to the same ledger.
func SelectCredentialFair(channelID int, candidates []dbmodel.ChannelKey, preferredKeyID int) dbmodel.ChannelKey {
	if channelID <= 0 || len(candidates) == 0 {
		return dbmodel.ChannelKey{}
	}

	// The registry lock only protects ledger lookup/creation. Fairness mutations
	// are serialized by the selected Provider's own lock, so unrelated Providers
	// never contend on one global scheduling mutex.
	ledger := credentialFairLedgerFor(channelID)
	ledger.mu.Lock()
	defer ledger.mu.Unlock()

	byID := make(map[int]dbmodel.ChannelKey, len(candidates))
	activeIDs := make(map[int]struct{}, len(candidates))
	candidateIDs := make([]int, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.ID <= 0 || !candidate.Enabled || candidate.ChannelKey == "" {
			continue
		}
		revision := normalizeCredentialRevision(candidate.CredentialRevision)
		member := ledger.members[candidate.ID]
		if member == nil || member.revision != revision {
			if member != nil && ledger.lastMember == candidate.ID {
				ledger.lastMember = 0
				ledger.consecutive = 0
			}
			member = &credentialFairMember{
				revision: revision,
				progress: ledger.watermark,
				eligible: true,
			}
			ledger.members[candidate.ID] = member
		} else {
			if !member.eligible && member.progress < ledger.watermark {
				member.progress = ledger.watermark
			}
			member.eligible = true
		}
		byID[candidate.ID] = candidate
		activeIDs[candidate.ID] = struct{}{}
		candidateIDs = append(candidateIDs, candidate.ID)
	}
	for keyID, member := range ledger.members {
		if _, active := activeIDs[keyID]; !active {
			member.eligible = false
		}
	}
	if len(candidateIDs) == 0 {
		return dbmodel.ChannelKey{}
	}

	selectedID := 0
	if _, ok := byID[preferredKeyID]; preferredKeyID > 0 && ok {
		selectedID = preferredKeyID
	} else {
		selectedID = candidateIDs[0]
		for _, candidateID := range candidateIDs[1:] {
			if credentialFairLess(ledger, candidateID, selectedID) {
				selectedID = candidateID
			}
		}
		if len(candidateIDs) > 1 && ledger.lastMember == selectedID && ledger.consecutive >= credentialMaxConsecutive {
			alternativeID := 0
			for _, candidateID := range candidateIDs {
				if candidateID == selectedID {
					continue
				}
				if alternativeID == 0 || credentialFairLess(ledger, candidateID, alternativeID) {
					alternativeID = candidateID
				}
			}
			if alternativeID != 0 {
				selectedID = alternativeID
			}
		}
	}

	member := ledger.members[selectedID]
	member.progress++
	ledger.sequence++
	member.lastSelected = ledger.sequence
	if member.progress > ledger.watermark {
		ledger.watermark = member.progress
	}
	if ledger.lastMember == selectedID {
		ledger.consecutive++
	} else {
		ledger.lastMember = selectedID
		ledger.consecutive = 1
	}
	return byID[selectedID]
}

func resetCredentialFairness() {
	credentialFairRuntime.mu.Lock()
	credentialFairRuntime.ledgers = make(map[int]*credentialFairLedger)
	credentialFairRuntime.mu.Unlock()
}
