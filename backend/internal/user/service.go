package user

import (
	"cmp"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"go.getarcane.app/kit/normalization"
	"go.getarcane.app/kit/pkg"
	"golang.org/x/crypto/argon2"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/getarcaneapp/arcane/backend/v3/internal/audit"
	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/role"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/dbutil"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/userctx"
)

type Argon2Params struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
	saltLength  uint32
	keyLength   uint32
}

func DefaultArgon2Params() *Argon2Params {
	return &Argon2Params{
		memory:      64 * 1024,
		iterations:  3,
		parallelism: 2,
		saltLength:  16,
		keyLength:   32,
	}
}

type UserService struct {
	db                   *database.DB
	roleService          *role.RoleService
	argon2Params         *Argon2Params
	argon2Slots          chan struct{}
	revokeSessionsExcept func(context.Context, *gorm.DB, string, string) error
}

var (
	ErrCannotRemoveLastAdmin = errors.New("cannot remove the last admin user")
	ErrUsernameRequired      = errors.New("username is required")
	ErrUsernameTaken         = errors.New("username already in use")
	ErrEmailTaken            = common.Classify(common.ErrConflict, errors.New("email already in use"))

	// ErrInsufficientPrivilege is returned when a caller attempts to modify a
	// target whose effective privilege is equal to or higher than the caller's
	// (e.g. a delegated users:update holder trying to edit a global admin).
	ErrInsufficientPrivilege = errors.New("insufficient privilege to modify this user")
)

func NewUserService(db *database.DB, roleService *role.RoleService, revokeSessionsExcept func(context.Context, *gorm.DB, string, string) error) *UserService {
	return &UserService{
		db:                   db,
		roleService:          roleService,
		argon2Params:         DefaultArgon2Params(),
		argon2Slots:          make(chan struct{}, 4),
		revokeSessionsExcept: revokeSessionsExcept,
	}
}

func (s *UserService) HashPassword(ctx context.Context, password string) (string, error) {
	salt := make([]byte, s.argon2Params.saltLength)
	_, err := rand.Read(salt)
	if err != nil {
		return "", err
	}

	select {
	case s.argon2Slots <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	hash := argon2.IDKey([]byte(password), salt, s.argon2Params.iterations, s.argon2Params.memory, s.argon2Params.parallelism, s.argon2Params.keyLength)
	<-s.argon2Slots

	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	encodedHash := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, s.argon2Params.memory, s.argon2Params.iterations, s.argon2Params.parallelism, b64Salt, b64Hash)

	return encodedHash, nil
}

func (s *UserService) ValidatePassword(ctx context.Context, encodedHash, password string) error {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 {
		return errors.New("invalid hash format")
	}

	var version int
	_, err := fmt.Sscanf(parts[2], "v=%d", &version)
	if err != nil {
		return err
	}
	if version != argon2.Version {
		return errors.New("incompatible version of argon2")
	}

	var memory, iterations uint32
	var parallelism uint8
	_, err = fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism)
	if err != nil {
		return err
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return err
	}

	decodedHash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return err
	}

	hashLen := len(decodedHash)
	if hashLen < 0 || hashLen > 0x7fffffff {
		return errors.New("invalid hash length")
	}

	select {
	case s.argon2Slots <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	comparisonHash := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(hashLen))
	<-s.argon2Slots

	// constant-time compare
	if subtle.ConstantTimeCompare(comparisonHash, decodedHash) != 1 {
		return errors.New("invalid password")
	}

	return nil
}

func (s *UserService) CreateUser(ctx context.Context, localUser *User) (*User, error) {
	if err := normalization.Normalize(localUser); err != nil {
		return nil, err
	}
	username, err := normalizeUsername(localUser.Username)
	if err != nil {
		return nil, err
	}
	localUser.Username = username

	err = dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		if localUser.OidcSubjectId == nil {
			if ensureEmailErr := ensureEmailAvailable(tx, localUser.ID, localUser.Email); ensureEmailErr != nil {
				return ensureEmailErr
			}
		}
		if createUserErr := tx.Create(localUser).Error; createUserErr != nil {
			if isDuplicateKeyError(tx, createUserErr) {
				return ErrUsernameTaken
			}
			return fmt.Errorf("failed to create user: %w", createUserErr)
		}
		return audit.RecordInTx(ctx, tx, types.AuditEntry{
			Action:     types.AuditActionUserCreate,
			TargetType: types.AuditTargetUser,
			TargetID:   localUser.ID,
			TargetName: localUser.Username,
			After:      userSnapshot(*localUser),
		})
	})
	if err != nil {
		return nil, err
	}
	return localUser, nil
}

