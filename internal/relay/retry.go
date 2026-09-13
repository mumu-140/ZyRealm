package relay

import (
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// Runtime recovery hints may outlive one request. Keep a conservative cap so
	// a malformed/malicious upstream cannot exile a candidate indefinitely.
	maxRuntimeRetryAfter = time.Hour
	// Same-request retries must stay short even when the upstream advertises a
	// long recovery window. Long hints belong in shared runtime availability.
	maxSameChannelRetryAfter = 60 * time.Second
)

// isRetryableStatus 判断 HTTP 状态码是否可重试
// 429(限流)、503(服务不可用)、>=500(服务端错误)、0(连接错误) 可重试
// 400/401/403/404 等客户端错误不可重试
func isRetryableStatus(code int) bool {
	return code == 0 || code == 429 || code >= 500
}

// isPassthroughStatus 判断是否应透传给下游客户端
// 429 和 503 透传，让客户端 SDK 的重试机制接管
func isPassthroughStatus(code int) bool {
	return code == 429 || code == 503
}

// parseRetryAfter parses both RFC 9110 Retry-After forms: delta-seconds and
// HTTP-date. The returned duration is suitable as a cross-request recovery hint
// and is capped independently from the much shorter same-request retry delay.
func parseRetryAfter(header string) time.Duration {
	return parseRetryAfterAt(header, time.Now())
}

func parseRetryAfterAt(header string, now time.Time) time.Duration {
	raw := strings.TrimSpace(header)
	if raw == "" {
		return 0
	}

	if secs, err := strconv.ParseInt(raw, 10, 64); err == nil {
		if secs <= 0 {
			return 0
		}
		maxSecs := int64(maxRuntimeRetryAfter / time.Second)
		if secs > maxSecs {
			secs = maxSecs
		}
		return time.Duration(secs) * time.Second
	}

	deadline, err := http.ParseTime(raw)
	if err != nil || !deadline.After(now) {
		return 0
	}
	if deadline.After(now.Add(maxRuntimeRetryAfter)) {
		return maxRuntimeRetryAfter
	}
	return deadline.Sub(now)
}

// computeBackoff 计算退避时间
// 优先使用 retryAfter（上游指定的等待时间），否则使用指数退避 + jitter
// retryNum 从 1 开始（第1次重试）
func computeBackoff(retryNum int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		if retryAfter > maxSameChannelRetryAfter {
			return maxSameChannelRetryAfter
		}
		return retryAfter
	}

	// 指数退避: 1s * 2^(retryNum-1)
	base := time.Second
	shift := retryNum - 1
	if shift > 5 {
		shift = 5
	}
	delay := base << shift

	if delay > maxSameChannelRetryAfter {
		delay = maxSameChannelRetryAfter
	}

	// 添加 10%-50% 的 jitter 防止惊群
	jitter := time.Duration(float64(delay) * (0.1 + rand.Float64()*0.4))
	return delay + jitter
}
