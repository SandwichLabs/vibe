package selfupdate

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/prefs"
)

func newService(t *testing.T, f *fakeGH, version string, opts ...Option) (*Service, *prefs.Recents) {
	t.Helper()
	rec := prefs.NewRecents(prefs.NewStore(filepath.Join(t.TempDir(), "cfg")), nil)
	return NewService(f.updater(version, opts...), rec, nil), rec
}

func TestService_AskOnceAndDevNeverAsks(t *testing.T) {
	f := newFakeGH(t)
	svc, _ := newService(t, f, "v0.1.1")
	assert.True(t, svc.State().NeedsConsent, "never asked")
	require.NoError(t, svc.SetCheck("weekly"))
	assert.False(t, svc.State().NeedsConsent)
	require.NoError(t, svc.SetCheck("off"))
	st := svc.State()
	assert.False(t, st.NeedsConsent, "either answer is an answer")
	assert.Equal(t, "off", st.Check)
	assert.Error(t, svc.SetCheck("daily"))

	dev, _ := newService(t, f, "dev")
	assert.False(t, dev.State().NeedsConsent, "dev builds never ask")
	assert.True(t, dev.State().IsDev)
}

func TestService_AutoCheckOnlyWhenWeeklyAndDue(t *testing.T) {
	f := newFakeGH(t)
	ctx := context.Background()
	svc, rec := newService(t, f, "v0.1.1")

	assert.Nil(t, svc.AutoCheck(ctx), "never asked: no request")
	require.NoError(t, svc.SetCheck("off"))
	assert.Nil(t, svc.AutoCheck(ctx), "off: no request")
	assert.Empty(t, f.hits)

	require.NoError(t, svc.SetCheck("weekly"))
	res := svc.AutoCheck(ctx)
	require.NotNil(t, res)
	assert.True(t, res.UpdateAvailable)
	assert.NotEmpty(t, rec.UpdateSettings().LastChecked, "the time is recorded")

	n := len(f.hits)
	assert.Nil(t, svc.AutoCheck(ctx), "checked just now: not due")
	assert.Len(t, f.hits, n)

	svc.now = func() time.Time { return time.Now().Add(8 * 24 * time.Hour) }
	assert.NotNil(t, svc.AutoCheck(ctx), "older than a week: due")
}

func TestService_AutoCheckFailureIsSilent(t *testing.T) {
	f := newFakeGH(t)
	f.status = 500
	svc, rec := newService(t, f, "v0.1.1")
	require.NoError(t, svc.SetCheck("weekly"))
	assert.Nil(t, svc.AutoCheck(context.Background()))
	assert.Empty(t, rec.UpdateSettings().LastChecked, "a failure is not recorded as a check")
}

func TestService_CheckNowIgnoresTheChoiceAndSkipRoundTrips(t *testing.T) {
	f := newFakeGH(t)
	svc, rec := newService(t, f, "v0.1.1")
	require.NoError(t, svc.SetCheck("off"))
	res, err := svc.CheckNow(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "v0.1.2", res.Latest)
	assert.Equal(t, "off", rec.UpdateSettings().Check, "an explicit check does not change the choice")

	require.NoError(t, svc.Skip("v0.1.2"))
	res, _ = svc.CheckNow(context.Background())
	assert.True(t, res.Skipped)
	assert.Error(t, svc.Skip("v1;x"))
	require.NoError(t, svc.Skip(""))
	assert.Empty(t, rec.UpdateSettings().SkippedVersion)
}

func TestService_DevHookActsLikeAReleaseBuild(t *testing.T) {
	f := newFakeGH(t)
	env := map[string]string{"VIBE_UPDATE_BASE_URL": f.srv.URL, "VIBE_UPDATE_PRETEND_VERSION": "v0.1.0"}
	svc, _ := newService(t, f, "dev", WithGetenv(func(k string) string { return env[k] }))
	assert.True(t, svc.State().NeedsConsent)
	res, err := svc.CheckNow(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "v0.1.0", res.Current)
	assert.True(t, res.UpdateAvailable)
	assert.Equal(t, ManagedExternal, res.Managed, "a dev binary is not in any manifest")
}
