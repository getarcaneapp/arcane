package ldap

import (
	"cmp"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
	"uuid"

	"github.com/go-ldap/ldap/v3"
	"go.uber.org/fx"
	"gorm.io/gorm"

	"kit.getarcane.app"
	"kit.getarcane.app/normalization"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/environment"
	"getarcane.dev/arcane/v3/internal/role"
	"getarcane.dev/arcane/v3/internal/session"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/internal/user"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/httpx"
	"getarcane.dev/arcane/v3/pkg/validation"
)

type (
	directory interface {
		Search(searchRequest *ldap.SearchRequest) (*ldap.SearchResult, error)
		SearchWithPaging(searchRequest *ldap.SearchRequest, pagingSize uint32) (*ldap.SearchResult, error)
		Bind(username, password string) error
		Close() error
	}

	LdapService struct {
		db              *database.DB
		settingsService *settings.SettingsService
		userService     *user.UserService
		roleService     *role.RoleService
		httpClient      *http.Client
		invalidateUser  environment.UserInvalidator
		connect         func(*settings.Settings) (directory, error)
		// syncMu keeps the directory read and its reconciliation together across scheduled and manual syncs.
		syncMu sync.Mutex
	}

	LdapServiceParams struct {
		fx.In

		DB              *database.DB
		SettingsService *settings.SettingsService
		UserService     *user.UserService
		RoleService     *role.RoleService
		HTTPClient      *http.Client
		InvalidateUser  environment.UserInvalidator
	}

	desiredUser struct {
		ldapID      string
		username    string
		email       *string
		displayName *string
		picture     string
		dn          string
	}

	directorySnapshot struct {
		// present holds the ID of every entry read, including entries skipped as unusable.
		present     map[string]struct{}
		memberships map[string][]string
		users       []desiredUser
	}

	syncedUser struct {
		id       string
		username string
		picture  string
		groups   []string
	}
)

const (
	maxPictureBytes  = 2 << 20
	syncSavepoint    = "ldap_sync"
	pictureTimeout   = 15 * time.Second
	directoryTimeout = 30 * time.Second
	searchPageSize   = 500
)

var (
	ErrLdapDisabled       = common.Classify(common.ErrBadRequest, errors.New("LDAP is not enabled"))
	ErrInvalidCredentials = errors.New("invalid LDAP credentials")
	ErrMissingAttributes  = common.Classify(common.ErrBadRequest, errors.New("LDAP unique identifier and username attributes must be configured"))
	ErrNoDirectoryUsers   = common.Classify(common.ErrConflict, errors.New("LDAP directory returned no usable users while LDAP-managed users exist; refusing to remove them"))

	pictureMimeTypes = []string{"image/png", "image/jpeg", "image/webp"}
)

func NewLdapService(p LdapServiceParams) (*LdapService, error) {
	httpClient, err := httpx.NewSafeOutboundHTTPClient(p.HTTPClient, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to configure LDAP profile picture client: %w", err)
	}
	return &LdapService{
		db:              p.DB,
		settingsService: p.SettingsService,
		userService:     p.UserService,
		roleService:     p.RoleService,
		httpClient:      httpClient,
		invalidateUser:  p.InvalidateUser,
		connect:         connectDirectory,
	}, nil
}

