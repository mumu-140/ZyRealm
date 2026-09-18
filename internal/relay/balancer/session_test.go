package balancer

import (
	"testing"
	"time"
)

func TestDeleteStickyRemovesSession(t *testing.T) {
	Reset()
	SetSticky(1, "gpt-4o", 10, 20)
	if entry := GetSticky(1, "gpt-4o", 60*time.Second); entry == nil || entry.ChannelKeyID != 20 {
		t.Fatalf("expected sticky session to exist before delete, got %#v", entry)
	}

	DeleteSticky(1, "gpt-4o")
	if entry := GetSticky(1, "gpt-4o", 60*time.Second); entry != nil {
		t.Fatalf("expected sticky session to be deleted, got %#v", entry)
	}
}

func TestResetStickyByChannelRemovesOnlyTargetChannel(t *testing.T) {
	Reset()
	SetSticky(1, "gpt-4o", 10, 100)
	SetSticky(2, "gpt-4o", 20, 200)
	SetSticky(3, "claude", 10, 300)

	ResetStateByChannel(10)

	if entry := GetSticky(1, "gpt-4o", time.Minute); entry != nil {
		t.Fatalf("expected target channel sticky session to be reset, got %#v", entry)
	}
	if entry := GetSticky(3, "claude", time.Minute); entry != nil {
		t.Fatalf("expected second target channel sticky session to be reset, got %#v", entry)
	}
	if entry := GetSticky(2, "gpt-4o", time.Minute); entry == nil || entry.ChannelID != 20 {
		t.Fatalf("expected unrelated sticky session to remain, got %#v", entry)
	}
}
