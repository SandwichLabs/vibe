package secrets

import "context"

// The three functions below are what #117 binds for a connection's
// credentials. They set, clear and describe; nothing here returns a secret.

// SetConnectionCredentials saves cred for ref under the person's sign-in
// method, bound to the connection's fingerprint. Empty secret fields keep the
// stored value when the binding is unchanged (write-only editing).
func (r *Resolver) SetConnectionCredentials(ctx context.Context, ref Ref, method string, cred Credential, b Binding) error {
	return r.SetCredentials(ctx, ref, method, cred, b, false)
}

// ClearConnectionCredentials removes the saved credential (keychain and
// in-memory). The environment is never touched.
func (r *Resolver) ClearConnectionCredentials(ctx context.Context, ref Ref) error {
	return r.ClearCredentials(ctx, ref)
}

// GetConnectionCredentialStatus describes the credential for the UI:
// {method, username/keyId, secretSet, fingerprintMatches, available, detail,
// env}. It never carries a secret.
func (r *Resolver) GetConnectionCredentialStatus(ctx context.Context, ref Ref, method string, b Binding) CredentialStatus {
	return r.Status(ctx, ref, method, b)
}