func (s *UserService) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	return dbutil.FirstWhere[User](ctx, s.db.DB, common.ErrUserNotFound, "username = ?", username)
}

func (s *UserService) GetUserByID(ctx context.Context, id string) (*User, error) {
	return dbutil.FirstWhere[User](ctx, s.db.DB, common.ErrUserNotFound, "id = ?", id)
}

func (s *UserService) GetUserByOidcSubjectId(ctx context.Context, subjectId string) (*User, error) {
	return dbutil.FirstWhere[User](ctx, s.db.DB, common.ErrUserNotFound, "oidc_subject_id = ?", subjectId)
}

// GetUserByEmail returns the user with the given email. The schema does not
// enforce email uniqueness, so a lookup matching more than one account returns
// common.ErrAmbiguousUserEmail instead of arbitrarily selecting one.
func (s *UserService) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	var users []User
	// Email addresses are matched case-insensitively; providers often return different casing.
	if err := s.db.DB.WithContext(ctx).Where("LOWER(email) = LOWER(?)", email).Limit(2).Find(&users).Error; err != nil {
		return nil, fmt.Errorf("failed to query users by email: %w", err)
	}
	switch len(users) {
	case 0:
		return nil, common.ErrUserNotFound
	case 1:
		return &users[0], nil
	default:
		return nil, common.ErrAmbiguousUserEmail
	}
}

// ResolveUserPermissions returns the effective PermissionSet for the given
// user ID, or nil when no role.RoleService is wired (RBAC disabled paths).
func (s *UserService) ResolveUserPermissions(ctx context.Context, userID string) (*authz.PermissionSet, error) {
	if s.roleService == nil {
		return nil, nil
	}
	return s.roleService.ResolvePermissions(ctx, userID)
}

// checkTargetPrivilege enforces actor-vs-target privilege ordering: a
// caller may not modify a target whose effective permissions make them a global
// admin unless the caller is itself a global admin. This prevents holders of
// delegated users:update/users:delete from seizing or removing admin accounts.
// A nil actorPerms denotes a trusted internal caller (e.g. the auth service
// acting on behalf of the account owner) and skips the privilege check.
//
// The target and any request-time global-admin actor are locked together before
// permissions are resolved through tx (never the TTL cache). Role-assignment
// writes lock the same user rows, so a concurrent promotion or demotion is
// serialized with the mutation. The request-time actor permissions remain part
// of the decision so revalidation can only reduce, never elevate, authority.
func (s *UserService) checkTargetPrivilege(ctx context.Context, tx *gorm.DB, actorPerms *authz.PermissionSet, targetID string) error {
	revalidateActor := actorPerms != nil && !actorPerms.Sudo && actorPerms.IsGlobalAdmin() && s.roleService != nil
	actorID := ""
	if revalidateActor {
		actor, ok := userctx.CurrentUserFromContext(ctx)
		if !ok || actor == nil || actor.ID == "" {
			return ErrInsufficientPrivilege
		}
		actorID = actor.ID
	}

	userIDs := []string{targetID}
	if actorID != "" && actorID != targetID {
		userIDs = append(userIDs, actorID)
	}
	var lockedUsers []User
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id IN ?", userIDs).
		Order("id ASC").
		Find(&lockedUsers).Error; err != nil {
		return fmt.Errorf("failed to lock users: %w", err)
	}
	targetFound := false
	actorFound := actorID == ""
	for i := range lockedUsers {
		targetFound = targetFound || lockedUsers[i].ID == targetID
		actorFound = actorFound || lockedUsers[i].ID == actorID
	}
	if !targetFound {
		return common.ErrUserNotFound
	}
	if !actorFound {
		return ErrInsufficientPrivilege
	}

	if actorPerms == nil || actorPerms.Sudo || s.roleService == nil {
		return nil
	}
	if revalidateActor {
		currentActorPerms, err := s.roleService.ResolveUserPermissionsInDB(ctx, tx, actorID)
		if err != nil {
			return fmt.Errorf("failed to resolve actor permissions: %w", err)
		}
		if currentActorPerms != nil && currentActorPerms.IsGlobalAdmin() {
			return nil
		}
	}
	targetPerms, err := s.roleService.ResolveUserPermissionsInDB(ctx, tx, targetID)
	if err != nil {
		return fmt.Errorf("failed to resolve target permissions: %w", err)
	}
	return kit.Ternary[error](targetPerms != nil && targetPerms.IsGlobalAdmin(), ErrInsufficientPrivilege, nil)
}

