package availability

// ResetChannel clears all process-local routing memory for one Provider. It is
// used after an explicit channel configuration mutation so a stale cooldown,
// credential verdict, fairness history, or capability negative observation does
// not survive a materially new Provider configuration.
func ResetChannel(channelID int) {
	if channelID <= 0 {
		return
	}

	shared.mu.Lock()
	for key := range shared.entries {
		if key.channelID == channelID {
			delete(shared.entries, key)
		}
	}
	shared.mu.Unlock()

	credentialRuntime.mu.Lock()
	for key := range credentialRuntime.entries {
		if key.channelID == channelID {
			delete(credentialRuntime.entries, key)
		}
	}
	credentialRuntime.mu.Unlock()

	credentialFairRuntime.mu.Lock()
	delete(credentialFairRuntime.ledgers, channelID)
	credentialFairRuntime.mu.Unlock()

	capabilityShared.mu.Lock()
	for key := range capabilityShared.entries {
		if key.channelID == channelID {
			delete(capabilityShared.entries, key)
		}
	}
	capabilityShared.mu.Unlock()
}
