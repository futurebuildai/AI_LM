package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/futurebuildai/ai-lm/pkg/httputil"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
)

type AuthConfig struct {
	JWKSURL     string
	Issuer      string
	PublicPaths []string
}

type AuthMiddleware struct {
	jwks        keyfunc.Keyfunc
	issuer      string
	publicPaths []string
	logger      *slog.Logger
}

// UserClaims holds standard OIDC claims plus the role fields AI_LM authorizes on.
// Tokens are issued by the same OIDC provider as GableLBM (shared JWKS).
type UserClaims struct {
	jwt.RegisteredClaims
	Email string   `json:"email,omitempty"`
	Roles []string `json:"roles,omitempty"`
	Role  string   `json:"role,omitempty"` // single-role field (Brain-issued tokens)
	OrgID string   `json:"org_id,omitempty"`
}

// Key for Context
type contextKey string

const UserContextKey contextKey = "user"

// NewAuthMiddleware initializes the JWKS fetcher and returns the middleware.
func NewAuthMiddleware(ctx context.Context, cfg AuthConfig, logger *slog.Logger) (*AuthMiddleware, error) {
	// Create the JWKS from the URL. This fetches keys immediately, caches them,
	// and refreshes automatically based on Cache-Control headers.
	k, err := keyfunc.NewDefault([]string{cfg.JWKSURL})
	if err != nil {
		return nil, fmt.Errorf("failed to create JWKS from URL %s: %w", cfg.JWKSURL, err)
	}

	return &AuthMiddleware{
		jwks:        k,
		issuer:      cfg.Issuer,
		publicPaths: cfg.PublicPaths,
		logger:      logger,
	}, nil
}

// Handler is the actual middleware function.
func (m *AuthMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 0. Check Public Paths.
		// Paths ending with "/" are treated as prefixes; others require exact match.
		for _, path := range m.publicPaths {
			if strings.HasSuffix(path, "/") {
				if strings.HasPrefix(r.URL.Path, path) {
					next.ServeHTTP(w, r)
					return
				}
			} else if r.URL.Path == path {
				next.ServeHTTP(w, r)
				return
			}
		}

		// 1. Extract Token
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			m.logger.Warn("Missing Authorization header", "path", r.URL.Path)
			httputil.RespondError(w, r, "Unauthorized: No token provided", http.StatusUnauthorized, nil)
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			m.logger.Warn("Invalid Authorization header format", "path", r.URL.Path)
			httputil.RespondError(w, r, "Unauthorized: Invalid token format", http.StatusUnauthorized, nil)
			return
		}
		tokenString := parts[1]

		// 2. Parse and Validate Token
		token, err := jwt.ParseWithClaims(tokenString, &UserClaims{}, m.jwks.Keyfunc)
		if err != nil {
			m.logger.Warn("Token validation failed", "error", err, "path", r.URL.Path)
			httputil.RespondError(w, r, "Unauthorized: Invalid token", http.StatusUnauthorized, nil)
			return
		}

		// 3. Verify Claims (Issuer)
		if !token.Valid {
			m.logger.Warn("Token is invalid", "path", r.URL.Path)
			httputil.RespondError(w, r, "Unauthorized: Invalid token", http.StatusUnauthorized, nil)
			return
		}

		claims, ok := token.Claims.(*UserClaims)
		if !ok {
			m.logger.Error("Failed to cast claims", "path", r.URL.Path)
			httputil.RespondError(w, r, "Internal Server Error", http.StatusInternalServerError, nil)
			return
		}

		// Optional: verify issuer strictly if configured.
		if m.issuer != "" && claims.Issuer != m.issuer {
			m.logger.Warn("Token issuer mismatch", "expected", m.issuer, "got", claims.Issuer)
			httputil.RespondError(w, r, "Unauthorized: Invalid issuer", http.StatusUnauthorized, nil)
			return
		}

		// 4. Inject into Context
		ctx := context.WithValue(r.Context(), UserContextKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireRole returns middleware that restricts access to users with one of the
// allowed roles. In dev mode (no auth configured, claims == nil), requests pass through.
func RequireRole(allowedRoles ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]bool, len(allowedRoles))
	for _, r := range allowedRoles {
		allowed[r] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := ClaimsFromContext(r.Context())
			if claims == nil {
				// Dev mode: no auth configured, pass through.
				next.ServeHTTP(w, r)
				return
			}

			// Check single-role field.
			if claims.Role != "" && allowed[claims.Role] {
				next.ServeHTTP(w, r)
				return
			}

			// Check OIDC roles (array field).
			for _, role := range claims.Roles {
				if allowed[role] {
					next.ServeHTTP(w, r)
					return
				}
			}

			httputil.RespondError(w, r, "Forbidden: insufficient role", http.StatusForbidden, nil)
		})
	}
}

// ClaimsFromContext retrieves UserClaims from the request context.
// Returns nil if no claims are present (unauthenticated or dev mode).
func ClaimsFromContext(ctx context.Context) *UserClaims {
	claims, _ := ctx.Value(UserContextKey).(*UserClaims)
	return claims
}