// UpdateUser persists the given user. actorPerms identifies the caller
// performing the change; authenticated global-admin callers must also be
// present in ctx. Pass nil for internal service-to-service calls.
func (s *UserService) UpdateUser(ctx context.Context, localUser *User, actorPerms *authz.PermissionSet) (*User, error) {
	if err := normalization.Normalize(localUser); err != nil {
		return nil, err
	}
	var updated User
	err := dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		if err := s.checkTargetPrivilege(ctx, tx, actorPerms, localUser.ID); err != nil {
			return err
		}
		var current User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", localUser.ID).First(&current).Error; err != nil {
			return fmt.Errorf("failed to load user: %w", err)
		}
		if !strings.EqualFold(kit.FromPtr(current.Email), kit.FromPtr(localUser.Email)) {
			if err := ensureEmailAvailable(tx, localUser.ID, localUser.Email); err != nil {
				return err
			}
		}
		if err := tx.Model(&User{}).
			Where("id = ?", localUser.ID).
			Select("username", "display_name", "email", "locale", "time_format", "font_size", "preferences", "updated_at").
			Updates(&User{
				BaseModel: database.BaseModel{
					UpdatedAt: new(time.Now()),
				},
				Username:    localUser.Username,
				DisplayName: localUser.DisplayName,
				Email:       localUser.Email,
				Locale:      localUser.Locale,
				TimeFormat:  localUser.TimeFormat,
				FontSize:    localUser.FontSize,
				Preferences: localUser.Preferences,
			}).Error; err != nil {
			if isDuplicateKeyError(tx, err) {
				return ErrUsernameTaken
			}
			return fmt.Errorf("failed to update user: %w", err)
		}
		if reloadErr := tx.Where("id = ?", localUser.ID).First(&updated).Error; reloadErr != nil {
			return fmt.Errorf("failed to reload user: %w", reloadErr)
		}
		return audit.RecordInTx(ctx, tx, types.AuditEntry{
			Action:     types.AuditActionUserUpdate,
			TargetType: types.AuditTargetUser,
			TargetID:   updated.ID,
			TargetName: updated.Username,
			Before:     userSnapshot(current),
			After:      userSnapshot(updated),
		})
	})
	if err != nil {
		return nil, err
	}
	*localUser = updated
	return localUser, nil
}

func ensureEmailAvailable(tx *gorm.DB, userID string, email *string) error {
	if kit.FromPtr(email) == "" {
		return nil
	}
	var ids []string
	if err := tx.Model(&User{}).
		Where("LOWER(email) = LOWER(?) AND id <> ?", *email, userID).
		Limit(1).
		Pluck("id", &ids).Error; err != nil {
		return fmt.Errorf("failed to check email availability: %w", err)
	}
	return kit.Ternary[error](len(ids) > 0, ErrEmailTaken, nil)
}

// TouchLastLogin updates only the user's last login time.
func (s *UserService) TouchLastLogin(ctx context.Context, userID string, at time.Time) error {
	err := s.db.DB.WithContext(ctx).
		Model(&User{}).
		Where("id = ?", userID).
		Updates(map[string]any{
			"last_login": at,
		}).Error
	if err != nil {
		return fmt.Errorf("failed to update user last login: %w", err)
	}
	return nil
}

func normalizeUsername(username string) (string, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return "", ErrUsernameRequired
	}
	return username, nil
}

func isDuplicateKeyError(db *gorm.DB, err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	translator, ok := db.Dialector.(gorm.ErrorTranslator)
	return ok && errors.Is(translator.Translate(err), gorm.ErrDuplicatedKey)
}

