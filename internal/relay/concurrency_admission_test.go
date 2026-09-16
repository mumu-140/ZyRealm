package relay

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/relay/availability"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/gin-gonic/gin"
)

func TestHandlerSaturatedCandidateDoesNotChargeCredentialFairness(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)
	availability.Reset()
	t.Cleanup(availability.Reset)

	var saturatedHits atomic.Int32
	saturatedUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		saturatedHits.Add(1)
		writeChatAdmissionSuccess(w, "admission-model")
	}))
	defer saturatedUpstream.Close()

	var fallbackHits atomic.Int32
	fallbackUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fallbackHits.Add(1)
		writeChatAdmissionSuccess(w, "admission-model")
	}))
	defer fallbackUpstream.Close()

	saturated := &dbmodel.Channel{
		Name:           "admission-saturated-chat",
		Type:           outbound.OutboundTypeOpenAIChat,
		Enabled:        true,
		BaseUrls:       []dbmodel.BaseUrl{{URL: saturatedUpstream.URL + "/v1"}},
		Model:          "admission-model",
		MaxConcurrency: 1,
		Keys: []dbmodel.ChannelKey{
			{Enabled: true, ChannelKey: "admission-saturated-key-1"},
			{Enabled: true, ChannelKey: "admission-saturated-key-2"},
		},
	}
	if err := op.ChannelCreate(saturated, ctx); err != nil {
		t.Fatalf("create saturated channel: %v", err)
	}
	saturated, err := op.ChannelGet(saturated.ID, ctx)
	if err != nil {
		t.Fatalf("reload saturated channel: %v", err)
	}

	fallback := &dbmodel.Channel{
		Name:     "admission-fallback-chat",
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []dbmodel.BaseUrl{{URL: fallbackUpstream.URL + "/v1"}},
		Model:    "admission-model",
		Keys:     []dbmodel.ChannelKey{{Enabled: true, ChannelKey: "admission-fallback-key"}},
	}
	if err := op.ChannelCreate(fallback, ctx); err != nil {
		t.Fatalf("create fallback channel: %v", err)
	}

	group := &dbmodel.Group{Name: "admission-chat-group", Mode: dbmodel.GroupModeFailover}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := op.GroupItemAdd(&dbmodel.GroupItem{
		GroupID: group.ID, ChannelID: saturated.ID, ModelName: "admission-model", Priority: 1, Weight: 1,
	}, ctx); err != nil {
		t.Fatalf("add saturated group item: %v", err)
	}
	if err := op.GroupItemAdd(&dbmodel.GroupItem{
		GroupID: group.ID, ChannelID: fallback.ID, ModelName: "admission-model", Priority: 2, Weight: 1,
	}, ctx); err != nil {
		t.Fatalf("add fallback group item: %v", err)
	}

	if !balancer.TryAcquireChannel(saturated.ID, saturated.MaxConcurrency) {
		t.Fatal("failed to reserve saturated channel slot")
	}
	t.Cleanup(func() { balancer.ReleaseChannel(saturated.ID) })

	body, err := json.Marshal(map[string]any{
		"model": "admission-chat-group",
		"messages": []map[string]string{{
			"role": "user", "content": "hello",
		}},
	})
	if err != nil {
		t.Fatalf("marshal chat request: %v", err)
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	Handler(inbound.InboundTypeOpenAIChat, c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if got := saturatedHits.Load(); got != 0 {
		t.Fatalf("saturated upstream hits = %d, want 0", got)
	}
	if got := fallbackHits.Load(); got != 1 {
		t.Fatalf("fallback upstream hits = %d, want 1", got)
	}

	wantFirstKeyID := lowestEligibleCredentialID(saturated.Keys)
	selected := availability.SelectCredentialFair(saturated.ID, saturated.Keys, 0)
	if selected.ID != wantFirstKeyID {
		t.Fatalf(
			"first post-request fair credential = %d, want %d; a candidate rejected for max concurrency must not advance credential fairness",
			selected.ID,
			wantFirstKeyID,
		)
	}
}

func TestImagesHandlerSaturatedCandidateDoesNotChargeCredentialFairness(t *testing.T) {
	ginTestMode(t)
	ctx := setupRelayTestDB(t)
	availability.Reset()
	t.Cleanup(availability.Reset)

	var saturatedHits atomic.Int32
	saturatedUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		saturatedHits.Add(1)
		writeImagesSuccess(w)
	}))
	defer saturatedUpstream.Close()

	var fallbackHits atomic.Int32
	fallbackUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fallbackHits.Add(1)
		writeImagesSuccess(w)
	}))
	defer fallbackUpstream.Close()

	saturated := newImagesTestChannel("admission-saturated-image", saturatedUpstream.URL)
	saturated.MaxConcurrency = 1
	saturated.Keys = []dbmodel.ChannelKey{
		{Enabled: true, ChannelKey: "admission-image-key-1"},
		{Enabled: true, ChannelKey: "admission-image-key-2"},
	}
	fallback := newImagesTestChannel("admission-fallback-image", fallbackUpstream.URL)
	group := &dbmodel.Group{Name: "admission-image-group", Mode: dbmodel.GroupModeFailover}
	created := persistImagesRoute(t, ctx, group, saturated, fallback)
	saturated = created[0]

	if !balancer.TryAcquireChannel(saturated.ID, saturated.MaxConcurrency) {
		t.Fatal("failed to reserve saturated image channel slot")
	}
	t.Cleanup(func() { balancer.ReleaseChannel(saturated.ID) })

	body, err := json.Marshal(map[string]string{
		"model":  "admission-image-group",
		"prompt": "draw",
	})
	if err != nil {
		t.Fatalf("marshal image request: %v", err)
	}
	recorder, c := newImagesTestContext(
		"/v1/images/generations",
		body,
		"application/json",
	)
	c.Set("api_key_id", 2001)
	ImagesHandler("/images/generations", c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if got := saturatedHits.Load(); got != 0 {
		t.Fatalf("saturated image upstream hits = %d, want 0", got)
	}
	if got := fallbackHits.Load(); got != 1 {
		t.Fatalf("fallback image upstream hits = %d, want 1", got)
	}

	wantFirstKeyID := lowestEligibleCredentialID(saturated.Keys)
	selected := availability.SelectCredentialFair(saturated.ID, saturated.Keys, 0)
	if selected.ID != wantFirstKeyID {
		t.Fatalf(
			"first post-request fair image credential = %d, want %d; a candidate rejected for max concurrency must not advance credential fairness",
			selected.ID,
			wantFirstKeyID,
		)
	}
}

