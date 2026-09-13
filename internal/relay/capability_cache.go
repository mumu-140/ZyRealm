package relay

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/protocolroute"
	"github.com/bestruirui/octopus/internal/relay/availability"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
)

const (
	capabilitySignatureVersion        = "cap-v1"
	capabilityConfigVersion           = "cfg-v1"
	capabilityNegativeCacheSkipPrefix = "capability negative cache"
)

var capabilityContentFields = map[string]struct{}{
	"Model":              {},
	"Messages":           {},
	"EmbeddingInput":     {},
	"RawInputItems":      {},
	"Conversation":       {},
	"ProviderExtensions": {},
}

type capabilityShape struct {
	Version                string                            `json:"version"`
	IngressProtocol        string                            `json:"ingress_protocol"`
	UpstreamProtocol       string                            `json:"upstream_protocol"`
	Features               protocolroute.RequestFeatureFlags `json:"features"`
	PresentParams          []string                          `json:"present_params,omitempty"`
	MessageCapabilities    []string                          `json:"message_capabilities,omitempty"`
	ReasoningEffort        string                            `json:"reasoning_effort,omitempty"`
	ReasoningBudget        string                            `json:"reasoning_budget,omitempty"`
	ThinkingType           string                            `json:"thinking_type,omitempty"`
	EnableThinking         string                            `json:"enable_thinking,omitempty"`
	ThinkingDisplay        string                            `json:"thinking_display,omitempty"`
	Verbosity              string                            `json:"verbosity,omitempty"`
	Modalities             []string                          `json:"modalities,omitempty"`
	Audio                  string                            `json:"audio,omitempty"`
	ResponseFormatHash     string                            `json:"response_format_hash,omitempty"`
	ServiceTier            string                            `json:"service_tier,omitempty"`
	Truncation             string                            `json:"truncation,omitempty"`
	ParallelToolCalls      string                            `json:"parallel_tool_calls,omitempty"`
	ToolsHash              string                            `json:"tools_hash,omitempty"`
	ToolChoiceHash         string                            `json:"tool_choice_hash,omitempty"`
	Include                []string                          `json:"include,omitempty"`
	WebSearchOptionsHash   string                            `json:"web_search_options_hash,omitempty"`
	ExtraBodyHash          string                            `json:"extra_body_hash,omitempty"`
	ProviderExtensionsHash string                            `json:"provider_extensions_hash,omitempty"`
	PromptHash             string                            `json:"prompt_hash,omitempty"`
	ContextManagementHash  string                            `json:"context_management_hash,omitempty"`
	Background             string                            `json:"background,omitempty"`
	MaxToolCalls           string                            `json:"max_tool_calls,omitempty"`
	ReasoningSummary       string                            `json:"reasoning_summary,omitempty"`
	ReasoningGenerate      string                            `json:"reasoning_generate_summary,omitempty"`
}

type capabilityConfigShape struct {
	Version           string   `json:"version"`
	ChannelType       string   `json:"channel_type"`
	BaseURLs          []string `json:"base_urls,omitempty"`
	PlanBaseURL       string   `json:"plan_base_url,omitempty"`
	Model             string   `json:"model,omitempty"`
	CustomModel       string   `json:"custom_model,omitempty"`
	Headers           []string `json:"headers,omitempty"`
	WSMode            string   `json:"ws_mode,omitempty"`
	ParamOverride     string   `json:"param_override,omitempty"`
	PlanParamOverride string   `json:"plan_param_override,omitempty"`
	ProxyMode         string   `json:"proxy_mode,omitempty"`
	ProxyLegacy       bool     `json:"proxy_legacy,omitempty"`
	ProxyConfigID     string   `json:"proxy_config_id,omitempty"`
	ChannelProxy      string   `json:"channel_proxy,omitempty"`
	ConfigRevision    int64    `json:"config_revision,omitempty"`
}

