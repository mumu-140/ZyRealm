package relay

import (
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/outlierwindow"
)

// failureScope 一次失败应写入哪一级健康度证据。
// 渠道-模型健康度是唯一存储粒度；scopeChannel 表示「把这次失败铺到该渠道全部模型子键」。
type failureScope int

const (
	scopeIgnore  failureScope = iota // 与上游健康无关：不写入
	scopeModel                       // 只影响当前 (channel, model)
	scopeChannel                     // 影响整渠道：写入该渠道全部已知 model 键
)

// clientErrorMarkers 客户端侧错误特征：请求本身不合法，换渠道也不会成功，不算上游不健康。
var clientErrorMarkers = []string{
	"validate_thinking_parts_role",
	"missing required parameter",
	"unsupported parameter",
	"unsupported value",
	"is not one of",
}

// channelErrorMarkers 渠道级特征：与具体模型无关，整渠道当前不可用。
var channelErrorMarkers = []string{
	"insufficient_user_quota",
	"insufficient_quota",
	"用户额度不足",
	"额度不足",
	"invalid token",
	"invalid api key",
	"invalid_api_key",
	"authentication_error",
	"account_deactivated",
	"no available accounts",
}

// connectionErrorMarkers 连接层失败特征：请求没能完成一次真实的模型调用。
var connectionErrorMarkers = []string{
	"connection reset by peer",
	"broken pipe",
	"failed to send request",
	"dial tcp",
	"i/o timeout",
	"client.timeout exceeded",
	"no such host",
	"tls handshake",
	"use of closed network connection",
}

// modelErrorMarkers 模型级特征：同渠道其它模型可能完全正常。
var modelErrorMarkers = []string{
	"model_not_found",
	"model not found",
	"model_not_support",
	"does not exist or you do not have access",
	"get_channel_failed",
	"负载已经达到上限",
	"负载已达上限",
}

// classifyFailureScope 判定失败作用域。text 为「错误信息 + 上游错误体」的拼接，
// statusCode==0 表示连接层失败（未拿到 HTTP 响应）。
// 判定顺序即优先级：已知模型级本地超时 → 连接层 → 客户端/内容策略 → 模型能力不匹配 → 渠道凭据/额度 → 拦截页 → 模型 → 状态码兜底。
func classifyFailureScope(statusCode int, text string) failureScope {
	text = strings.ToLower(text)

	// first-token timeout 是当前 channel×model 的服务质量证据。虽然它通常
	// 没有 HTTP status（statusCode==0），也不能因此铺成整渠道失败。
	if strings.Contains(text, "first token timeout") {
		return scopeModel
	}
	if statusCode == 0 || containsAny(text, connectionErrorMarkers) {
		return scopeChannel
	}
	if isBlockedInvalidRequestError(text) || containsAny(text, clientErrorMarkers) || containsAny(text, contentPolicyMarkers) {
		return scopeIgnore
	}
	// Anthropic-compatible gateways can reject a newer MessageContent variant
	// before model execution. This is compatibility evidence, not provider-health
	// evidence, so keep outlier/circuit accounting neutral while routing retries.
	if isAnthropicPayloadSchemaMismatch(text) {
		return scopeIgnore
	}
	// Some relays wrap a model/effort capability error in an auth-shaped 401
	// envelope (for example code=invalid_api_key). The semantic capability
	// marker must win so a healthy credential/provider is not poisoned.
	if containsAny(text, modelCapabilityMarkers) {
		return scopeIgnore
	}
	if isUpstreamQuotaError(text) || isNoAvailableAccountError(text) || containsAny(text, channelErrorMarkers) {
		return scopeChannel
	}
	if isInterceptPageResponse(text) {
		return scopeChannel
	}
	if containsAny(text, modelErrorMarkers) ||
		isUpstreamContextLimitError(text) ||
		isUpstreamRateLimitError(text) ||
		needsConversationRestart(text) {
		return scopeModel
	}
	switch {
	case statusCode == 401 || statusCode == 402 || statusCode == 403:
		return scopeChannel // 凭据/计费类，报文未命中特征时按渠道级处理
	case statusCode >= 500:
		return scopeChannel // 网关/上游整体故障：502/503/504/52x 均非单模型问题
	default:
		return scopeModel // 其余 4xx：保守只惩罚当前模型
	}
}

// isInterceptPageResponse 识别 Cloudflare 拦截页与裸 HTML 响应体（上游未走到模型）。
func isInterceptPageResponse(text string) bool {
	if strings.Contains(text, "<html") || strings.Contains(text, "<!doctype html") {
		return true
	}
	return strings.Contains(text, "just a moment") ||
		strings.Contains(text, "attention required") ||
		strings.Contains(text, "cf-ray") ||
		strings.Contains(text, "cloudflare")
}

func containsAny(text string, markers []string) bool {
	for _, m := range markers {
		if strings.Contains(text, m) {
			return true
		}
	}
	return false
}

// reportOutlierDecision applies an already-decided outlier scope without
// reclassifying raw status/error text. Runtime consumers should use this after
// RoutingDecision has been computed so one wire result has exactly one policy
// verdict. Legacy callers/tests may still use reportOutlierFailure while the
// classifier migration is in progress.
func reportOutlierDecision(channelID int, modelName string, scope failureScope, statusCode int, now time.Time) {
	switch scope {
	case scopeIgnore:
		return
	case scopeChannel:
		outlierwindow.ReportChannel(channelID, modelName, false, statusCode, now)
	default:
		outlierwindow.Report(channelID, modelName, false, statusCode, now)
	}
}

// reportOutlierFailure is the compatibility entry point for callers that do
// not yet carry RoutingDecision. New relay-path code should use
// reportOutlierDecision with decision.OutlierScope.
func reportOutlierFailure(channelID int, modelName string, statusCode int, errText string, now time.Time) {
	reportOutlierDecision(channelID, modelName, classifyFailureScope(statusCode, errText), statusCode, now)
}

// outlierErrorText 拼接错误信息与上游报文并统一小写。
func outlierErrorText(err error, upstreamBody string) string {
	var b strings.Builder
	if err != nil {
		b.WriteString(err.Error())
	}
	if upstreamBody != "" {
		b.WriteString(" ")
		b.WriteString(upstreamBody)
	}
	return strings.ToLower(b.String())
}
