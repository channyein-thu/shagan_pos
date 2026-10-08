package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"shagan_pos/internal/middleware"
)

// do sends one request through CORS(origins) to a handler that returns 200.
func doCORS(method, origin string, origins []string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.CORS(origins))
	r.Any("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(method, "/x", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if method == http.MethodOptions {
		req.Header.Set("Access-Control-Request-Method", "POST")
		req.Header.Set("Access-Control-Request-Headers", "authorization,x-staff-token")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestParseCORSOrigins(t *testing.T) {
	assert.Equal(t, []string{"https://a.example", "https://b.example"},
		middleware.ParseCORSOrigins(" https://a.example/ , https://b.example,, "))
	assert.Empty(t, middleware.ParseCORSOrigins(""))
	assert.Empty(t, middleware.ParseCORSOrigins(" , "))
}

func TestCORS_ListedOrigin_IsEchoedWithTheFullHeaderSet(t *testing.T) {
	w := doCORS(http.MethodGet, "https://app.example", []string{"https://app.example"})

	assert.Equal(t, http.StatusOK, w.Code)
	h := w.Header()
	assert.Equal(t, "https://app.example", h.Get("Access-Control-Allow-Origin"), "the one origin, never *")
	assert.Contains(t, h.Get("Access-Control-Allow-Headers"), "X-Staff-Token")
	assert.Contains(t, h.Get("Access-Control-Allow-Headers"), "X-Manager-Approval-Token")
	assert.Contains(t, h.Get("Access-Control-Allow-Headers"), "If-None-Match")
	assert.Contains(t, h.Get("Access-Control-Allow-Headers"), "Authorization")
	assert.Equal(t, "ETag", h.Get("Access-Control-Expose-Headers"))
	assert.Empty(t, h.Get("Access-Control-Allow-Credentials"), "bearer-token API: credentials must never be allowed")
	assert.Contains(t, h.Values("Vary"), "Origin")
}

func TestCORS_UnlistedOrigin_GetsNoCORSHeaders(t *testing.T) {
	w := doCORS(http.MethodGet, "https://evil.example", []string{"https://app.example"})

	assert.Equal(t, http.StatusOK, w.Code, "the request itself still runs; the browser enforces the block")
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Headers"))
	assert.Contains(t, w.Header().Values("Vary"), "Origin")
}

func TestCORS_OriginMatchIsExactNotPrefixOrSubstring(t *testing.T) {
	origins := []string{"https://app.example"}
	for _, o := range []string{"https://app.example.evil.com", "http://app.example", "https://sub.app.example", "https://app.example:8443"} {
		assert.Empty(t, doCORS(http.MethodGet, o, origins).Header().Get("Access-Control-Allow-Origin"), o)
	}
}

func TestCORS_OriginMatchIsCaseInsensitive(t *testing.T) {
	w := doCORS(http.MethodGet, "https://APP.example", []string{"https://app.example"})
	assert.Equal(t, "https://APP.example", w.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORS_NoOriginHeader_IsUntouched(t *testing.T) {
	// Postman, curl and the frontends' server-side proxy send no Origin.
	w := doCORS(http.MethodGet, "", []string{"https://app.example"})
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORS_EmptyList_AllowsNoBrowserOrigin(t *testing.T) {
	w := doCORS(http.MethodGet, "https://app.example", nil)
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORS_ExplicitStar_OptsBackIntoAnyOrigin(t *testing.T) {
	w := doCORS(http.MethodGet, "https://anything.example", []string{"*"})
	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Credentials"))
}

func TestCORS_Preflight_ListedOrigin_Is204WithHeaders(t *testing.T) {
	w := doCORS(http.MethodOptions, "https://app.example", []string{"https://app.example"})
	require.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, "https://app.example", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Contains(t, w.Header().Get("Access-Control-Allow-Methods"), "PATCH")
	assert.Equal(t, "600", w.Header().Get("Access-Control-Max-Age"))
}

func TestCORS_Preflight_UnlistedOrigin_Is204WithoutHeaders(t *testing.T) {
	w := doCORS(http.MethodOptions, "https://evil.example", []string{"https://app.example"})
	require.Equal(t, http.StatusNoContent, w.Code)
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"), "no allow-origin on the preflight => the browser refuses to send the real request")
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Methods"))
}
