package httpapi

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
)

const allowedOriginsEnvironment = "WARMMO_ALLOWED_ORIGINS"

var developmentOrigins = []string{
	"http://127.0.0.1:5173",
	"http://localhost:5173",
}

type LocalSecurity struct {
	allowedOrigins map[string]struct{}
	sessionToken   string
}

func NewLocalSecurityFromEnvironment(releaseOrigin string) (*LocalSecurity, error) {
	origins := developmentOrigins
	if trimmed := strings.TrimSpace(releaseOrigin); trimmed != "" {
		origins = []string{trimmed}
		return NewLocalSecurity(origins)
	}
	return NewLocalSecurityWithDefaults(origins)
}

func NewLocalSecurityWithDefaults(origins []string) (*LocalSecurity, error) {
	for origin := range strings.SplitSeq(os.Getenv(allowedOriginsEnvironment), ",") {
		if trimmed := strings.TrimSpace(origin); trimmed != "" {
			origins = append(origins, trimmed)
		}
	}
	return NewLocalSecurity(origins)
}

func NewLocalSecurity(origins []string) (*LocalSecurity, error) {
	allowedOrigins := make(map[string]struct{}, len(origins))
	for _, origin := range origins {
		normalized, err := normalizeOrigin(origin)
		if err != nil {
			return nil, fmt.Errorf("configure allowed origin %q: %w", origin, err)
		}
		allowedOrigins[normalized] = struct{}{}
	}

	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("generate local session token: %w", err)
	}
	return &LocalSecurity{
		allowedOrigins: allowedOrigins,
		sessionToken:   base64.RawURLEncoding.EncodeToString(tokenBytes),
	}, nil
}

func (s *LocalSecurity) Session(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, map[string]string{"token": s.sessionToken})
}

func (s *LocalSecurity) Protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if !isLoopbackHost(request.Host) {
			writeSecurityError(response, http.StatusForbidden, "INVALID_HOST", "请求 Host 不受信任")
			return
		}

		origin := request.Header.Get("Origin")
		if origin != "" {
			if _, allowed := s.allowedOrigins[origin]; !allowed {
				writeSecurityError(response, http.StatusForbidden, "ORIGIN_NOT_ALLOWED", "请求来源未获授权")
				return
			}
			setCORSHeaders(response, origin, request)
		}

		if request.Method == http.MethodOptions {
			if origin == "" {
				writeSecurityError(response, http.StatusBadRequest, "ORIGIN_REQUIRED", "预检请求缺少 Origin")
				return
			}
			response.WriteHeader(http.StatusNoContent)
			return
		}

		if request.Method == http.MethodPost && request.URL.Path == "/api/v1/auth/session" && origin == "" {
			writeSecurityError(response, http.StatusForbidden, "ORIGIN_REQUIRED", "会话请求缺少 Origin")
			return
		}

		if isPublicLocalEndpoint(request) {
			next.ServeHTTP(response, request)
			return
		}
		if !s.hasValidBearerToken(request.Header.Get("Authorization")) {
			response.Header().Set("WWW-Authenticate", "Bearer")
			writeSecurityError(response, http.StatusUnauthorized, "LOCAL_AUTH_REQUIRED", "需要连接本地 Warmmo Core")
			return
		}
		next.ServeHTTP(response, request)
	})
}

func (s *LocalSecurity) hasValidBearerToken(header string) bool {
	scheme, token, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(s.sessionToken)) == 1
}

func normalizeOrigin(origin string) (string, error) {
	parsed, err := url.Parse(origin)
	if err != nil {
		return "", err
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", errors.New("origin must use http or https and include a host")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", errors.New("origin must not include credentials, path, query, or fragment")
	}
	return parsed.Scheme + "://" + parsed.Host, nil
}

func isLoopbackHost(host string) bool {
	return host == "127.0.0.1:8787" || host == "localhost:8787"
}

func isPublicLocalEndpoint(request *http.Request) bool {
	if request.Method == http.MethodPost && request.URL.Path == "/api/v1/auth/session" {
		return true
	}
	return request.Method == http.MethodGet && request.URL.Path == "/api/v1/runtime"
}

func setCORSHeaders(response http.ResponseWriter, origin string, request *http.Request) {
	response.Header().Set("Access-Control-Allow-Origin", origin)
	response.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
	response.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type")
	response.Header().Add("Vary", "Origin")
	response.Header().Add("Vary", "Access-Control-Request-Method")
	response.Header().Add("Vary", "Access-Control-Request-Headers")
	if strings.EqualFold(request.Header.Get("Access-Control-Request-Private-Network"), "true") {
		response.Header().Set("Access-Control-Allow-Private-Network", "true")
	}
}

func writeSecurityError(response http.ResponseWriter, status int, code, message string) {
	writeJSON(response, status, errorResponse{Code: code, Message: message})
}