// SetPassword hashes and persists a new password for the given user and clears
// the first-login password-change requirement for trusted internal callers.
func (s *UserService) SetPassword(ctx context.Context, localUser *User, password string) (*User, error) {
	if localUser == nil {
		return nil, errors.New("user is nil")
	}

	hashedPassword, err := s.HashPassword(ctx, password)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	var updated User
	err = dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		result := tx.Model(&User{}).
			Where("id = ?", localUser.ID).
			Select("password_hash", "requires_password_change", "updated_at").
			Updates(&User{
				BaseModel: database.BaseModel{
					UpdatedAt: new(time.Now()),
				},
				PasswordHash: hashedPassword,
			})
		if result.Error != nil {
			return fmt.Errorf("failed to update user password: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return common.ErrUserNotFound
		}
		if reloadErr := tx.Where("id = ?", localUser.ID).First(&updated).Error; reloadErr != nil {
			return fmt.Errorf("failed to reload user: %w", reloadErr)
		}
		return audit.RecordInTx(ctx, tx, types.AuditEntry{
			Action:     types.AuditActionUserPasswordReset,
			TargetType: types.AuditTargetUser,
			TargetID:   updated.ID,
			TargetName: updated.Username,
		})
	})
	if err != nil {
		return nil, err
	}
	*localUser = updated
	return localUser, nil
}

// SetPasswordAndRevokeSessionsExcept atomically sets a new password, clears
// the first-login password-change requirement, and revokes every session for
// the user except exceptSessionID. Pass an empty exceptSessionID to revoke all
// sessions. This is for trusted internal callers.
func (s *UserService) SetPasswordAndRevokeSessionsExcept(ctx context.Context, localUser *User, password, exceptSessionID string, actorPerms *authz.PermissionSet) (*User, error) {
	if localUser == nil {
		return nil, errors.New("user is nil")
	}
	if strings.TrimSpace(localUser.ID) == "" {
		return nil, common.ErrUserNotFound
	}

	hashedPassword, err := s.HashPassword(ctx, password)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	var updatedUser User
	err = dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		if privilegeErr := s.checkTargetPrivilege(ctx, tx, actorPerms, localUser.ID); privilegeErr != nil {
			return privilegeErr
		}
		if loadPasswordResetUserErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", localUser.ID).
			First(&updatedUser).Error; loadPasswordResetUserErr != nil {
			if errors.Is(loadPasswordResetUserErr, gorm.ErrRecordNotFound) {
				return common.ErrUserNotFound
			}
			return fmt.Errorf("failed to load user for password reset: %w", loadPasswordResetUserErr)
		}

		updatedUser.PasswordHash = hashedPassword
		updatedUser.RequiresPasswordChange = false
		if updatePasswordErr := tx.Save(&updatedUser).Error; updatePasswordErr != nil {
			return fmt.Errorf("failed to update user password: %w", updatePasswordErr)
		}
		if recordErr := audit.RecordInTx(ctx, tx, types.AuditEntry{
			Action:     types.AuditActionUserPasswordReset,
			TargetType: types.AuditTargetUser,
			TargetID:   updatedUser.ID,
			TargetName: updatedUser.Username,
			Metadata:   map[string]any{"sessionsRevoked": true},
		}); recordErr != nil {
			return recordErr
		}
		return s.revokeSessionsExcept(ctx, tx, updatedUser.ID, exceptSessionID)
	})
	if err != nil {
		return nil, err
	}

	*localUser = updatedUser
	return localUser, nil
}

