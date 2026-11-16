package ldap

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/go-ldap/ldap/v3"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/role"
	"getarcane.dev/arcane/v3/internal/session"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/internal/user"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/httpx"
	"getarcane.dev/types/v3"
)

type (
	fakeDirectory struct {
		users    []*ldap.Entry
		groups   []*ldap.Entry
		password string
		filters  []string
		paged    []string
		binds    []string
	}

	testEnv struct {
		service      *LdapService
		db           *database.DB
		roles        *role.RoleService
		invalidated  []string
		connectCalls int
	}
)

func (f *fakeDirectory) Search(req *ldap.SearchRequest) (*ldap.SearchResult, error) {
	f.filters = append(f.filters, req.Filter)
	switch {
	case req.Filter == "(objectClass=person)":
		return &ldap.SearchResult{Entries: f.users}, nil
	case req.Filter == "(objectClass=groupOfNames)":
		return &ldap.SearchResult{Entries: f.groups}, nil
	case strings.HasPrefix(req.Filter, "(&(objectClass=person)"):
		return &ldap.SearchResult{Entries: f.matchingUsers(req.Filter)}, nil
	default:
		return &ldap.SearchResult{}, nil
	}
}

func (f *fakeDirectory) SearchWithPaging(req *ldap.SearchRequest, pagingSize uint32) (*ldap.SearchResult, error) {
	f.paged = append(f.paged, req.Filter)
	if pagingSize == 0 {
		return nil, errors.New("paging size must be set")
	}
	return f.Search(req)
}

func (f *fakeDirectory) matchingUsers(filter string) []*ldap.Entry {
	var matches []*ldap.Entry
	for _, candidate := range f.users {
		if strings.Contains(filter, "(entryUUID="+ldap.EscapeFilter(candidate.GetAttributeValue("entryUUID"))+")") {
			matches = append(matches, candidate)
		}
	}
	return matches
}

func (f *fakeDirectory) Bind(username, password string) error {
	f.binds = append(f.binds, username)
	if password != f.password {
		return ldap.NewError(ldap.LDAPResultInvalidCredentials, errors.New("invalid credentials"))
	}
	return nil
}

func (f *fakeDirectory) Close() error {
	return nil
}

func newTestEnv(t *testing.T, fake *fakeDirectory, overrides map[string]string) *testEnv {
	t.Helper()
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := gormDB.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, gormDB.AutoMigrate(
		&settings.SettingVariable{},
		&user.User{},
		&user.UserAvatar{},
		&session.UserSession{},
		&role.Role{},
		&role.UserRoleAssignment{},
		&role.OidcRoleMapping{},
	))
	require.NoError(t, gormDB.Exec("CREATE UNIQUE INDEX idx_users_username_unique ON users(username)").Error)
	require.NoError(t, gormDB.Exec("CREATE TABLE IF NOT EXISTS environments (id text PRIMARY KEY)").Error)
	db := &database.DB{DB: gormDB}

	roles := role.NewRoleService(db)
	require.NoError(t, roles.EnsureBuiltInRoles(t.Context()))
	settingsEnv, err := settings.LoadEnv()
	require.NoError(t, err)
	settingsSvc, err := settings.NewSettingsService(settings.SettingsServiceParams{
		Ctx:    t.Context(),
		DB:     db,
		Config: &config.Config{},
		Env:    settingsEnv,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, settingsSvc.Stop(context.WithoutCancel(t.Context())))
	})

	values := map[string]string{
		"ldapEnabled":                       "true",
		"ldapBase":                          "dc=example,dc=com",
		"ldapAttributeUserUniqueIdentifier": "entryUUID",
		"ldapAttributeUserUsername":         "uid",
		"ldapAttributeUserEmail":            "mail",
		"ldapAttributeUserDisplayName":      "displayName",
		"ldapAttributeGroupName":            "cn",
	}
	for key, value := range overrides {
		values[key] = value
	}
	updates := make([]types.SettingUpdate, 0, len(values))
	for key, value := range values {
		updates = append(updates, types.SettingUpdate{Key: key, Value: value})
	}
	require.NoError(t, settingsSvc.UpdateSettingValues(t.Context(), updates))

	httpClient, err := httpx.NewSafeOutboundHTTPClient(nil, nil)
	require.NoError(t, err)
	env := &testEnv{
		db:    db,
		roles: roles,
	}
	env.service = &LdapService{
		db:              db,
		settingsService: settingsSvc,
		userService: user.NewUserService(user.UserServiceParams{
			DB:                   db,
			RoleService:          roles,
			SettingsService:      settingsSvc,
			RevokeSessionsExcept: session.RevokeAllUserSessionsExceptInDB,
		}),
		roleService:    roles,
		httpClient:     httpClient,
		invalidateUser: env.recordInvalidation,
		connect:        env.connectFake(fake),
	}
	return env
}