func connectDirectory(cfg *settings.Settings) (directory, error) {
	target, err := url.Parse(cfg.LdapUrl.Value)
	if err != nil {
		return nil, common.Classify(common.ErrBadRequest, fmt.Errorf("invalid LDAP URL: %w", err))
	}
	tlsConfig := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: cfg.LdapSkipCertVerify.IsTrue(),
		ServerName:         target.Hostname(),
	}
	conn, err := ldap.DialURL(cfg.LdapUrl.Value, ldap.DialWithTLSConfig(tlsConfig))
	if err != nil {
		return nil, common.Classify(common.ErrUnavailable, fmt.Errorf("failed to connect to LDAP: %w", err))
	}
	conn.SetTimeout(directoryTimeout)
	if target.Scheme == "ldap" && cfg.LdapStartTls.IsTrue() {
		if tlsErr := conn.StartTLS(tlsConfig); tlsErr != nil {
			_ = conn.Close()
			return nil, common.Classify(common.ErrUnavailable, fmt.Errorf("failed to start TLS with LDAP; use ldaps:// or turn off StartTLS: %w", tlsErr))
		}
	}
	if bindErr := conn.Bind(cfg.LdapBindDn.Value, cfg.LdapBindPassword.Value); bindErr != nil {
		_ = conn.Close()
		return nil, common.Classify(common.ErrUnavailable, fmt.Errorf("failed to bind to LDAP as the service account: %w", bindErr))
	}
	return conn, nil
}

func (s *LdapService) SyncAll(ctx context.Context) error {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	cfg := s.settingsService.GetSettingsConfig()
	if !cfg.LdapEnabled.IsTrue() {
		return ErrLdapDisabled
	}
	if strings.TrimSpace(cfg.LdapAttributeUserUniqueIdentifier.Value) == "" || strings.TrimSpace(cfg.LdapAttributeUserUsername.Value) == "" {
		return ErrMissingAttributes
	}
	conn, err := s.connect(cfg)
	if err != nil {
		return err
	}
	defer func() {
		_ = conn.Close()
	}()

	snapshot, err := fetchDirectory(ctx, conn, cfg)
	if err != nil {
		return err
	}
	var managed int64
	if countErr := s.db.WithContext(ctx).Model(&user.User{}).Where("ldap_id IS NOT NULL").Count(&managed).Error; countErr != nil {
		return fmt.Errorf("failed to count LDAP users: %w", countErr)
	}
	if len(snapshot.users) == 0 && managed > 0 {
		return ErrNoDirectoryUsers
	}
	mappings, err := s.roleService.ListOidcMappings(ctx)
	if err != nil {
		return fmt.Errorf("failed to list role mappings: %w", err)
	}
	roles, err := s.roleService.ListAllRoles(ctx)
	if err != nil {
		return err
	}

	var synced []syncedUser
	var removed []string
	err = s.db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if lockErr := s.roleService.LockGlobalAdminGuard(tx); lockErr != nil {
			return lockErr
		}
		var existing []user.User
		if findErr := tx.Where("ldap_id IS NOT NULL").Find(&existing).Error; findErr != nil {
			return fmt.Errorf("failed to load LDAP users: %w", findErr)
		}
		var stageErr error
		if synced, stageErr = upsertUsers(ctx, tx, snapshot.users, existing, snapshot.memberships); stageErr != nil {
			return stageErr
		}
		if snapshot.memberships != nil {
			if rolesErr := s.assignRoles(ctx, tx, synced, mappings, roles); rolesErr != nil {
				return rolesErr
			}
		}
		removed, stageErr = s.deprovisionUsers(ctx, tx, cfg, snapshot.present, existing)
		return stageErr
	})
	if err != nil {
		return fmt.Errorf("failed to reconcile LDAP users: %w", err)
	}

	for _, u := range synced {
		s.invalidateUser(u.id)
	}
	for _, id := range removed {
		s.invalidateUser(id)
	}
	for _, u := range synced {
		if u.picture == "" {
			continue
		}
		data, mimeType, pictureErr := s.loadPicture(ctx, u.picture)
		if pictureErr == nil {
			pictureErr = s.userService.UploadAvatar(ctx, u.id, data, mimeType)
		}
		if pictureErr != nil {
			slog.WarnContext(ctx, "Failed to save LDAP profile picture", "username", u.username, "error", pictureErr)
		}
	}

	slog.InfoContext(ctx, "LDAP sync completed", "users", len(synced), "removed", len(removed))
	return nil
}