func TestLoadAwareModesPreferCapacityHeadroomOverLowerAbsoluteInflight(t *testing.T) {
	modes := []struct {
		name string
		mode dbmodel.GroupMode
	}{
		{name: "least-used", mode: dbmodel.GroupModeLeastUsed},
		{name: "p2c", mode: dbmodel.GroupModeP2C},
	}

	for _, tc := range modes {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			ctx := setupRelayTestDB(t)
			availability.Reset()
			t.Cleanup(availability.Reset)

			var tightHits atomic.Int32
			tightUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				tightHits.Add(1)
				writeChatAdmissionSuccess(w, "headroom-model")
			}))
			defer tightUpstream.Close()

			var roomyHits atomic.Int32
			roomyUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				roomyHits.Add(1)
				writeChatAdmissionSuccess(w, "headroom-model")
			}))
			defer roomyUpstream.Close()

			tight := &dbmodel.Channel{
				Name:           "headroom-tight-" + tc.name,
				Type:           outbound.OutboundTypeOpenAIChat,
				Enabled:        true,
				BaseUrls:       []dbmodel.BaseUrl{{URL: tightUpstream.URL + "/v1"}},
				Model:          "headroom-model",
				MaxConcurrency: 3,
				Keys:           []dbmodel.ChannelKey{{Enabled: true, ChannelKey: "headroom-tight-key"}},
			}
			roomy := &dbmodel.Channel{
				Name:           "headroom-roomy-" + tc.name,
				Type:           outbound.OutboundTypeOpenAIChat,
				Enabled:        true,
				BaseUrls:       []dbmodel.BaseUrl{{URL: roomyUpstream.URL + "/v1"}},
				Model:          "headroom-model",
				MaxConcurrency: 10,
				Keys:           []dbmodel.ChannelKey{{Enabled: true, ChannelKey: "headroom-roomy-key"}},
			}
			if err := op.ChannelCreate(tight, ctx); err != nil {
				t.Fatalf("create tight channel: %v", err)
			}
			if err := op.ChannelCreate(roomy, ctx); err != nil {
				t.Fatalf("create roomy channel: %v", err)
			}

			groupName := "headroom-group-" + tc.name
			group := &dbmodel.Group{Name: groupName, Mode: tc.mode}
			if err := op.GroupCreate(group, ctx); err != nil {
				t.Fatalf("create group: %v", err)
			}
			for _, channelID := range []int{tight.ID, roomy.ID} {
				if err := op.GroupItemAdd(&dbmodel.GroupItem{
					GroupID: group.ID, ChannelID: channelID, ModelName: "headroom-model", Priority: 1, Weight: 1,
				}, ctx); err != nil {
					t.Fatalf("add group item: %v", err)
				}
			}

			reserveChannelSlots(t, tight.ID, tight.MaxConcurrency, 2)
			reserveChannelSlots(t, roomy.ID, roomy.MaxConcurrency, 4)

			body, err := json.Marshal(map[string]any{
				"model": groupName,
				"messages": []map[string]string{{
					"role": "user", "content": "hello",
				}},
			})
			if err != nil {
				t.Fatalf("marshal request: %v", err)
			}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			Handler(inbound.InboundTypeOpenAIChat, c)

			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
			}
			if got := tightHits.Load(); got != 0 {
				t.Fatalf("tight channel hits = %d, want 0; 2/3 utilization must rank behind 4/10", got)
			}
			if got := roomyHits.Load(); got != 1 {
				t.Fatalf("roomy channel hits = %d, want 1; 4/10 utilization should win", got)
			}
		})
	}
}

func reserveChannelSlots(t *testing.T, channelID, maxConcurrency, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		if !balancer.TryAcquireChannel(channelID, maxConcurrency) {
			t.Fatalf("reserve channel %d slot %d/%d", channelID, i+1, count)
		}
	}
	t.Cleanup(func() {
		for i := 0; i < count; i++ {
			balancer.ReleaseChannel(channelID)
		}
	})
}

func lowestEligibleCredentialID(keys []dbmodel.ChannelKey) int {
	lowest := 0
	for _, key := range keys {
		if key.ID <= 0 || !key.Enabled || key.ChannelKey == "" {
			continue
		}
		if lowest == 0 || key.ID < lowest {
			lowest = key.ID
		}
	}
	return lowest
}

func writeChatAdmissionSuccess(w http.ResponseWriter, modelName string) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"id":      "resp_ok",
		"object":  "chat.completion",
		"created": 1,
		"model":   modelName,
		"choices": []map[string]any{{
			"index": 0,
			"message": map[string]string{
				"role": "assistant", "content": "ok",
			},
		}},
	}); err != nil {
		return
	}
}