func capabilitySignature(request *transformerModel.InternalLLMRequest, plan *protocolroute.AttemptPlan) string {
	if request == nil || plan == nil {
		return ""
	}
	shape := capabilityShape{
		Version:                capabilitySignatureVersion,
		IngressProtocol:        fmt.Sprint(plan.IngressProtocol()),
		UpstreamProtocol:       fmt.Sprint(plan.UpstreamProtocol()),
		Features:               plan.Features(),
		PresentParams:          requestCapabilityParamNames(request),
		MessageCapabilities:    messageCapabilityDescriptors(request.Messages),
		ReasoningEffort:        strings.ToLower(strings.TrimSpace(request.ReasoningEffort)),
		ThinkingDisplay:        strings.ToLower(strings.TrimSpace(request.ThinkingDisplay)),
		Modalities:             normalizedStrings(request.Modalities),
		Include:                normalizedStrings(request.Include),
		ResponseFormatHash:     jsonHash(request.ResponseFormat),
		ToolsHash:              jsonHash(request.Tools),
		ToolChoiceHash:         jsonHash(request.ToolChoice),
		WebSearchOptionsHash:   rawJSONHash(request.WebSearchOptions),
		ExtraBodyHash:          rawJSONHash(request.ExtraBody),
		ProviderExtensionsHash: jsonHash(request.ProviderExtensions),
		PromptHash:             rawJSONHash(request.Prompt),
		ContextManagementHash:  rawJSONHash(request.ContextManagement),
		ReasoningSummary:       normalizedOptionalString(request.ReasoningSummary),
		ReasoningGenerate:      normalizedOptionalString(request.ReasoningGenerateSummary),
	}
	if request.ReasoningBudget != nil {
		shape.ReasoningBudget = fmt.Sprint(*request.ReasoningBudget)
	}
	if request.Thinking != nil {
		shape.ThinkingType = strings.ToLower(strings.TrimSpace(request.Thinking.Type))
	}
	if request.EnableThinking != nil {
		shape.EnableThinking = fmt.Sprint(*request.EnableThinking)
	}
	if request.Verbosity != nil {
		shape.Verbosity = strings.ToLower(strings.TrimSpace(*request.Verbosity))
	}
	if request.Audio != nil {
		shape.Audio = strings.ToLower(strings.TrimSpace(request.Audio.Format)) + ":" + strings.ToLower(strings.TrimSpace(request.Audio.Voice))
	}
	if request.ServiceTier != nil {
		shape.ServiceTier = strings.ToLower(strings.TrimSpace(*request.ServiceTier))
	}
	if request.Truncation != nil {
		shape.Truncation = strings.ToLower(strings.TrimSpace(*request.Truncation))
	}
	if request.ParallelToolCalls != nil {
		shape.ParallelToolCalls = fmt.Sprint(*request.ParallelToolCalls)
	}
	if request.Background != nil {
		shape.Background = fmt.Sprint(*request.Background)
	}
	if request.MaxToolCalls != nil {
		shape.MaxToolCalls = fmt.Sprint(*request.MaxToolCalls)
	}
	return stableDigest(capabilitySignatureVersion, shape)
}

func capabilityConfigFingerprint(channel *dbmodel.Channel, plan *protocolroute.AttemptPlan) string {
	if channel == nil || plan == nil {
		return ""
	}
	baseURLs := make([]string, 0, len(channel.BaseUrls))
	for _, baseURL := range channel.BaseUrls {
		if value := strings.TrimSpace(baseURL.URL); value != "" {
			baseURLs = append(baseURLs, value)
		}
	}
	sort.Strings(baseURLs)

	headers := make([]string, 0, len(plan.HeaderPolicy().Set))
	for key, value := range plan.HeaderPolicy().Set {
		headers = append(headers, strings.ToLower(strings.TrimSpace(key))+"="+value)
	}
	sort.Strings(headers)

	shape := capabilityConfigShape{
		Version:           capabilityConfigVersion,
		ChannelType:       fmt.Sprint(channel.Type),
		BaseURLs:          baseURLs,
		PlanBaseURL:       strings.TrimSpace(plan.BaseURL()),
		Model:             channel.Model,
		CustomModel:       channel.CustomModel,
		Headers:           headers,
		WSMode:            string(channel.WSMode.Normalize()),
		PlanParamOverride: rawJSONHash(plan.ParamOverride()),
		ProxyMode:         fmt.Sprint(channel.ProxyMode),
		ProxyLegacy:       channel.Proxy,
		ConfigRevision:    plan.ConfigRevision(),
	}
	if channel.ParamOverride != nil {
		shape.ParamOverride = bytesDigest([]byte(strings.TrimSpace(*channel.ParamOverride)))
	}
	if channel.ProxyConfigID != nil {
		shape.ProxyConfigID = fmt.Sprint(*channel.ProxyConfigID)
	}
	if channel.ChannelProxy != nil {
		shape.ChannelProxy = strings.TrimSpace(*channel.ChannelProxy)
	}
	return stableDigest(capabilityConfigVersion, shape)
}

func filterCapabilityNegativePlans(
	channel *dbmodel.Channel,
	request *transformerModel.InternalLLMRequest,
	plans []*protocolroute.AttemptPlan,
	now time.Time,
) ([]*protocolroute.AttemptPlan, availability.CapabilitySnapshot, bool) {
	if len(plans) == 0 || channel == nil || request == nil {
		return plans, availability.CapabilitySnapshot{}, false
	}
	filtered := make([]*protocolroute.AttemptPlan, 0, len(plans))
	var nearest availability.CapabilitySnapshot
	blockedAny := false
	for _, plan := range plans {
		if plan == nil {
			continue
		}
		info := availability.CapabilityInfo(
			channel.ID,
			plan.UpstreamModel(),
			capabilitySignature(request, plan),
			capabilityConfigFingerprint(channel, plan),
			now,
		)
		if !info.Blocked {
			filtered = append(filtered, plan)
			continue
		}
		blockedAny = true
		if nearest.ExpiresAt.IsZero() || info.ExpiresAt.Before(nearest.ExpiresAt) {
			nearest = info
		}
	}
	return filtered, nearest, blockedAny
}