func (s *LdapService) Authenticate(ctx context.Context, ldapID, password string) error {
	if password == "" {
		return ErrInvalidCredentials
	}
	cfg := s.settingsService.GetSettingsConfig()
	conn, err := s.connect(cfg)
	if err != nil {
		return err
	}
	defer func() {
		_ = conn.Close()
	}()

	values := []string{ldap.EscapeFilter(ldapID)}
	parsed, parseErr := uuid.Parse(ldapID)
	decoded, decodeErr := base64.StdEncoding.DecodeString(ldapID)
	switch {
	case parseErr == nil && !isTextID(string(parsed[:])):
		values = append(values, ldap.EscapeFilter(string(parsed[:])))
	case decodeErr == nil && len(decoded) != len(uuid.UUID{}) && !isTextID(string(decoded)):
		values = append(values, ldap.EscapeFilter(string(decoded)))
	}
	var alternatives strings.Builder
	for _, value := range values {
		fmt.Fprintf(&alternatives, "(%s=%s)", cfg.LdapAttributeUserUniqueIdentifier.Value, value)
	}

	result, err := conn.Search(&ldap.SearchRequest{
		BaseDN:     cfg.LdapBase.Value,
		Scope:      ldap.ScopeWholeSubtree,
		Filter:     fmt.Sprintf("(&%s(|%s))", cfg.LdapUserSearchFilter.Value, alternatives.String()),
		Attributes: []string{"dn"},
	})
	if err != nil {
		return common.Classify(common.ErrUnavailable, fmt.Errorf("failed to look up LDAP user: %w", err))
	}
	if len(result.Entries) != 1 {
		slog.DebugContext(ctx, "LDAP login lookup did not match exactly one entry", "matches", len(result.Entries))
		return ErrInvalidCredentials
	}

	bindErr := conn.Bind(result.Entries[0].DN, password)
	switch {
	case ldap.IsErrorWithCode(bindErr, ldap.LDAPResultInvalidCredentials):
		return ErrInvalidCredentials
	case bindErr != nil:
		return common.Classify(common.ErrUnavailable, fmt.Errorf("failed to bind as LDAP user: %w", bindErr))
	default:
		return nil
	}
}

func fetchDirectory(ctx context.Context, conn directory, cfg *settings.Settings) (directorySnapshot, error) {
	idAttr := cfg.LdapAttributeUserUniqueIdentifier.Value
	usernameAttr := cfg.LdapAttributeUserUsername.Value
	result, err := conn.SearchWithPaging(&ldap.SearchRequest{
		BaseDN: cfg.LdapBase.Value,
		Scope:  ldap.ScopeWholeSubtree,
		Filter: cfg.LdapUserSearchFilter.Value,
		Attributes: kit.TrimNonEmpty([]string{
			idAttr,
			usernameAttr,
			cfg.LdapAttributeUserEmail.Value,
			cfg.LdapAttributeUserDisplayName.Value,
			cfg.LdapAttributeUserProfilePicture.Value,
		}),
	}, searchPageSize)
	if err != nil {
		return directorySnapshot{}, common.Classify(common.ErrUnavailable, fmt.Errorf("failed to query LDAP users: %w", err))
	}

	desired := make([]desiredUser, 0, len(result.Entries))
	present := make(map[string]struct{}, len(result.Entries))
	for _, entry := range result.Entries {
		rawID := entry.GetAttributeValue(idAttr)
		if rawID == "" {
			slog.WarnContext(ctx, "Skipping LDAP user without a unique identifier", "dn", entry.DN)
			continue
		}
		ldapID := base64.StdEncoding.EncodeToString([]byte(rawID))
		switch {
		case isTextID(rawID):
			ldapID = rawID
		case len(rawID) == len(uuid.UUID{}):
			ldapID = uuid.UUID([]byte(rawID)).String()
		}
		present[ldapID] = struct{}{}
		username := strings.TrimSpace(entry.GetAttributeValue(usernameAttr))
		if username == "" {
			slog.WarnContext(ctx, "Skipping LDAP user without a username", "dn", entry.DN)
			continue
		}
		email := strings.TrimSpace(entry.GetAttributeValue(cfg.LdapAttributeUserEmail.Value))
		if email != "" && !validation.IsValidUserEmail(email) {
			slog.WarnContext(ctx, "Ignoring invalid LDAP email address", "username", username, "email", email)
		}
		desired = append(desired, desiredUser{
			ldapID:      ldapID,
			username:    username,
			email:       kit.Ternary(validation.IsValidUserEmail(email), &email, nil),
			displayName: new(cmp.Or(strings.TrimSpace(entry.GetAttributeValue(cfg.LdapAttributeUserDisplayName.Value)), username)),
			picture:     entry.GetAttributeValue(cfg.LdapAttributeUserProfilePicture.Value),
			dn:          normalizeDN(entry.DN),
		})
	}

	if strings.TrimSpace(cfg.LdapAttributeGroupName.Value) == "" || strings.TrimSpace(cfg.LdapAttributeGroupMember.Value) == "" {
		return directorySnapshot{users: desired, present: present}, nil
	}
	memberships, err := fetchMemberships(ctx, conn, cfg, desired)
	if err != nil {
		return directorySnapshot{}, err
	}
	return directorySnapshot{users: desired, present: present, memberships: memberships}, nil
}