func (e *testEnv) recordInvalidation(userID string) {
	e.invalidated = append(e.invalidated, userID)
}

func (e *testEnv) connectFake(fake *fakeDirectory) func(*settings.Settings) (directory, error) {
	return func(*settings.Settings) (directory, error) {
		e.connectCalls++
		return fake, nil
	}
}

func entry(dn string, attrs map[string][]string) *ldap.Entry {
	result := &ldap.Entry{DN: dn}
	for name, values := range attrs {
		result.Attributes = append(result.Attributes, &ldap.EntryAttribute{
			Name:   name,
			Values: values,
		})
	}
	return result
}

func (e *testEnv) createUser(t *testing.T, u user.User) {
	t.Helper()
	require.NoError(t, e.db.Create(&u).Error)
}

func (e *testEnv) findUser(t *testing.T, where string, args ...any) user.User {
	t.Helper()
	var u user.User
	require.NoError(t, e.db.Where(where, args...).First(&u).Error)
	return u
}

func (e *testEnv) assignments(t *testing.T, userID string) []string {
	t.Helper()
	rows, err := e.roles.ListUserAssignments(t.Context(), userID)
	require.NoError(t, err)
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Source+":"+row.RoleID)
	}
	return out
}

func TestSyncAllReconcilesUsersAndRoles(t *testing.T) {
	carolID := uuid.MustParse("12345678-9abc-def0-1234-56789abcdef1")
	fake := &fakeDirectory{
		users: []*ldap.Entry{
			entry("uid=alice,ou=people,dc=example,dc=com", map[string][]string{
				"entryUUID":   {"u-alice"},
				"uid":         {"alice"},
				"mail":        {"alice@example.com"},
				"displayName": {"Alice Jones"},
			}),
			entry("uid=bob,ou=people,dc=example,dc=com", map[string][]string{
				"entryUUID": {"u-bob"},
				"uid":       {"bob"},
				"mail":      {"bob@example.com"},
			}),
			entry("uid=carol,ou=people,dc=example,dc=com", map[string][]string{
				"entryUUID": {string(carolID[:])},
				"uid":       {"carol"},
			}),
			entry("uid=dave,ou=people,dc=example,dc=com", map[string][]string{
				"entryUUID": {"u-dave"},
				"uid":       {"dave-ldap"},
				"mail":      {"dave@example.com"},
			}),
			entry("uid=erin,ou=people,dc=example,dc=com", map[string][]string{
				"entryUUID": {"u-\x01erin"},
				"uid":       {"erin"},
			}),
			entry("uid=frank,ou=people,dc=example,dc=com", map[string][]string{
				"entryUUID": {" u-frank "},
				"uid":       {"frank"},
			}),
		},
		groups: []*ldap.Entry{
			entry("cn=admins,ou=groups,dc=example,dc=com", map[string][]string{
				"cn":     {"admins"},
				"member": {"UID=Alice, OU=People, DC=example, DC=com", "uid=bob,ou=elsewhere,dc=example,dc=com", "uid=carol"},
			}),
			entry("cn=devs,ou=groups,dc=example,dc=com", map[string][]string{
				"cn":     {"devs"},
				"member": {"bob", "uid=carol,ou=people,dc=example,dc=com", "uid=dave,ou=people,dc=example,dc=com"},
			}),
		},
	}
	env := newTestEnv(t, fake, nil)
	ctx := t.Context()

	env.createUser(t, user.User{
		BaseModel: database.BaseModel{ID: "root"},
		Username:  "root",
	})
	require.NoError(t, env.roles.ReplaceUserAssignments(ctx, "root", role.RoleAssignmentSourceManual, []role.UserRoleAssignment{{RoleID: authz.BuiltInRoleAdmin}}))
	env.createUser(t, user.User{
		BaseModel: database.BaseModel{ID: "alice-id"},
		Username:  "alice-old",
		LdapID:    new("u-alice"),
		Disabled:  true,
	})
	env.createUser(t, user.User{
		BaseModel: database.BaseModel{ID: "missing-id"},
		Username:  "missing",
		LdapID:    new("u-missing"),
	})
	env.createUser(t, user.User{
		BaseModel: database.BaseModel{ID: "dave-local"},
		Username:  "dave",
		Email:     new("dave@example.com"),
	})
	require.NoError(t, env.db.Create(&session.UserSession{
		UserID:           "missing-id",
		ExpiresAt:        time.Now().Add(time.Hour),
		LastUsedAt:       time.Now(),
		RefreshTokenHash: "missing-refresh",
	}).Error)
	_, err := env.roles.CreateOidcMapping(ctx, "admins", authz.BuiltInRoleAdmin, nil)
	require.NoError(t, err)
	_, err = env.roles.CreateOidcMapping(ctx, "devs", authz.BuiltInRoleViewer, nil)
	require.NoError(t, err)

	require.NoError(t, env.service.SyncAll(ctx))

	alice := env.findUser(t, "ldap_id = ?", "u-alice")
	assert.Equal(t, "alice-id", alice.ID)
	assert.Equal(t, "alice", alice.Username)
	assert.Equal(t, "alice@example.com", *alice.Email)
	assert.Equal(t, "Alice Jones", *alice.DisplayName)
	assert.False(t, alice.Disabled)
	assert.Equal(t, []string{"ldap:" + authz.BuiltInRoleAdmin}, env.assignments(t, alice.ID))

	bob := env.findUser(t, "ldap_id = ?", "u-bob")
	assert.Equal(t, "bob", *bob.DisplayName)
	assert.Equal(t, []string{"ldap:" + authz.BuiltInRoleViewer}, env.assignments(t, bob.ID))

	carol := env.findUser(t, "ldap_id = ?", carolID.String())
	assert.Equal(t, "carol", carol.Username)
	assert.Nil(t, carol.Email)
	assert.Equal(t, []string{"ldap:" + authz.BuiltInRoleViewer}, env.assignments(t, carol.ID))

	assert.Equal(t, "erin", env.findUser(t, "ldap_id = ?", base64.StdEncoding.EncodeToString([]byte("u-\x01erin"))).Username)
	assert.Equal(t, "frank", env.findUser(t, "ldap_id = ?", " u-frank ").Username)
	assert.Contains(t, fake.paged, "(objectClass=person)")
	assert.Contains(t, fake.paged, "(objectClass=groupOfNames)")

	var daveLdap int64
	require.NoError(t, env.db.Model(&user.User{}).Where("ldap_id = ?", "u-dave").Count(&daveLdap).Error)
	assert.Zero(t, daveLdap)
	assert.Nil(t, env.findUser(t, "id = ?", "dave-local").LdapID)

	assert.True(t, env.findUser(t, "id = ?", "missing-id").Disabled)
	var missingSession session.UserSession
	require.NoError(t, env.db.Where("user_id = ?", "missing-id").First(&missingSession).Error)
	assert.NotNil(t, missingSession.RevokedAt)

	assert.Equal(t, []string{"manual:" + authz.BuiltInRoleAdmin}, env.assignments(t, "root"))
	assert.Contains(t, env.invalidated, "missing-id")
	assert.Contains(t, env.invalidated, alice.ID)

	require.NoError(t, env.service.SyncAll(ctx))
	assert.Equal(t, alice.ID, env.findUser(t, "ldap_id = ?", "u-alice").ID)
}

