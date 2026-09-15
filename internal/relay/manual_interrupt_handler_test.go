package relay

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRelayRequestCloneSharesControlContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	parent, parentCancel := context.WithCancel(context.Background())
	defer parentCancel()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(parent)
	control := newRelayControl(parent, LiveRequestSnapshot{RequestID: "lr_clone_control"})
	request := &relayRequest{c: c, control: control}

	clone := cloneRelayRequestForAttempt(request)
	if clone.control != control {
		t.Fatal("attempt clone must share the logical request control pointer")
	}
	if clone.requestContext() != control.Context() {
		t.Fatal("control context must be authoritative for cloned relay execution")
	}

	control.Interrupt()
	<-clone.requestContext().Done()
	if !errors.Is(context.Cause(clone.requestContext()), errManualInterrupt) {
		t.Fatalf("clone context cause = %v", context.Cause(clone.requestContext()))
	}
	if parent.Err() != nil {
		t.Fatalf("interrupting clone control canceled ingress parent: %v", parent.Err())
	}
}

func TestRelayRequestContextFallsBackToIngressWithoutControl(t *testing.T) {
	gin.SetMode(gin.TestMode)
	parent := context.WithValue(context.Background(), struct{}{}, "ingress")
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(parent)

	request := &relayRequest{c: c}
	if request.requestContext() != parent {
		t.Fatal("request without control must preserve existing ingress context semantics")
	}
}
