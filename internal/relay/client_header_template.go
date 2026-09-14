package relay

import (
	"context"
	"net/http"

	"github.com/bestruirui/octopus/internal/headerutil"
	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/log"
)

type clientHeaderTemplateContextKey struct{}

func contextWithClientHeaderTemplateSource(ctx context.Context, source http.Header) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	snapshot := headerutil.SnapshotClientHeaderTemplateSource(source)
	if snapshot == nil {
		return ctx
	}
	return context.WithValue(ctx, clientHeaderTemplateContextKey{}, snapshot)
}

func clientHeaderTemplateSourceFromContext(ctx context.Context) http.Header {
	if ctx == nil {
		return nil
	}
	source, _ := ctx.Value(clientHeaderTemplateContextKey{}).(http.Header)
	return source
}

func (r *relayRequest) clientHeaderTemplateSource() http.Header {
	if r == nil {
		return nil
	}
	if r.templateHeaderSource != nil {
		return r.templateHeaderSource
	}
	if r.c != nil && r.c.Request != nil {
		return headerutil.SnapshotClientHeaderTemplateSource(r.c.Request.Header)
	}
	return nil
}

func (ra *relayAttempt) renderedChannelForWS() *dbmodel.Channel {
	if ra == nil || ra.channel == nil {
		return nil
	}
	cloned := *ra.channel
	cloned.CustomHeader = renderEffectiveCustomHeaders(ra.effectiveHeaders(), ra.clientHeaderTemplateSource())
	return &cloned
}

func renderedChannelForTemplateSource(channel *dbmodel.Channel, source http.Header) *dbmodel.Channel {
	if channel == nil {
		return nil
	}
	cloned := *channel
	cloned.CustomHeader = renderCustomHeaderSlice(channel.CustomHeader, source)
	return &cloned
}

func renderEffectiveCustomHeaders(headers map[string]string, source http.Header) []dbmodel.CustomHeader {
	if len(headers) == 0 {
		return nil
	}
	result := make([]dbmodel.CustomHeader, 0, len(headers))
	for key, value := range headers {
		rendered := renderConfiguredHeader(key, value, source)
		if rendered == nil {
			continue
		}
		result = append(result, *rendered)
	}
	return result
}

func renderCustomHeaderSlice(headers []dbmodel.CustomHeader, source http.Header) []dbmodel.CustomHeader {
	if len(headers) == 0 {
		return nil
	}
	result := make([]dbmodel.CustomHeader, 0, len(headers))
	for _, header := range headers {
		rendered := renderConfiguredHeader(header.HeaderKey, header.HeaderValue, source)
		if rendered == nil {
			continue
		}
		result = append(result, *rendered)
	}
	return result
}

func renderConfiguredHeader(key, value string, source http.Header) *dbmodel.CustomHeader {
	if headerutil.HasClientHeaderTemplate(value) && !headerutil.IsAllowedClientHeaderTemplateTarget(key) {
		log.Debugf("skipping client-header template for protected or invalid target %q", key)
		return nil
	}
	result := headerutil.RenderClientHeaderTemplate(value, source)
	if !result.Apply {
		log.Debugf("skipping client-header template for %q: %s", key, result.Reason)
		return nil
	}
	return &dbmodel.CustomHeader{HeaderKey: key, HeaderValue: result.Value}
}
