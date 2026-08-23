package balancer

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func TestChannelConcurrencyLimit(t *testing.T) {
	Reset()
	var acquired atomic.Int64
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if TryAcquireChannel(1, 3) {
				acquired.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := acquired.Load(); got != 3 {
		t.Fatalf("acquired %d slots, want 3", got)
	}
	if got := CurrentChannelConcurrency(1); got != 3 {
		t.Fatalf("current concurrency = %d, want 3", got)
	}
	for range 3 {
		ReleaseChannel(1)
	}
	if got := CurrentChannelConcurrency(1); got != 0 {
		t.Fatalf("current concurrency after release = %d, want 0", got)
	}
}

// TestUnlimitedChannelConcurrencyStillCounted maxConcurrency<=0 表示不限准入，
// 但并发数必须照常统计：LeastUsed / P2C 用 CurrentChannelConcurrency 判负载，
// 不统计会让无限渠道恒显示 0 负载而被永久偏爱。
func TestUnlimitedChannelConcurrencyStillCounted(t *testing.T) {
	Reset()
	for i := range 50 {
		if !TryAcquireChannel(2, 0) {
			t.Fatalf("第 %d 次准入被拒，无限渠道必须永远放行", i+1)
		}
	}
	if got := CurrentChannelConcurrency(2); got != 50 {
		t.Fatalf("无限渠道并发数 = %d, want 50（不限准入≠不统计）", got)
	}
	for range 50 {
		ReleaseChannel(2)
	}
	if got := CurrentChannelConcurrency(2); got != 0 {
		t.Fatalf("释放后并发数 = %d, want 0", got)
	}
}

// TestUnlimitedChannelReleaseNotNegative 释放次数超过占用次数时并发数不得为负。
func TestUnlimitedChannelReleaseNotNegative(t *testing.T) {
	Reset()
	TryAcquireChannel(3, 0)
	ReleaseChannel(3)
	ReleaseChannel(3)
	if got := CurrentChannelConcurrency(3); got != 0 {
		t.Fatalf("超额释放后并发数 = %d, want 0", got)
	}
}

// TestLeastUsedSeesUnlimitedChannelLoad 无限渠道满载时 LeastUsed 必须把它排在空闲渠道之后。
// 修复前 TryAcquireChannel 对 maxConcurrency<=0 直接 return true 不计数，
// 无限渠道恒为 0 并发，会被 LeastUsed 持续选中。
func TestLeastUsedSeesUnlimitedChannelLoad(t *testing.T) {
	Reset()
	for range 5 {
		TryAcquireChannel(801, 0) // 无限渠道，已有 5 个在途
	}
	items := []model.GroupItem{
		{ID: 1, Priority: 1, ChannelID: 801, ModelName: "m"},
		{ID: 2, Priority: 1, ChannelID: 802, ModelName: "m"}, // 空闲
	}
	got := (&LeastUsed{}).Candidates(items)
	if got[0].ChannelID != 802 {
		t.Fatalf("LeastUsed 首位 = %d, want 802（无限渠道负载未被看见）", got[0].ChannelID)
	}
}

// TestP2CDoesNotFavorUnlimitedChannel 两候选时 P2C 必然比较两者并发，
// 满载的无限渠道不得胜出。修复前它恒显示 0 并发，约半数请求会被它拿到首位。
func TestP2CDoesNotFavorUnlimitedChannel(t *testing.T) {
	Reset()
	for range 5 {
		TryAcquireChannel(811, 0)
	}
	items := []model.GroupItem{
		{ID: 1, Priority: 1, ChannelID: 811, ModelName: "m"},
		{ID: 2, Priority: 1, ChannelID: 812, ModelName: "m"},
	}
	b := &P2C{}
	for i := range 200 {
		if got := b.Candidates(items)[0].ChannelID; got != 812 {
			t.Fatalf("第 %d 次 P2C 首位 = %d, want 812（满载无限渠道被偏爱）", i+1, got)
		}
	}
}
