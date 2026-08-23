package balancer

import (
	"sync"
	"sync/atomic"
)

var globalConcurrency sync.Map // channel ID -> *atomic.Int64

func channelConcurrency(channelID int) *atomic.Int64 {
	value, _ := globalConcurrency.LoadOrStore(channelID, &atomic.Int64{})
	return value.(*atomic.Int64)
}

// TryAcquireChannel reserves one in-flight slot without exceeding maxConcurrency.
// maxConcurrency <= 0 表示不限并发：准入永远放行，但并发数照常统计。
// 「不限并发」只关掉准入上限检查，不能关掉计数——LeastUsed / P2C 靠
// CurrentChannelConcurrency 判负载，无限渠道若恒为 0 会被这两个策略持续偏爱。
func TryAcquireChannel(channelID, maxConcurrency int) bool {
	counter := channelConcurrency(channelID)
	if maxConcurrency <= 0 {
		counter.Add(1)
		return true
	}
	for {
		current := counter.Load()
		if current >= int64(maxConcurrency) {
			return false
		}
		if counter.CompareAndSwap(current, current+1) {
			return true
		}
	}
}

func ReleaseChannel(channelID int) {
	value, ok := globalConcurrency.Load(channelID)
	if !ok {
		return
	}
	counter := value.(*atomic.Int64)
	for {
		current := counter.Load()
		if current <= 0 {
			return
		}
		if counter.CompareAndSwap(current, current-1) {
			if current == 1 {
				globalConcurrency.CompareAndDelete(channelID, counter)
			}
			return
		}
	}
}

func CurrentChannelConcurrency(channelID int) int64 {
	value, ok := globalConcurrency.Load(channelID)
	if !ok {
		return 0
	}
	return value.(*atomic.Int64).Load()
}

func resetConcurrencyByChannel(channelID int) {
	globalConcurrency.Delete(channelID)
}