func TestSyncAllHardDeleteKeepsLastGlobalAdmin(t *testing.T) {
	fake := &fakeDirectory{
		users: []*ldap.Entry{
			entry("uid=keeper,ou=people,dc=example,dc=com", map[string][]string{
				"entryUUID": {"u-keeper"},
				"uid":       {"keeper"},
			}),
		},
	}
	env := newTestEnv(t, fake, map[string]string{"ldapSoftDeleteUsers": "false"})
	ctx := t.Context()

	env.createUser(t, user.User{
		BaseModel: database.BaseModel{ID: "boss-id"},
		Username:  "boss",
		LdapID:    new("u-boss"),
	})
	require.NoError(t, env.roles.ReplaceUserAssignments(ctx, "boss-id", role.RoleAssignmentSourceLdap, []role.UserRoleAssignment{{RoleID: authz.BuiltInRoleAdmin}}))
	env.createUser(t, user.User{
		BaseModel: database.BaseModel{ID: "gone-id"},
		Username:  "gone",
		LdapID:    new("u-gone"),
	})

	require.NoError(t, env.service.SyncAll(ctx))

	var remaining []string
	require.NoError(t, env.db.Model(&user.User{}).Where("ldap_id IN ?", []string{"u-boss", "u-gone"}).Order("id").Pluck("id", &remaining).Error)
	assert.Equal(t, []string{"boss-id"}, remaining)
	assert.Equal(t, []string{"ldap:" + authz.BuiltInRoleAdmin}, env.assignments(t, "boss-id"))
}

