package accessauth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

const (
	testAccessIssuer   = "https://test.cloudflareaccess.com"
	testAccessAudience = "reactorlab-test-audience"
	testAdminEmail     = "admin@example.com"
)

type accessTestToken struct {
	issuer    string
	audience  string
	email     string
	expiresAt time.Time
	notBefore time.Time
}

func TestCloudflareAccessValidator(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	otherPrivateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate second RSA key: %v", err)
	}

	validator := NewCloudflareAccessValidatorWithKeySet(
		testAccessIssuer,
		testAccessAudience,
		testAdminEmail,
		&oidc.StaticKeySet{
			PublicKeys: []crypto.PublicKey{
				&privateKey.PublicKey,
			},
		},
	)

	now := time.Now().UTC()

	valid := accessTestToken{
		issuer:    testAccessIssuer,
		audience:  testAccessAudience,
		email:     testAdminEmail,
		expiresAt: now.Add(time.Hour),
		notBefore: now.Add(-time.Minute),
	}

	tests := []struct {
		name       string
		token      string
		claims     accessTestToken
		signingKey *rsa.PrivateKey
		wantError  bool
	}{
		{
			name:       "valid assertion",
			claims:     valid,
			signingKey: privateKey,
		},
		{
			name:      "malformed assertion",
			token:     "not-a-jwt",
			wantError: true,
		},
		{
			name: "expired assertion",
			claims: accessTestToken{
				issuer:    valid.issuer,
				audience:  valid.audience,
				email:     valid.email,
				expiresAt: now.Add(-time.Hour),
				notBefore: valid.notBefore,
			},
			signingKey: privateKey,
			wantError:  true,
		},
		{
			name: "not yet valid assertion",
			claims: accessTestToken{
				issuer:    valid.issuer,
				audience:  valid.audience,
				email:     valid.email,
				expiresAt: valid.expiresAt,
				notBefore: now.Add(10 * time.Minute),
			},
			signingKey: privateKey,
			wantError:  true,
		},
		{
			name: "wrong audience",
			claims: accessTestToken{
				issuer:    valid.issuer,
				audience:  "another-audience",
				email:     valid.email,
				expiresAt: valid.expiresAt,
				notBefore: valid.notBefore,
			},
			signingKey: privateKey,
			wantError:  true,
		},
		{
			name: "wrong issuer",
			claims: accessTestToken{
				issuer:    "https://other.cloudflareaccess.com",
				audience:  valid.audience,
				email:     valid.email,
				expiresAt: valid.expiresAt,
				notBefore: valid.notBefore,
			},
			signingKey: privateKey,
			wantError:  true,
		},
		{
			name: "wrong email",
			claims: accessTestToken{
				issuer:    valid.issuer,
				audience:  valid.audience,
				email:     "someone@example.com",
				expiresAt: valid.expiresAt,
				notBefore: valid.notBefore,
			},
			signingKey: privateKey,
			wantError:  true,
		},
		{
			name:       "wrong signature",
			claims:     valid,
			signingKey: otherPrivateKey,
			wantError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rawToken := tt.token

			if tt.signingKey != nil {
				rawToken = signAccessTestToken(
					t,
					tt.signingKey,
					tt.claims,
				)
			}

			identity, err := validator.Validate(
				context.Background(),
				rawToken,
			)

			if tt.wantError {
				if err == nil {
					t.Fatal("expected validation error")
				}
				return
			}

			if err != nil {
				t.Fatalf("Validate() error: %v", err)
			}

			if identity.Email != testAdminEmail {
				t.Fatalf(
					"email = %q; want %q",
					identity.Email,
					testAdminEmail,
				)
			}
		})
	}
}

func signAccessTestToken(
	t *testing.T,
	privateKey *rsa.PrivateKey,
	claims accessTestToken,
) string {
	t.Helper()

	signer, err := jose.NewSigner(
		jose.SigningKey{
			Algorithm: jose.RS256,
			Key:       privateKey,
		},
		(&jose.SignerOptions{}).WithType("JWT"),
	)
	if err != nil {
		t.Fatalf("create JWT signer: %v", err)
	}

	standardClaims := jwt.Claims{
		Issuer:    claims.issuer,
		Audience:  jwt.Audience{claims.audience},
		Expiry:    jwt.NewNumericDate(claims.expiresAt),
		NotBefore: jwt.NewNumericDate(claims.notBefore),
		IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
	}

	privateClaims := struct {
		Email string `json:"email"`
	}{
		Email: claims.email,
	}

	rawToken, err := jwt.Signed(signer).
		Claims(standardClaims).
		Claims(privateClaims).
		Serialize()
	if err != nil {
		t.Fatalf("sign Access test token: %v", err)
	}

	return rawToken
}

