package awssso

import (
	"context"
	"errors"
	"net/url"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssooidc"
	oidctypes "github.com/aws/aws-sdk-go-v2/service/ssooidc/types"
	"github.com/aws/smithy-go"
)

const (
	deviceGrant      = "urn:ietf:params:oauth:grant-type:device_code"
	defaultInterval  = 5 * time.Second
	slowDownStep     = 5 * time.Second
	defaultCodeLife  = 10 * time.Minute
	maxPollTransient = 3
	maxInterval      = 60 * time.Second
	maxCodeLife      = 30 * time.Minute
)

// Prompt is what the person is shown: open URL (already host-checked), and
// check that the page shows Code. Neither is a secret.
type Prompt struct {
	URL       string
	Code      string
	ExpiresAt time.Time
}

// registration is one OIDC client registration.
type registration struct {
	id, secret string
	expiresAt  time.Time
}

// DeviceLogin runs the OIDC device-authorization flow for the profile: register
// a client, show the person a URL and code (show is called once), poll until
// they approve, and write the token cache in the AWS CLI's format so the CLI
// and the SDK's refresh both work. It blocks until approval, denial, expiry or
// ctx is done, and never opens a browser. show must not block.
func (p *Provider) DeviceLogin(ctx context.Context, name string, show func(Prompt)) error {
	if show == nil {
		return errors.New("DeviceLogin needs a function to show the sign-in prompt")
	}
	if p.optErr != nil {
		return p.optErr
	}
	prof, err := p.LoadProfile(ctx, name)
	if err != nil {
		return err
	}
	oidc := p.oidcClient(prof.Region)
	reg, err := p.register(ctx, oidc, prof)
	if err != nil {
		return err
	}
	auth, err := p.startAuthorization(ctx, oidc, prof, reg)
	if err != nil {
		return err
	}
	pr, err := p.prompt(prof, auth)
	if err != nil {
		return err
	}
	show(pr)
	tok, err := p.poll(ctx, oidc, prof, reg, auth, pr.ExpiresAt)
	if err != nil {
		return err
	}
	if err := p.storeLogin(prof, reg, tok); err != nil {
		return err
	}
	p.clearCaches(prof.Name)
	return nil
}

func (p *Provider) callCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, p.timeout)
}

// failure maps an OIDC error during the flow (not the poll) to this package's.
func (p *Provider) failure(ctx context.Context, prof Profile, err error) error {
	if ce := ctx.Err(); ce != nil {
		return ce
	}
	var api smithy.APIError
	if errors.As(err, &api) {
		return &ServiceError{Profile: prof.Name, Code: api.ErrorCode()}
	}
	return ErrUnreachable
}

func (p *Provider) register(ctx context.Context, oidc *ssooidc.Client, prof Profile) (registration, error) {
	scopes := prof.Scopes
	if len(scopes) == 0 {
		scopes = []string{defaultScope}
	}
	in := &ssooidc.RegisterClientInput{ClientName: aws.String("slaw"), ClientType: aws.String("public"), Scopes: scopes}
	if prof.Kind == KindSession {
		in.GrantTypes = []string{deviceGrant, "refresh_token"}
		in.IssuerUrl = aws.String(prof.StartURL)
	}
	cctx, cancel := p.callCtx(ctx)
	defer cancel()
	out, err := oidc.RegisterClient(cctx, in)
	if err != nil {
		return registration{}, p.failure(ctx, prof, err)
	}
	if aws.ToString(out.ClientId) == "" || aws.ToString(out.ClientSecret) == "" {
		return registration{}, &ServiceError{Profile: prof.Name, Code: "IncompleteRegistration"}
	}
	reg := registration{id: aws.ToString(out.ClientId), secret: aws.ToString(out.ClientSecret)}
	if out.ClientSecretExpiresAt > 0 {
		reg.expiresAt = time.Unix(out.ClientSecretExpiresAt, 0).UTC()
	}
	return reg, nil
}

func (p *Provider) startAuthorization(ctx context.Context, oidc *ssooidc.Client, prof Profile, reg registration) (*ssooidc.StartDeviceAuthorizationOutput, error) {
	cctx, cancel := p.callCtx(ctx)
	defer cancel()
	out, err := oidc.StartDeviceAuthorization(cctx, &ssooidc.StartDeviceAuthorizationInput{
		ClientId: aws.String(reg.id), ClientSecret: aws.String(reg.secret), StartUrl: aws.String(prof.StartURL)})
	if err != nil {
		return nil, p.failure(ctx, prof, err)
	}
	return out, nil
}