// AttachOidcSubjectTransactional safely links an OIDC subject to the given user inside a DB transaction.
// It uses a row lock (FOR UPDATE) to prevent concurrent merges from racing and validates that the
// user isn't already linked to a different subject. The provided updateFn can mutate the user (e.g.,
// roles, display name, tokens, last login) before persisting.
//
// Note: The clause.Locking{Strength: "UPDATE"} statement is used to acquire a row-level lock.
// This MUST be done inside a transaction to ensure the lock is held until the update is committed.
func (s *UserService) AttachOidcSubjectTransactional(ctx context.Context, userID, subject string, updateFn func(u *User)) (*User, error) {
	var out *User
	err := dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		var u User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", userID).
			First(&u).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return common.ErrUserNotFound
			}
			return fmt.Errorf("failed to load user for OIDC merge: %w", err)
		}

		// If already linked to a different subject, abort
		if u.OidcSubjectId != nil && *u.OidcSubjectId != "" && *u.OidcSubjectId != subject {
			return common.Classify(common.ErrConflict, errors.New("user already linked to another OIDC subject"))
		}

		// Link subject
		u.OidcSubjectId = new(subject)

		if updateFn != nil {
			updateFn(&u)
		}

		if err := normalization.Normalize(&u); err != nil {
			return err
		}
		if err := tx.Save(&u).Error; err != nil {
			// Bubble up uniqueness violations with a clearer message
			if strings.Contains(strings.ToLower(err.Error()), "unique") || strings.Contains(strings.ToLower(err.Error()), "duplicate key") {
				slog.WarnContext(ctx, "OIDC subject link rejected by uniqueness constraint", "userId", userID, "error", err)
				return common.Classify(common.ErrConflict, errors.New("oidc subject is already linked to another user"))
			}
			return fmt.Errorf("failed to persist OIDC merge: %w", err)
		}
		out = &u
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *UserService) CreateDefaultAdmin(ctx context.Context) error {
	// If users already exist and at least one effective global admin remains,
	// there is nothing to bootstrap or recover — bail before touching the
	// `arcane` username at all. The username is mutable, so treating it as a
	// provenance marker would let anyone with users:create/users:update rename
	// an account to `arcane` and get promoted on the next restart.
	if s.roleService != nil {
		var count int64
		if err := s.db.WithContext(ctx).Model(&User{}).Count(&count).Error; err != nil {
			return fmt.Errorf("failed to count users: %w", err)
		}
		if count > 0 {
			admins, err := s.roleService.CountAdmins(ctx, s.db.DB)
			if err != nil {
				return fmt.Errorf("failed to count global admins: %w", err)
			}
			if admins > 0 {
				return nil
			}
		}
	}

	// Step 1: ensure the default admin user row exists. If the users table is
	// empty we create it; otherwise (zero global admins — a broken/ancient
	// upgrade) we find the existing `arcane` user to recover, if any.
	var adminUserID string
	err := dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&User{}).Count(&count).Error; err != nil {
			return fmt.Errorf("failed to count users: %w", err)
		}

		if count == 0 {
			hashedPassword, err := s.HashPassword(ctx, "arcane-admin")
			if err != nil {
				return fmt.Errorf("failed to hash default admin password: %w", err)
			}
			email := "admin@localhost"
			displayName := "Arcane Admin"
			userModel := &User{
				Username:               "arcane",
				Email:                  new(email),
				DisplayName:            new(displayName),
				PasswordHash:           hashedPassword,
				RequiresPasswordChange: true,
			}
			if createAdminUserErr := tx.Create(userModel).Error; createAdminUserErr != nil {
				return fmt.Errorf("failed to create default admin user: %w", createAdminUserErr)
			}
			adminUserID = userModel.ID
			slog.InfoContext(ctx, "👑 Default admin user created!")
			slog.InfoContext(ctx, "🔑 Username: arcane")
			slog.InfoContext(ctx, "🔑 Password: arcane-admin")
			slog.InfoContext(ctx, "⚠️  User will be prompted to change password on first login")
			return nil
		}

		// Users exist but no global admin does (recovery only — the early
		// return above skips this path on healthy installs). See if `arcane`
		// is present; if not, leave the install alone.
		var existing User
		err := tx.Where("username = ?", "arcane").First(&existing).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return fmt.Errorf("failed to look up default admin user: %w", err)
		}
		adminUserID = existing.ID
		return nil
	})
	if err != nil {
		return err
	}

	if adminUserID == "" || s.roleService == nil {
		return nil
	}
	return s.grantDefaultAdminRole(ctx, adminUserID)
}

// grantDefaultAdminRole grants the global Admin role to the bootstrap
// user. Reached only on fresh bootstrap or zero-admin recovery.
func (s *UserService) grantDefaultAdminRole(ctx context.Context, adminUserID string) error {
	if err := s.roleService.BindUser(ctx, adminUserID, authz.BuiltInRoleAdmin); err != nil {
		return fmt.Errorf("failed to grant default admin global role: %w", err)
	}
	slog.InfoContext(ctx, "Default admin granted global Admin role binding", "userId", adminUserID)
	return nil
}

