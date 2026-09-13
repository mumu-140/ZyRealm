package balancer

// HasAlternativeProvider reports whether the current request still has an
// untried, request-eligible candidate belonging to a different provider.
// Candidates already removed by shared runtime eligibility are not present in
// the iterator, and providers skipped earlier in this request are ignored.
func (it *Iterator) HasAlternativeProvider(channelID int) bool {
	if it == nil {
		return false
	}
	start := it.index + 1
	if start < 0 {
		start = 0
	}
	for i := start; i < len(it.candidates); i++ {
		candidateID := it.candidates[i].ChannelID
		if candidateID <= 0 || candidateID == channelID {
			continue
		}
		if _, skipped := it.skippedProviders[candidateID]; skipped {
			continue
		}
		return true
	}
	return false
}
