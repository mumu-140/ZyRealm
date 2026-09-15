package handlers

import (
	"net/http"
	"strings"

	"github.com/bestruirui/octopus/internal/relay"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/gin-gonic/gin"
)

func listLiveRequests(c *gin.Context) {
	resp.Success(c, gin.H{"requests": relay.ListLiveRequests()})
}

func interruptLiveRequest(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		resp.Error(c, http.StatusBadRequest, "invalid request id")
		return
	}
	if !relay.InterruptLiveRequest(id) {
		resp.Error(c, http.StatusNotFound, "live request not found")
		return
	}
	resp.Success(c, gin.H{
		"request_id":          id,
		"interrupt_requested": true,
	})
}