// DeleteUser removes the user with the given id. actorPerms identifies the
// caller performing the deletion; authenticated global-admin callers must also
// be present in ctx. Pass nil for internal service-to-service calls. A
// non-admin caller may not delete a global admin target.
func (s *UserService) DeleteUser(ctx context.Context, id string, actorPerms *authz.PermissionSet) error {
	err := dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		if err := s.roleService.LockGlobalAdminGuard(tx); err != nil {
			return err
		}
		if err := s.checkTargetPrivilege(ctx, tx, actorPerms, id); err != nil {
			return err
		}
		targetPerms, err := s.roleService.ResolveUserPermissionsInDB(ctx, tx, id)
		if err != nil {
			return fmt.Errorf("failed to resolve target permissions: %w", err)
		}
		var existing User
		if loadErr := tx.Where("id = ?", id).Limit(1).Find(&existing).Error; loadErr != nil {
			return fmt.Errorf("failed to load user: %w", loadErr)
		}
		if deleteErr := tx.Delete(&User{}, "id = ?", id).Error; deleteErr != nil {
			return fmt.Errorf("failed to delete user: %w", deleteErr)
		}
		if recordErr := audit.RecordInTx(ctx, tx, types.AuditEntry{
			Action:     types.AuditActionUserDelete,
			TargetType: types.AuditTargetUser,
			TargetID:   id,
			TargetName: existing.Username,
			Before:     userSnapshot(existing),
		}); recordErr != nil {
			return recordErr
		}
		if !targetPerms.IsGlobalAdmin() {
			return nil
		}
		return s.roleService.AssertGlobalAdminExists(ctx, tx)
	})
	if errors.Is(err, common.ErrNoGlobalAdminRemains) {
		return ErrCannotRemoveLastAdmin
	}
	return err
}

func (s *UserService) ListUsersPaginated(ctx context.Context, params pagination.QueryParams) ([]types.User, pagination.Response, error) {
	var users []User
	query := s.db.WithContext(ctx).
		Model(&User{}).
		Where("is_service_account = ?", false)

	if term := strings.TrimSpace(params.Search); term != "" {
		searchPattern := "%" + term + "%"
		query = query.Where(
			"username LIKE ? OR COALESCE(email, '') LIKE ? OR COALESCE(display_name, '') LIKE ?",
			searchPattern, searchPattern, searchPattern,
		)
	}

	paginationResp, err := pagination.PaginateAndSortDB(params, query, &users)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to paginate users: %w", err)
	}

	result := s.toUserResponseDtos(ctx, users)

	return result, paginationResp, nil
}

func (s *UserService) ToUserResponseDto(ctx context.Context, u User) (types.User, error) {
	dto := s.toUserResponseDto(ctx, u)
	if dto.IsGlobalAdmin && s.roleService != nil {
		remaining, err := s.roleService.CountAdmins(ctx, s.db.DB, u.ID)
		if err != nil {
			return types.User{}, fmt.Errorf("failed to check whether user can be deleted: %w", err)
		}
		dto.CanDelete = remaining > 0
	}
	return dto, nil
}

// toUserResponseDtos maps a user list. CanDelete is false on the only
// effective global admin.
func (s *UserService) toUserResponseDtos(ctx context.Context, users []User) []types.User {
	result := make([]types.User, len(users))
	for i, u := range users {
		result[i] = s.toUserResponseDto(ctx, u)
	}
	if s.roleService == nil {
		return result
	}
	if total, err := s.roleService.CountAdmins(ctx, s.db.DB); err == nil && total == 1 {
		for i := range result {
			if result[i].IsGlobalAdmin {
				result[i].CanDelete = false
			}
		}
	}
	return result
}

func (s *UserService) toUserResponseDto(ctx context.Context, u User) types.User {
	dto := types.User{
		ID:                     u.ID,
		Username:               u.Username,
		DisplayName:            u.DisplayName,
		Email:                  u.Email,
		CanDelete:              true,
		OidcSubjectId:          u.OidcSubjectId,
		Locale:                 u.Locale,
		TimeFormat:             u.TimeFormat,
		FontSize:               u.FontSize,
		Preferences:            u.Preferences,
		RequiresPasswordChange: u.RequiresPasswordChange,
		CreatedAt:              u.CreatedAt.Format("2006-01-02T15:04:05.999999Z"),
		UpdatedAt:              u.UpdatedAt.Format("2006-01-02T15:04:05.999999Z"),
		Bindings:               []types.RoleBindingSummary{},
		Groups:                 []types.GroupRef{},
		PermissionsByEnv:       map[string][]string{},
		ScopedPermissionsByEnv: map[string][]string{},
	}
	if u.LastLogin != nil {
		lastLogin := u.LastLogin.Format("2006-01-02T15:04:05.999999Z")
		dto.LastLogin = &lastLogin
	}
	// Populate AvatarURL when the user has a custom avatar stored
	if u.HasAvatar {
		avatarURL := fmt.Sprintf("/api/users/%s/avatar", u.ID)
		dto.AvatarURL = &avatarURL
	}
	if s.roleService == nil {
		return dto
	}
	if bindings, err := s.roleService.ListUserBindings(ctx, u.ID); err == nil {
		dto.Bindings = bindings
	}
	if groups, err := s.roleService.ListUserGroups(ctx, u.ID); err == nil {
		dto.Groups = groups
	}
	if ps, err := s.roleService.ResolvePermissions(ctx, u.ID); err == nil && ps != nil {
		dto.IsGlobalAdmin = ps.IsGlobalAdmin()
		dto.PermissionsByEnv, dto.ScopedPermissionsByEnv = permissionSetToMaps(ps)
	}
	return dto
}