// prompt builds the Prompt from the device authorization. Every URL AWS sent
// must pass the host check; one that does not is an error, not a prompt.
func (p *Provider) prompt(prof Profile, auth *ssooidc.StartDeviceAuthorizationOutput) (Prompt, error) {
	testHost := ""
	if p.oidcEndpoint != "" {
		if u, err := url.Parse(p.oidcEndpoint); err == nil {
			testHost = u.Host
		}
	}
	code := aws.ToString(auth.UserCode)
	if code == "" || aws.ToString(auth.DeviceCode) == "" {
		return Prompt{}, errBadVerificationURL
	}
	var chosen string
	for _, raw := range []string{aws.ToString(auth.VerificationUriComplete), aws.ToString(auth.VerificationUri)} {
		if raw == "" {
			continue
		}
		if err := checkVerificationURL(raw, prof, testHost); err != nil {
			return Prompt{}, err
		}
		if chosen == "" {
			chosen = raw
		}
	}
	if chosen == "" {
		return Prompt{}, errBadVerificationURL
	}
	life := time.Duration(auth.ExpiresIn) * time.Second
	if life <= 0 {
		life = defaultCodeLife
	}
	life = min(life, maxCodeLife)
	return Prompt{URL: chosen, Code: code, ExpiresAt: p.now().Add(life)}, nil
}

// poll waits for the person to approve. It honours the server's interval,
// slows down by 5 s on slow_down, and stops at the code's expiry.
func (p *Provider) poll(ctx context.Context, oidc *ssooidc.Client, prof Profile, reg registration,
	auth *ssooidc.StartDeviceAuthorizationOutput, deadline time.Time) (*ssooidc.CreateTokenOutput, error) {
	interval := time.Duration(auth.Interval) * time.Second
	if interval <= 0 {
		interval = defaultInterval
	}
	interval = min(interval, maxInterval)
	transient := 0
	for {
		wait := min(interval, deadline.Sub(p.now()))
		if wait <= 0 {
			return nil, ErrDeviceCodeExpired
		}
		if err := p.sleep(ctx, wait); err != nil {
			return nil, err
		}
		if !p.now().Before(deadline) {
			return nil, ErrDeviceCodeExpired
		}
		cctx, cancel := p.callCtx(ctx)
		out, err := oidc.CreateToken(cctx, &ssooidc.CreateTokenInput{ClientId: aws.String(reg.id), ClientSecret: aws.String(reg.secret),
			GrantType: aws.String(deviceGrant), DeviceCode: auth.DeviceCode})
		cancel()
		if err == nil {
			return out, nil
		}
		next, terr := p.pollOutcome(ctx, prof, err, &transient)
		if terr != nil {
			return nil, terr
		}
		interval = min(interval+next, maxInterval)
	}
}

// pollOutcome says how much to add to the interval and whether to stop.
func (p *Provider) pollOutcome(ctx context.Context, prof Profile, err error, transient *int) (time.Duration, error) {
	if ce := ctx.Err(); ce != nil {
		return 0, ce
	}
	var pending *oidctypes.AuthorizationPendingException
	var slow *oidctypes.SlowDownException
	var expired *oidctypes.ExpiredTokenException
	var denied *oidctypes.AccessDeniedException
	switch {
	case errors.As(err, &pending):
		*transient = 0
		return 0, nil
	case errors.As(err, &slow):
		*transient = 0
		return slowDownStep, nil
	case errors.As(err, &expired):
		return 0, ErrDeviceCodeExpired
	case errors.As(err, &denied):
		return 0, ErrAccessDenied
	}
	var api smithy.APIError
	if errors.As(err, &api) {
		return 0, &ServiceError{Profile: prof.Name, Code: api.ErrorCode()}
	}
	if *transient++; *transient >= maxPollTransient {
		return 0, ErrUnreachable
	}
	return 0, nil
}

// storeLogin writes the token cache. An sso-session profile also gets the
// registration and refresh token (the SDK refreshes only with all three);
// a legacy profile gets the access token alone.
func (p *Provider) storeLogin(prof Profile, reg registration, tok *ssooidc.CreateTokenOutput) error {
	access := aws.ToString(tok.AccessToken)
	if access == "" {
		return &ServiceError{Profile: prof.Name, Code: "NoAccessToken"}
	}
	life := time.Duration(tok.ExpiresIn) * time.Second
	if life <= 0 {
		return &ServiceError{Profile: prof.Name, Code: "NoTokenExpiry"}
	}
	t := tokenFile{StartURL: prof.StartURL, Region: prof.Region, AccessToken: access,
		ExpiresAt: p.now().Add(life).UTC().Format(time.RFC3339)}
	if prof.Kind == KindSession {
		t.ClientID, t.ClientSecret = reg.id, reg.secret
		t.RefreshToken = aws.ToString(tok.RefreshToken)
		if !reg.expiresAt.IsZero() {
			t.RegistrationExpiresAt = reg.expiresAt.Format(time.RFC3339)
		}
	}
	return p.writeToken(tokenPath(p.home, prof.cacheKey()), t)
}