func TestRequireAccessRequiresJWTAssertion(t *testing.T) {
	handler := RequireAccess(
		stubAccessValidator{
			identity: AccessIdentity{
				Email: testAdminEmail,
			},
		},
		http.HandlerFunc(func(
			response http.ResponseWriter,
			request *http.Request,
		) {
			response.WriteHeader(http.StatusNoContent)
		}),
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"https://minibase.reactorlab.dev/admin/",
		nil,
	)

	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf(
			"status = %d; want %d",
			response.Code,
			http.StatusUnauthorized,
		)
	}
}

func TestRequireAccessDoesNotTrustPlainIdentityHeader(
	t *testing.T,
) {
	handler := RequireAccess(
		stubAccessValidator{
			identity: AccessIdentity{
				Email: testAdminEmail,
			},
		},
		http.HandlerFunc(func(
			response http.ResponseWriter,
			request *http.Request,
		) {
			response.WriteHeader(http.StatusNoContent)
		}),
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"https://minibase.reactorlab.dev/admin/",
		nil,
	)

	request.Header.Set(
		"Cf-Access-Authenticated-User-Email",
		testAdminEmail,
	)

	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf(
			"status = %d; want %d",
			response.Code,
			http.StatusUnauthorized,
		)
	}
}

func TestRequireAccessRejectsInvalidAssertion(t *testing.T) {
	handler := RequireAccess(
		stubAccessValidator{
			err: errors.New("invalid assertion"),
		},
		http.HandlerFunc(func(
			response http.ResponseWriter,
			request *http.Request,
		) {
			response.WriteHeader(http.StatusNoContent)
		}),
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"https://minibase.reactorlab.dev/admin/",
		nil,
	)

	request.Header.Set(
		AccessJWTHeader,
		"invalid-token",
	)

	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf(
			"status = %d; want %d",
			response.Code,
			http.StatusForbidden,
		)
	}
}

func TestRequireAccessAllowsValidAssertion(t *testing.T) {
	handler := RequireAccess(
		stubAccessValidator{
			identity: AccessIdentity{
				Email: testAdminEmail,
			},
		},
		http.HandlerFunc(func(
			response http.ResponseWriter,
			request *http.Request,
		) {
			response.WriteHeader(http.StatusNoContent)
		}),
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"https://minibase.reactorlab.dev/admin/",
		nil,
	)

	request.Header.Set(
		AccessJWTHeader,
		"accepted-token",
	)

	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf(
			"status = %d; want %d",
			response.Code,
			http.StatusNoContent,
		)
	}
}

func TestMissingAccessConfigurationFailsClosed(
	t *testing.T,
) {
	_, err := NewCloudflareAccessValidator(
		AccessConfig{},
	)

	if err == nil {
		t.Fatal(
			"expected missing Access configuration to fail",
		)
	}
}

func TestAccessConfigFromEnvironment(t *testing.T) {
	t.Setenv(
		"REACTORLAB_ACCESS_TEAM_DOMAIN",
		"https://example.cloudflareaccess.com",
	)

	t.Setenv(
		"REACTORLAB_ACCESS_AUDIENCE",
		"audience",
	)

	t.Setenv(
		"REACTORLAB_ACCESS_ADMIN_EMAIL",
		"Admin@Example.com",
	)

	config := ConfigFromEnvironment()

	if config.TeamDomain !=
		"https://example.cloudflareaccess.com" ||
		config.Audience != "audience" ||
		config.AdminEmail != "Admin@Example.com" {

		t.Fatalf(
			"unexpected Access configuration: %#v",
			config,
		)
	}
}

func TestAccessValidatorInterfaceCanBeInjected(
	t *testing.T,
) {
	validator := AccessTokenValidator(
		stubAccessValidator{
			identity: AccessIdentity{
				Email: testAdminEmail,
			},
		},
	)

	identity, err := validator.Validate(
		context.Background(),
		"test-token",
	)
	if err != nil {
		t.Fatalf("Validate() error: %v", err)
	}

	if identity.Email != testAdminEmail {
		t.Fatalf(
			"email = %q; want %q",
			identity.Email,
			testAdminEmail,
		)
	}
}

type stubAccessValidator struct {
	identity AccessIdentity
	err      error
}

func (validator stubAccessValidator) Validate(
	context.Context,
	string,
) (AccessIdentity, error) {
	return validator.identity, validator.err
}