func fetchMemberships(ctx context.Context, conn directory, cfg *settings.Settings, desired []desiredUser) (map[string][]string, error) {
	nameAttr := cfg.LdapAttributeGroupName.Value
	memberAttr := cfg.LdapAttributeGroupMember.Value
	result, err := conn.SearchWithPaging(&ldap.SearchRequest{
		BaseDN:     cfg.LdapBase.Value,
		Scope:      ldap.ScopeWholeSubtree,
		Filter:     cfg.LdapUserGroupSearchFilter.Value,
		Attributes: []string{nameAttr, memberAttr},
	}, searchPageSize)
	if err != nil {
		return nil, common.Classify(common.ErrUnavailable, fmt.Errorf("failed to query LDAP groups: %w", err))
	}

	synced := make(map[string]struct{}, len(desired))
	dnsByUsername := make(map[string][]string, len(desired))
	for _, d := range desired {
		synced[d.dn] = struct{}{}
		dnsByUsername[d.username] = append(dnsByUsername[d.username], d.dn)
	}

	memberships := make(map[string][]string, len(desired))
	for _, entry := range result.Entries {
		name := strings.TrimSpace(entry.GetAttributeValue(nameAttr))
		if name == "" {
			slog.WarnContext(ctx, "Skipping LDAP group without a name", "dn", entry.DN)
			continue
		}
		for _, member := range entry.GetAttributeValues(memberAttr) {
			matches := dnsByUsername[strings.TrimSpace(member)]
			if _, parseErr := ldap.ParseDN(member); parseErr == nil {
				matches = []string{normalizeDN(member)}
			}
			if len(matches) != 1 {
				slog.DebugContext(ctx, "Skipping LDAP group member that does not match exactly one synced user", "group", name, "member", member)
				continue
			}
			if _, found := synced[matches[0]]; !found || slices.Contains(memberships[matches[0]], name) {
				continue
			}
			memberships[matches[0]] = append(memberships[matches[0]], name)
		}
	}
	return memberships, nil
}

