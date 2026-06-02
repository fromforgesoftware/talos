// Package http holds Talos's JSON:API controllers. The audit surface is
// read-only — events are appended over gRPC, never via REST.
package http

import (
	"net/http"
	"os"

	gws "github.com/gorilla/websocket"

	"github.com/fromforgesoftware/go-kit/auth/jwt"
	"github.com/fromforgesoftware/go-kit/openapi"
	"github.com/fromforgesoftware/go-kit/search/query"
	kitrest "github.com/fromforgesoftware/go-kit/transport/rest"

	"github.com/fromforgesoftware/talos/internal/api"
	"github.com/fromforgesoftware/talos/internal/app"
)

// AuditEventController exposes /api/audit-events as a read-only JSON:API
// collection (list + get-by-id) plus a multiplexed WebSocket live tail at
// /api/audit-events/stream, the browser-consumable side of the gRPC Subscribe.
type AuditEventController struct {
	events    app.AuditEventUsecase
	upgrader  gws.Upgrader
	validator jwt.Validator // nil = stream auth disabled
}

func NewAuditEventController(events app.AuditEventUsecase, validator jwt.Validator) kitrest.Controller {
	// Cross-site WebSocket hijacking guard: only the origins in
	// TALOS_WS_ALLOWED_ORIGINS may open the live tail. Secure by default —
	// when no allow-list is configured every origin is denied, unless the
	// operator explicitly opts into the insecure posture with
	// TALOS_WS_ALLOW_INSECURE=1 (development only), which allows all origins.
	allowed := parseOrigins(os.Getenv("TALOS_WS_ALLOWED_ORIGINS"))
	allowInsecure := os.Getenv("TALOS_WS_ALLOW_INSECURE") == "1"
	return &AuditEventController{
		events:    events,
		validator: validator,
		upgrader: gws.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return originAllowed(allowed, allowInsecure, r.Header.Get("Origin"))
			},
		},
	}
}

func (c *AuditEventController) Routes(r kitrest.Router) {
	r.Route("/api/audit-events", func(r kitrest.Router) {
		r.Get("", kitrest.NewJsonApiListHandler(
			c.events, api.AuditEventToDTO,
			kitrest.HandlerWithOpenAPI(
				openapi.Summary("List audit events"),
				openapi.Description("Filter with filter[actor], filter[resourceType], filter[resourceId], filter[action], and filter[timestamp] range bounds."),
				openapi.Tags("audit"),
			),
		))
		// Static /stream is registered before the /{id} param route so chi
		// matches it ahead of get-by-id.
		r.Get("/stream", http.HandlerFunc(c.stream))
		r.Get("/{id}", kitrest.NewJsonApiGetHandler(
			c.events, api.AuditEventToDTO, []query.ParseOpt{},
			kitrest.HandlerWithOpenAPI(openapi.Summary("Get an audit event"), openapi.Tags("audit"), openapi.Errors(404)),
		))
	})
}

// stream upgrades to a WebSocket and runs a multiplexed live-tail session: the
// client opens any number of filtered subscriptions over the one socket using
// the kit's Message envelope (see audit_stream.go).
func (c *AuditEventController) stream(w http.ResponseWriter, r *http.Request) {
	// When a validator is configured, require a valid bearer token. Browsers
	// can't set headers on a WebSocket handshake, so the token rides in the
	// access_token query param (Authorization is still accepted for non-browser
	// clients). Checked before the upgrade so a 401 is a plain HTTP response.
	if c.validator != nil {
		token := streamToken(r)
		if token == "" {
			http.Error(w, "missing access token", http.StatusUnauthorized)
			return
		}
		if _, err := c.validator.Validate(r.Context(), token); err != nil {
			http.Error(w, "invalid access token", http.StatusUnauthorized)
			return
		}
	}

	conn, err := c.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade has already written the error response.
	}
	newStreamSession(conn, c.events).run(r.Context())
}

// streamToken pulls the bearer token from the access_token query param or the
// Authorization header.
func streamToken(r *http.Request) string {
	if t := r.URL.Query().Get("access_token"); t != "" {
		return t
	}
	const prefix = "Bearer "
	if h := r.Header.Get("Authorization"); len(h) > len(prefix) && h[:len(prefix)] == prefix {
		return h[len(prefix):]
	}
	return ""
}
