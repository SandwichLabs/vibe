// Package awssso signs a person in to AWS with an IAM Identity Center (SSO)
// profile from their ~/.aws/config, in Go, and hands back short-lived role
// credentials. It is a domain leaf: it imports only the AWS SDK and the
// standard library, holds no package-level mutable state, and reads no
// environment variable itself. The caller passes the home directory and a getenv
// (the process snapshot, never a workspace .env overlay).
//
// What it does:
//
//   - LoadProfile reads one profile and accepts only SSO profiles (an
//     sso-session profile or the legacy sso_start_url form). Static keys,
//     credential_process, role_arn/source_profile chains and web identity are
//     refused, and so is a profile that mixes them with SSO. It never calls the
//     SDK's default chain, so no AWS_* variable, AWS_PROFILE, IMDS or endpoint
//     override can change which identity is used or where a token is sent.
//   - Session reads the shared token cache (~/.aws/sso/cache, the AWS CLI's
//     format) without any network.
//   - Credentials turns the cached SSO token into role credentials, refreshing
//     the token with its refresh token when the profile allows it. Results are
//     cached per profile in the Provider (never on disk).
//   - DeviceLogin runs the OIDC device-authorization flow natively and writes the
//     token cache the way the AWS CLI does, so each sees the other's session.
//
// Out of scope: the SDK's HTTP transport still honours HTTPS_PROXY / NO_PROXY,
// and Go's TLS roots honour SSL_CERT_FILE / SSL_CERT_DIR, from the process
// environment. TLS is still verified; whoever controls the process environment
// already controls the process. Only AWS_* endpoint, profile and credential
// variables are guaranteed to have no effect.
//
// No value this package returns or logs ever contains an access token, refresh
// token, client secret or role key: Creds redacts itself under every fmt verb,
// slog and JSON, and every error carries only the profile name.
package awssso
