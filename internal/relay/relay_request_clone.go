package relay

// cloneRelayRequestForAttempt preserves the old shallow-copy semantics for
// request-scoped pointers/slices while copying atomic state through Load/Store.
// This avoids copying sync/atomic.noCopy values by assignment, which go vet
// correctly rejects.
func cloneRelayRequestForAttempt(request *relayRequest) *relayRequest {
	if request == nil {
		return nil
	}
	cloned := &relayRequest{
		c:                    request.c,
		ctx:                  request.ctx,
		control:              request.control,
		inAdapter:            request.inAdapter,
		internalRequest:      request.internalRequest,
		metrics:              request.metrics,
		apiKeyID:             request.apiKeyID,
		requestModel:         request.requestModel,
		groupID:              request.groupID,
		groupSessionTTL:      request.groupSessionTTL,
		iter:                 request.iter,
		attemptBudget:        request.attemptBudget,
		templateHeaderSource: request.templateHeaderSource,
		rawBody:              request.rawBody,
		streamWriter:         request.streamWriter,
		heartbeat:            request.heartbeat,
	}
	cloned.streamPayloadWritten.Store(request.streamPayloadWritten.Load())
	cloned.responseCollected.Store(request.responseCollected.Load())
	return cloned
}
