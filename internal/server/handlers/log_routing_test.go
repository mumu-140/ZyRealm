package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

func TestGetLogRoutingExplanationRejectsInvalidIDBeforeLookup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Params = gin.Params{{Key: "id", Value: "not-an-id"}}

	getLogRoutingExplanation(c)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", recorder.Code, recorder.Body.String())
	}
}

func TestLogRoutingExplanationEndpointRequiresAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/api/v1/log/:id/routing", middleware.Auth(), getLogRoutingExplanation)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/log/123/routing", nil)
	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s, want auth 401", recorder.Code, recorder.Body.String())
	}
}
