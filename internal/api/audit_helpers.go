// Package api: audit-event recording helpers (v1.0.0c).
//
// Mutating handlers across this package call one of the helpers below
// to record an event into s.audit. Two shapes matter:
//
//   - auditSuccess(r, action, resource)
//     The action completed; record actor + action + resource + ip.
//
//   - auditFailure(r, action, resource, err)
//     The action FAILED; record same fields plus the error message
//     as detail and result="failure". For internal errors (5xx) we
//     still log so an operator can see "matthew tried to delete
//     bucket foo and got a 500."
//
// Both helpers are best-effort: they pull the caller's claims from
// the request context for actor/role, the remote address from
// r.RemoteAddr (chi's middleware doesn't rewrite this), and forward
// to s.audit.Log. They never block or fail the calling handler —
// the audit Logger swallows write errors and emits a slog warning.
//
// Failed-auth events (action="auth:login" / "auth:forbidden") use
// auditFailure with actor="" since by definition there is no
// authenticated caller yet.

package api

import (
	"net/http"

	"github.com/mattjackson/basement/internal/audit"
	"github.com/mattjackson/basement/internal/auth"
)

// auditSuccess records a successful mutating action. Caller-supplied
// action and resource follow the conventions in the v1.0.0c spec
// (e.g. "bucket:delete", "bucket:{cid}:{bid}").
func (s *Server) auditSuccess(r *http.Request, action, resource string) {
	s.auditEmit(r, action, resource, audit.ResultSuccess, "")
}

// auditFailure records a failed mutating action plus the error
// message as Detail. err==nil is tolerated (recorded as empty
// detail) so callers can use this for forbidden-style outcomes
// where the "error" is policy not an exception.
func (s *Server) auditFailure(r *http.Request, action, resource string, err error) {
	detail := ""
	if err != nil {
		detail = err.Error()
	}
	s.auditEmit(r, action, resource, audit.ResultFailure, detail)
}

// auditFailureDetail records a failed mutating action with a
// caller-provided detail string. Used for code paths where the
// "error" is descriptive text rather than a Go error (e.g.
// "X-Confirm-Delete header missing").
func (s *Server) auditFailureDetail(r *http.Request, action, resource, detail string) {
	s.auditEmit(r, action, resource, audit.ResultFailure, detail)
}

// auditEmit is the single funnel; the typed wrappers above keep the
// call sites readable.
func (s *Server) auditEmit(r *http.Request, action, resource, result, detail string) {
	if s.audit == nil {
		return
	}

	actor := ""
	actorRole := ""
	if claims, ok := auth.FromContext(r.Context()); ok && claims != nil {
		actor = claims.UserID
		actorRole = claims.Role
	}

	s.audit.Log(audit.Event{
		Actor:     actor,
		ActorRole: actorRole,
		Action:    action,
		Resource:  resource,
		Result:    result,
		Detail:    detail,
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
	})
}

// clientIP returns the request's remote address, stripping the
// trailing port if present. This is the best-effort source IP —
// reverse proxies put the real client in X-Forwarded-For, but
// basement doesn't currently parse that header (no trust list).
// For an unraid / docker-compose typical deploy r.RemoteAddr is
// the container-network address, which is enough for "this came
// from inside the cluster vs. the public internet."
func clientIP(r *http.Request) string {
	ra := r.RemoteAddr
	if ra == "" {
		return ""
	}
	// Strip port if present. net.SplitHostPort would be the
	// proper way but the audit log is forgiving — we don't want
	// to import net just for this and we don't want to fail on
	// an unexpected shape.
	for i := len(ra) - 1; i >= 0; i-- {
		if ra[i] == ':' {
			return ra[:i]
		}
		if ra[i] < '0' || ra[i] > '9' {
			break
		}
	}
	return ra
}

// resourceCluster / resourceBucket / resourceKey / resourceShare /
// resourceSync / resourceUser / resourceRole / resourceAssignment
// build the canonical Resource string for each domain. Centralised
// so future scope-grammar changes update every call site.
func resourceCluster(cid string) string   { return "cluster:" + cid }
func resourceBucket(cid, bid string) string { return "bucket:" + cid + ":" + bid }
func resourceKey(cid, kid string) string   { return "key:" + cid + ":" + kid }
func resourceShare(token string) string    { return "share:" + token }
func resourceSync(jobID string) string     { return "sync:" + jobID }
func resourceUser(uname string) string     { return "user:" + uname }
func resourceRole(roleID string) string    { return "role:" + roleID }
func resourceAssignment(userID, roleID, scope string) string {
	return "assignment:" + userID + ":" + roleID + ":" + scope
}

// resourceHost is the singleton resource for org-capability edits.
const resourceHost = "host:*"