func recordCapabilityNegative(
	channel *dbmodel.Channel,
	request *transformerModel.InternalLLMRequest,
	plan *protocolroute.AttemptPlan,
	result attemptResult,
	now time.Time,
) time.Time {
	if channel == nil || request == nil || plan == nil {
		return time.Time{}
	}
	return availability.RecordCapabilityNegative(
		channel.ID,
		plan.UpstreamModel(),
		capabilitySignature(request, plan),
		capabilityConfigFingerprint(channel, plan),
		compactCapabilityReason(outlierErrorText(result.Err, result.UpstreamErrorBody)),
		now,
	)
}

func clearCapabilityNegative(channel *dbmodel.Channel, request *transformerModel.InternalLLMRequest, plan *protocolroute.AttemptPlan) {
	if channel == nil || request == nil || plan == nil {
		return
	}
	availability.ClearCapabilityNegative(
		channel.ID,
		plan.UpstreamModel(),
		capabilitySignature(request, plan),
		capabilityConfigFingerprint(channel, plan),
	)
}

func capabilityNegativeCacheSkipReason(info availability.CapabilitySnapshot) string {
	if info.ExpiresAt.IsZero() {
		return capabilityNegativeCacheSkipPrefix
	}
	return fmt.Sprintf("%s until %s", capabilityNegativeCacheSkipPrefix, info.ExpiresAt.UTC().Format(time.RFC3339))
}

func hasCapabilityNegativeCacheSkip(attempts []dbmodel.ChannelAttempt) bool {
	for _, attempt := range attempts {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(attempt.Msg)), capabilityNegativeCacheSkipPrefix) {
			return true
		}
	}
	return false
}

func requestCapabilityParamNames(request *transformerModel.InternalLLMRequest) []string {
	if request == nil {
		return nil
	}
	value := reflect.ValueOf(*request)
	typeOf := value.Type()
	out := make([]string, 0, typeOf.NumField())
	for index := 0; index < typeOf.NumField(); index++ {
		fieldType := typeOf.Field(index)
		if _, excluded := capabilityContentFields[fieldType.Name]; excluded {
			continue
		}
		jsonTag := strings.Split(fieldType.Tag.Get("json"), ",")[0]
		if jsonTag == "-" {
			continue
		}
		if !capabilityValuePresent(value.Field(index)) {
			continue
		}
		out = append(out, fieldType.Name)
	}
	sort.Strings(out)
	return out
}

func capabilityValuePresent(value reflect.Value) bool {
	if !value.IsValid() {
		return false
	}
	switch value.Kind() {
	case reflect.Pointer, reflect.Interface:
		return !value.IsNil()
	case reflect.Slice, reflect.Map:
		return !value.IsNil() && value.Len() > 0
	case reflect.String:
		return value.Len() > 0
	case reflect.Bool:
		return value.Bool()
	default:
		return !value.IsZero()
	}
}

func messageCapabilityDescriptors(messages []transformerModel.Message) []string {
	if len(messages) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	add := func(value string) {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			seen[value] = struct{}{}
		}
	}
	for _, message := range messages {
		add("role:" + message.Role)
		if message.Content.Content != nil {
			add("content:text")
		}
		for _, part := range message.Content.MultipleContent {
			partType := part.Type
			if partType == "" && part.Document != nil {
				partType = "document"
			}
			if partType == "" && part.ServerToolUse != nil {
				partType = "server_tool_use"
			}
			if partType == "" && part.ServerToolResult != nil {
				partType = "server_tool_result"
			}
			add("content:" + partType)
			if part.Document != nil {
				add("document:" + part.Document.Type)
			}
			if part.ServerToolUse != nil {
				add("server_tool_use:" + part.ServerToolUse.Name)
			}
			if part.ServerToolResult != nil {
				add("server_tool_result:" + part.ServerToolResult.BlockType)
			}
		}
		if len(message.ToolCalls) > 0 {
			add("assistant:tool_calls")
		}
		if message.ToolCallID != nil {
			add("tool:result")
		}
		if message.ReasoningContent != nil || message.Reasoning != nil || len(message.ReasoningBlocks) > 0 {
			add("history:reasoning")
		}
		for _, block := range message.ReasoningBlocks {
			add("reasoning_block:" + string(block.Kind))
			add("reasoning_provider:" + block.Provider)
		}
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func normalizedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if normalized := strings.ToLower(strings.TrimSpace(value)); normalized != "" {
			out = append(out, normalized)
		}
	}
	sort.Strings(out)
	return out
}

func normalizedOptionalString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(*value))
}

func rawJSONHash(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	return bytesDigest(raw)
}

func jsonHash(value any) string {
	if value == nil {
		return ""
	}
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) == 0 || string(encoded) == "null" || string(encoded) == "[]" || string(encoded) == "{}" {
		return ""
	}
	return bytesDigest(encoded)
}

func stableDigest(prefix string, value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return prefix + ":" + bytesDigest(encoded)
}

func bytesDigest(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func compactCapabilityReason(reason string) string {
	reason = strings.TrimSpace(reason)
	const maxReasonBytes = 256
	if len(reason) > maxReasonBytes {
		return reason[:maxReasonBytes]
	}
	return reason
}