func upsertUsers(ctx context.Context, tx *gorm.DB, desired []desiredUser, existing []user.User, memberships map[string][]string) ([]syncedUser, error) {
	byLdapID := make(map[string]user.User, len(existing))
	for _, u := range existing {
		byLdapID[kit.FromPtr(u.LdapID)] = u
	}

	synced := make([]syncedUser, 0, len(desired))
	for _, d := range desired {
		current := byLdapID[d.ldapID]
		var conflicts []string
		if err := tx.Model(&user.User{}).
			Where("id <> ?", current.ID).
			Where("username = ? OR LOWER(email) = LOWER(?)", d.username, kit.FromPtr(d.email)).
			Limit(1).
			Pluck("id", &conflicts).Error; err != nil {
			return nil, fmt.Errorf("failed to check LDAP user %s for conflicts: %w", d.username, err)
		}
		if len(conflicts) > 0 {
			slog.WarnContext(ctx, "Skipping LDAP user whose username or email is already used by another account", "username", d.username)
			continue
		}

		values := user.User{
			BaseModel: database.BaseModel{
				ID:        cmp.Or(current.ID, uuid.New().String()),
				UpdatedAt: new(time.Now()),
			},
			Username:    d.username,
			Email:       d.email,
			DisplayName: d.displayName,
			LdapID:      new(d.ldapID),
		}
		if err := normalization.Normalize(&values); err != nil {
			return nil, fmt.Errorf("failed to normalize LDAP user %s: %w", d.username, err)
		}
		if current.ID == "" {
			if err := tx.Create(&values).Error; err != nil {
				return nil, fmt.Errorf("failed to create LDAP user %s: %w", d.username, err)
			}
		} else if err := tx.Model(&user.User{}).
			Where("id = ?", values.ID).
			Select("username", "email", "display_name", "disabled", "updated_at").
			Updates(&values).Error; err != nil {
			return nil, fmt.Errorf("failed to update LDAP user %s: %w", d.username, err)
		}
		if current.Disabled {
			slog.InfoContext(ctx, "Re-enabled LDAP user found in the directory again", "username", d.username)
		}
		synced = append(synced, syncedUser{
			id:       values.ID,
			username: values.Username,
			picture:  d.picture,
			groups:   memberships[d.dn],
		})
	}
	return synced, nil
}

func (s *LdapService) deprovisionUsers(ctx context.Context, tx *gorm.DB, cfg *settings.Settings, inDirectory map[string]struct{}, existing []user.User) ([]string, error) {
	softDelete := cfg.LdapSoftDeleteUsers.IsTrue()
	removed := make([]string, 0, len(existing))
	for _, u := range existing {
		if _, found := inDirectory[kit.FromPtr(u.LdapID)]; found || (softDelete && u.Disabled) {
			continue
		}
		if err := tx.SavePoint(syncSavepoint).Error; err != nil {
			return nil, fmt.Errorf("failed to create savepoint: %w", err)
		}
		if softDelete {
			if err := tx.Model(&user.User{}).Where("id = ?", u.ID).Update("disabled", true).Error; err != nil {
				return nil, fmt.Errorf("failed to disable LDAP user %s: %w", u.Username, err)
			}
			if err := session.RevokeAllUserSessionsExceptInDB(ctx, tx, u.ID, ""); err != nil {
				return nil, fmt.Errorf("failed to revoke sessions of LDAP user %s: %w", u.Username, err)
			}
			if err := s.roleService.ReplaceUserAssignmentsInDB(ctx, tx, u.ID, role.RoleAssignmentSourceLdap, nil); err != nil {
				return nil, fmt.Errorf("failed to clear roles of LDAP user %s: %w", u.Username, err)
			}
		} else if err := tx.Delete(&user.User{}, "id = ?", u.ID).Error; err != nil {
			return nil, fmt.Errorf("failed to delete LDAP user %s: %w", u.Username, err)
		}
		applied, err := settle(ctx, tx, u.Username, s.roleService.AssertGlobalAdminExists(ctx, tx))
		if err != nil {
			return nil, fmt.Errorf("failed to remove LDAP user %s: %w", u.Username, err)
		}
		if !applied {
			continue
		}
		slog.InfoContext(ctx, "Removed LDAP user missing from the directory", "username", u.Username, "action", kit.Ternary(softDelete, "disabled", "deleted"))
		removed = append(removed, u.ID)
	}
	return removed, nil
}

