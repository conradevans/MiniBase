package accessauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
)

const AccessJWTHeader = "Cf-Access-Jwt-Assertion"

var ErrAccessDenied = errors.New("access denied")

type AccessIdentity struct {
	Email string
}

type AccessTokenValidator interface {
	Validate(
		ctx context.Context,
		rawToken string,
	) (AccessIdentity, error)
}

type AccessConfig struct {
	TeamDomain string
	Audience   string
	AdminEmail string
}

type CloudflareAccessValidator struct {
	verifier   *oidc.IDTokenVerifier
	adminEmail string
}

type rejectingAccessValidator struct{}

type accessIdentityContextKey struct{}

func RequireAccess(
	validator AccessTokenValidator,
	next http.Handler,
) http.Handler {
	if validator == nil {
		validator = rejectingAccessValidator{}
	}

	return http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		rawToken := strings.TrimSpace(
			r.Header.Get(AccessJWTHeader),
		)
		if rawToken == "" {
			http.Error(
				w,
				"access authentication required",
				http.StatusUnauthorized,
			)
			return
		}

		identity, err := validator.Validate(
			r.Context(),
			rawToken,
		)
		if err != nil {
			http.Error(
				w,
				"access denied",
				http.StatusForbidden,
			)
			return
		}

		next.ServeHTTP(
			w,
			r.WithContext(
				withAccessIdentity(
					r.Context(),
					identity,
				),
			),
		)
	})
}

func ConfigFromEnvironment() AccessConfig {
	return AccessConfig{
		TeamDomain: os.Getenv(
			"REACTORLAB_ACCESS_TEAM_DOMAIN",
		),
		Audience: os.Getenv(
			"REACTORLAB_ACCESS_AUDIENCE",
		),
		AdminEmail: os.Getenv(
			"REACTORLAB_ACCESS_ADMIN_EMAIL",
		),
	}
}

func NewCloudflareAccessValidator(
	config AccessConfig,
) (AccessTokenValidator, error) {
	teamDomain, err := normalizedAccessTeamDomain(
		config.TeamDomain,
	)
	if err != nil {
		return nil, err
	}

	audience := strings.TrimSpace(config.Audience)
	if audience == "" {
		return nil, fmt.Errorf(
			"REACTORLAB_ACCESS_AUDIENCE is required",
		)
	}

	adminEmail := normalizedEmail(config.AdminEmail)
	if adminEmail == "" {
		return nil, fmt.Errorf(
			"REACTORLAB_ACCESS_ADMIN_EMAIL is required",
		)
	}

	keySet := oidc.NewRemoteKeySet(
		context.Background(),
		teamDomain+"/cdn-cgi/access/certs",
	)

	return NewCloudflareAccessValidatorWithKeySet(
		teamDomain,
		audience,
		adminEmail,
		keySet,
	), nil
}

func NewCloudflareAccessValidatorWithKeySet(
	issuer string,
	audience string,
	adminEmail string,
	keySet oidc.KeySet,
) *CloudflareAccessValidator {
	return &CloudflareAccessValidator{
		verifier: oidc.NewVerifier(
			issuer,
			keySet,
			&oidc.Config{
				ClientID: audience,
			},
		),
		adminEmail: normalizedEmail(adminEmail),
	}
}

func (v *CloudflareAccessValidator) Validate(
	ctx context.Context,
	rawToken string,
) (AccessIdentity, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return AccessIdentity{}, ErrAccessDenied
	}

	token, err := v.verifier.Verify(ctx, rawToken)
	if err != nil {
		return AccessIdentity{}, fmt.Errorf(
			"verify Access token: %w",
			err,
		)
	}

	var claims struct {
		Email string `json:"email"`
	}

	if err := token.Claims(&claims); err != nil {
		return AccessIdentity{}, fmt.Errorf(
			"read Access token claims: %w",
			err,
		)
	}

	email := normalizedEmail(claims.Email)
	if email == "" || email != v.adminEmail {
		return AccessIdentity{}, ErrAccessDenied
	}

	return AccessIdentity{
		Email: email,
	}, nil
}

func (rejectingAccessValidator) Validate(
	context.Context,
	string,
) (AccessIdentity, error) {
	return AccessIdentity{}, ErrAccessDenied
}

func normalizedAccessTeamDomain(
	value string,
) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf(
			"REACTORLAB_ACCESS_TEAM_DOMAIN is required",
		)
	}

	parsed, err := url.Parse(value)
	if err != nil ||
		parsed.Scheme != "https" ||
		parsed.Host == "" ||
		parsed.User != nil ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" ||
		(parsed.Path != "" && parsed.Path != "/") {
		return "", fmt.Errorf(
			"REACTORLAB_ACCESS_TEAM_DOMAIN must be an HTTPS origin",
		)
	}

	return "https://" + parsed.Host, nil
}

func normalizedEmail(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func withAccessIdentity(
	ctx context.Context,
	identity AccessIdentity,
) context.Context {
	return context.WithValue(
		ctx,
		accessIdentityContextKey{},
		identity,
	)
}

func IdentityFromContext(
	ctx context.Context,
) (AccessIdentity, bool) {
	identity, ok := ctx.Value(
		accessIdentityContextKey{},
	).(AccessIdentity)

	return identity, ok
}
