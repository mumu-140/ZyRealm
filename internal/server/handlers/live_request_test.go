package handlers

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/gin-gonic/gin"
)

func TestListLiveRequestsReturnsDedicatedRequestsCollection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	listLiveRequests(c)

	if recorder.Code != 200 {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var envelope struct {
		Code int `json:"code"`
		Data struct {
			Requests []json.RawMessage `json:"requests"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Code != 200 {
		t.Fatalf("response code=%d", envelope.Code)
	}
	if envelope.Data.Requests == nil {
		t.Fatal("live request list must encode an empty collection as [] rather than null")
	}
}

func TestInterruptLiveRequestRejectsUnknownID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Params = gin.Params{{Key: "id", Value: "lr_missing_api_request"}}

	interruptLiveRequest(c)

	if recorder.Code != 404 {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var envelope resp.ResponseStruct
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Code != 404 {
		t.Fatalf("response code=%d, want 404", envelope.Code)
	}
}

func TestInterruptLiveRequestRejectsBlankID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Params = gin.Params{{Key: "id", Value: "   "}}

	interruptLiveRequest(c)

	if recorder.Code != 400 {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
