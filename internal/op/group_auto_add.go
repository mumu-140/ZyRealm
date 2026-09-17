package op

import (
	"context"
	"sort"
	"strings"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/dlclark/regexp2"
)

type GroupAutoAddResult struct {
	Matched int `json:"matched"`
	Added   int `json:"added"`
}

type groupAutoAddMemberKey struct {
	channelID int
	modelName string
}

func GroupAutoAdd(groupID int, ctx context.Context) (*GroupAutoAddResult, error) {
	group, err := GroupGet(groupID, ctx)
	if err != nil {
		return nil, err
	}
	llms, err := ChannelLLMList(ctx)
	if err != nil {
		return nil, err
	}
	adds, matched, err := resolveGroupAutoAddCandidates(*group, llms)
	if err != nil {
		return nil, err
	}
	result := &GroupAutoAddResult{Matched: matched, Added: len(adds)}
	if len(adds) == 0 {
		return result, nil
	}
	if _, err := GroupUpdate(&model.GroupUpdateRequest{
		ID:         groupID,
		ItemsToAdd: adds,
	}, ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func resolveGroupAutoAddCandidates(group model.Group, llms []model.LLMChannel) ([]model.GroupItemAddRequest, int, error) {
	pattern := strings.TrimSpace(group.MatchRegex)
	groupName := strings.TrimSpace(group.Name)

	var regex *regexp2.Regexp
	var err error
	if pattern != "" {
		regex, err = regexp2.Compile(pattern, regexp2.ECMAScript)
		if err != nil {
			return nil, 0, err
		}
	} else if groupName == "" {
		return []model.GroupItemAddRequest{}, 0, nil
	}

	existing := make(map[groupAutoAddMemberKey]struct{}, len(group.Items))
	maxPriority := 0
	for _, item := range group.Items {
		existing[groupAutoAddMemberKey{channelID: item.ChannelID, modelName: item.ModelName}] = struct{}{}
		if item.Priority > maxPriority {
			maxPriority = item.Priority
		}
	}

	matchedCandidates := make([]model.LLMChannel, 0)
	matched := 0
	needle := strings.ToLower(groupName)
	for _, llm := range llms {
		isMatch := false
		if regex != nil {
			isMatch, err = regex.MatchString(llm.Name)
			if err != nil {
				return nil, 0, err
			}
		} else {
			isMatch = strings.Contains(strings.ToLower(llm.Name), needle)
		}
		if !isMatch {
			continue
		}

		matched++
		key := groupAutoAddMemberKey{channelID: llm.ChannelID, modelName: llm.Name}
		if _, ok := existing[key]; ok {
			continue
		}
		matchedCandidates = append(matchedCandidates, llm)
	}

	sort.SliceStable(matchedCandidates, func(i, j int) bool {
		left := strings.ToLower(matchedCandidates[i].Name)
		right := strings.ToLower(matchedCandidates[j].Name)
		if left != right {
			return left < right
		}
		if matchedCandidates[i].Name != matchedCandidates[j].Name {
			return matchedCandidates[i].Name < matchedCandidates[j].Name
		}
		return matchedCandidates[i].ChannelID < matchedCandidates[j].ChannelID
	})

	adds := make([]model.GroupItemAddRequest, 0, len(matchedCandidates))
	for index, llm := range matchedCandidates {
		adds = append(adds, model.GroupItemAddRequest{
			ChannelID: llm.ChannelID,
			ModelName: llm.Name,
			Priority:  maxPriority + index + 1,
			Weight:    1,
		})
	}
	return adds, matched, nil
}