func TestSyncAllRequiresLdapEnabled(t *testing.T) {
	env := newTestEnv(t, &fakeDirectory{}, map[string]string{"ldapEnabled": "false"})

	require.ErrorIs(t, env.service.SyncAll(t.Context()), ErrLdapDisabled)
	assert.Zero(t, env.connectCalls)
}

func TestSyncAllRequiresIdentifierAttributes(t *testing.T) {
	env := newTestEnv(t, &fakeDirectory{}, map[string]string{"ldapAttributeUserUsername": ""})

	require.ErrorIs(t, env.service.SyncAll(t.Context()), ErrMissingAttributes)
	assert.Zero(t, env.connectCalls)
}

func TestSyncAllRefusesEmptyDirectoryWithManagedUsers(t *testing.T) {
	env := newTestEnv(t, &fakeDirectory{}, nil)
	env.createUser(t, user.User{
		BaseModel: database.BaseModel{ID: "kept-id"},
		Username:  "kept",
		LdapID:    new("u-kept"),
	})

	err := env.service.SyncAll(t.Context())
	require.ErrorIs(t, err, ErrNoDirectoryUsers)
	require.ErrorIs(t, err, common.ErrConflict)
	assert.False(t, env.findUser(t, "id = ?", "kept-id").Disabled)
}

func TestSyncAllWithoutGroupNameAttributeKeepsRoles(t *testing.T) {
	fake := &fakeDirectory{
		users: []*ldap.Entry{
			entry("uid=alice,ou=people,dc=example,dc=com", map[string][]string{
				"entryUUID": {"u-alice"},
				"uid":       {"alice"},
			}),
		},
	}
	env := newTestEnv(t, fake, map[string]string{"ldapAttributeGroupName": ""})
	ctx := t.Context()
	env.createUser(t, user.User{
		BaseModel: database.BaseModel{ID: "alice-id"},
		Username:  "alice",
		LdapID:    new("u-alice"),
	})
	require.NoError(t, env.roles.ReplaceUserAssignments(ctx, "alice-id", role.RoleAssignmentSourceLdap, []role.UserRoleAssignment{{RoleID: authz.BuiltInRoleAdmin}}))

	require.NoError(t, env.service.SyncAll(ctx))

	assert.Equal(t, []string{"ldap:" + authz.BuiltInRoleAdmin}, env.assignments(t, "alice-id"))
	assert.NotContains(t, fake.paged, "(objectClass=groupOfNames)")
}