func (s *LdapService) assignRoles(ctx context.Context, tx *gorm.DB, synced []syncedUser, mappings []role.OidcRoleMapping, roles []role.Role) error {
	permissions := make(map[string][]string, len(roles))
	for _, r := range roles {
		permissions[r.ID] = r.Permissions
	}
	// Grant new global admins before revoking old ones so a handoff passes the last-admin guard.
	admins := make([]syncedUser, 0, len(synced))
	others := make([]syncedUser, 0, len(synced))
	for _, u := range synced {
		granted := authz.NewPermissionSet()
		for _, a := range role.MappedAssignments(mappings, u.groups) {
			if a.EnvironmentID == nil {
				granted.AddGlobal(permissions[a.RoleID]...)
			}
		}
		if granted.IsGlobalAdmin() {
			admins = append(admins, u)
			continue
		}
		others = append(others, u)
	}

	for _, u := range slices.Concat(admins, others) {
		if err := tx.SavePoint(syncSavepoint).Error; err != nil {
			return fmt.Errorf("failed to create savepoint: %w", err)
		}
		err := s.roleService.ReplaceUserAssignmentsInDB(ctx, tx, u.id, role.RoleAssignmentSourceLdap, role.MappedAssignments(mappings, u.groups))
		if err == nil {
			err = s.roleService.AssertGlobalAdminExists(ctx, tx)
		}
		if _, settleErr := settle(ctx, tx, u.username, err); settleErr != nil {
			return fmt.Errorf("failed to sync roles for LDAP user %s: %w", u.username, settleErr)
		}
	}
	return nil
}

func settle(ctx context.Context, tx *gorm.DB, username string, err error) (bool, error) {
	if !errors.Is(err, common.ErrNoGlobalAdminRemains) && !errors.Is(err, common.ErrInvalidRoleAssignment) {
		return err == nil, err
	}
	if rollbackErr := tx.RollbackTo(syncSavepoint).Error; rollbackErr != nil {
		return false, fmt.Errorf("failed to roll back to savepoint: %w", rollbackErr)
	}
	slog.WarnContext(ctx, "Skipping LDAP change that would leave no global admin or references a missing role", "username", username, "error", err)
	return false, nil
}

func (s *LdapService) loadPicture(ctx context.Context, picture string) ([]byte, string, error) {
	data, decodeErr := base64.StdEncoding.DecodeString(picture)
	if decodeErr != nil {
		data = []byte(picture)
	}
	if parsed, parseErr := url.Parse(picture); parseErr == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		requestCtx, cancel := context.WithTimeout(ctx, pictureTimeout)
		defer cancel()
		req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, picture, http.NoBody)
		if err != nil {
			return nil, "", fmt.Errorf("failed to create profile picture request: %w", err)
		}
		res, err := s.httpClient.Do(req)
		if err != nil {
			return nil, "", fmt.Errorf("failed to download profile picture: %w", err)
		}
		defer func() {
			_ = res.Body.Close()
		}()
		if res.StatusCode != http.StatusOK {
			return nil, "", fmt.Errorf("failed to download profile picture: unexpected status %d", res.StatusCode)
		}
		if data, err = io.ReadAll(io.LimitReader(res.Body, maxPictureBytes+1)); err != nil {
			return nil, "", fmt.Errorf("failed to read profile picture: %w", err)
		}
	}
	if len(data) > maxPictureBytes {
		return nil, "", fmt.Errorf("profile picture must not exceed %d MB", maxPictureBytes>>20)
	}
	mimeType := http.DetectContentType(data)
	if !slices.Contains(pictureMimeTypes, mimeType) {
		return nil, "", fmt.Errorf("unsupported profile picture format %s", mimeType)
	}
	return data, mimeType, nil
}