func permissionSetToMaps(ps *authz.PermissionSet) (all, scoped map[string][]string) {
	all, scoped = map[string][]string{}, map[string][]string{}
	for _, envID := range append([]string{""}, slices.Collect(maps.Keys(ps.PerEnv))...) {
		envAll, envScoped := ps.PermissionsForEnv(envID)
		if !ps.Sudo && slices.Contains(envAll, authz.PermAll) {
			envAll = slices.Compact(slices.Sorted(slices.Values(slices.Concat(
				envAll,
				slices.DeleteFunc(authz.AllPermissions(), func(p string) bool {
					return envID != "" && !authz.IsEnvScoped(p)
				}),
			))))
		}
		if len(envAll) > 0 {
			all[cmp.Or(envID, "global")] = envAll
		}
		if len(envScoped) > 0 {
			scoped[cmp.Or(envID, "global")] = envScoped
		}
	}
	return all, scoped
}

func (s *UserService) GetUser(ctx context.Context, userID string) (*User, error) {
	slog.DebugContext(ctx, "GetUser called", "userId", userID)
	return s.getUser(ctx, userID, s.db.DB)
}

func (s *UserService) getUser(ctx context.Context, userID string, tx *gorm.DB) (*User, error) {
	var userRecord User
	err := tx.WithContext(ctx).
		Where("id = ?", userID).
		First(&userRecord).
		Error
	return &userRecord, err
}

// UploadAvatar stores avatar bytes for a user, replacing any existing avatar.
func (s *UserService) UploadAvatar(ctx context.Context, userID string, data []byte, mimeType string) error {
	return dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		avatar := UserAvatar{
			UserID:   userID,
			Data:     data,
			MimeType: mimeType,
		}
		if err := tx.Save(&avatar).Error; err != nil {
			return fmt.Errorf("failed to save avatar data: %w", err)
		}

		err := tx.Model(&User{}).
			Where("id = ?", userID).
			Updates(map[string]any{
				"has_avatar": true,
			}).Error
		if err != nil {
			return fmt.Errorf("failed to update user avatar flag: %w", err)
		}
		return nil
	})
}

// DeleteAvatar removes the avatar for a user.
func (s *UserService) DeleteAvatar(ctx context.Context, userID string) error {
	return dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", userID).Delete(&UserAvatar{}).Error; err != nil {
			return fmt.Errorf("failed to delete avatar data: %w", err)
		}

		err := tx.Model(&User{}).
			Where("id = ?", userID).
			Updates(map[string]any{
				"has_avatar": false,
			}).Error
		if err != nil {
			return fmt.Errorf("failed to update user avatar flag: %w", err)
		}
		return nil
	})
}

// GetAvatar returns the raw avatar data and MIME type for a user.
// Returns (nil, "", nil) when the user has no custom avatar.
func (s *UserService) GetAvatar(ctx context.Context, userID string) ([]byte, string, error) {
	u, err := s.GetUserByID(ctx, userID)
	if err != nil {
		return nil, "", err
	}
	if !u.HasAvatar {
		return nil, "", nil
	}
	var avatar UserAvatar
	if loadAvatarErr := s.db.DB.WithContext(ctx).Where("user_id = ?", userID).First(&avatar).Error; loadAvatarErr != nil {
		return nil, "", kit.Ternary(errors.Is(loadAvatarErr, gorm.ErrRecordNotFound), nil, loadAvatarErr)
	}
	return avatar.Data, avatar.MimeType, nil
}