func TestLoadPicture(t *testing.T) {
	env := newTestEnv(t, &fakeDirectory{}, nil)
	jpeg := []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00}
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

	tests := []struct {
		wantErr  error
		name     string
		picture  string
		wantMime string
	}{
		{name: "base64 jpeg", picture: base64.StdEncoding.EncodeToString(jpeg), wantMime: "image/jpeg"},
		{name: "raw png bytes", picture: string(png), wantMime: "image/png"},
		{name: "http url is downloaded through the safe client", picture: "http://127.0.0.1:1/avatar.png", wantErr: common.ErrUnsafeRemoteURL},
		{name: "https url is downloaded through the safe client", picture: "https://127.0.0.1:1/avatar.png", wantErr: common.ErrUnsafeRemoteURL},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, mimeType, err := env.service.loadPicture(t.Context(), tc.picture)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantMime, mimeType)
		})
	}

	_, _, err := env.service.loadPicture(t.Context(), "ftp://127.0.0.1/avatar.png")
	require.ErrorContains(t, err, "unsupported profile picture format")
}

func TestAuthenticateBindsAsDirectoryUser(t *testing.T) {
	binaryID := uuid.MustParse("12345678-9abc-def0-1234-56789abcdef1")
	fake := &fakeDirectory{
		password: "s3cret",
		users: []*ldap.Entry{
			entry("uid=alice,ou=people,dc=example,dc=com", map[string][]string{"entryUUID": {"u-(alice)*"}}),
			entry("uid=carol,ou=people,dc=example,dc=com", map[string][]string{"entryUUID": {string(binaryID[:])}}),
			entry("uid=erin,ou=people,dc=example,dc=com", map[string][]string{"entryUUID": {"u-\x01erin"}}),
			entry("uid=gus,ou=people,dc=example,dc=com", map[string][]string{"entryUUID": {"\xff\xfe\x00gus"}}),
		},
	}
	env := newTestEnv(t, fake, nil)

	tests := []struct {
		wantErr  error
		name     string
		ldapID   string
		password string
		wantBind string
	}{
		{name: "valid password", ldapID: "u-(alice)*", password: "s3cret", wantBind: "uid=alice,ou=people,dc=example,dc=com"},
		{name: "binary identifier", ldapID: binaryID.String(), password: "s3cret", wantBind: "uid=carol,ou=people,dc=example,dc=com"},
		{name: "control character identifier", ldapID: base64.StdEncoding.EncodeToString([]byte("u-\x01erin")), password: "s3cret", wantBind: "uid=erin,ou=people,dc=example,dc=com"},
		{name: "non-utf8 identifier", ldapID: base64.StdEncoding.EncodeToString([]byte("\xff\xfe\x00gus")), password: "s3cret", wantBind: "uid=gus,ou=people,dc=example,dc=com"},
		{name: "wrong password", ldapID: "u-(alice)*", password: "nope", wantErr: ErrInvalidCredentials, wantBind: "uid=alice,ou=people,dc=example,dc=com"},
		{name: "unknown user", ldapID: "u-nobody", password: "s3cret", wantErr: ErrInvalidCredentials},
		{name: "empty password", ldapID: "u-(alice)*", wantErr: ErrInvalidCredentials},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake.binds = nil
			err := env.service.Authenticate(t.Context(), tc.ldapID, tc.password)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
			if tc.wantBind == "" {
				assert.Empty(t, fake.binds)
				return
			}
			assert.Equal(t, []string{tc.wantBind}, fake.binds)
		})
	}
	assert.Contains(t, fake.filters, `(&(objectClass=person)(|(entryUUID=u-\28alice\29\2a)))`)
}
