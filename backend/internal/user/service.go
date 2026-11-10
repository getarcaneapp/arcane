package user

import (
	"cmp"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"kit.getarcane.app"
	"kit.getarcane.app/normalization"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/role"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
	"getarcane.dev/arcane/v3/pkg/pagination"
	"getarcane.dev/arcane/v3/pkg/sqlite"
	"getarcane.dev/arcane/v3/pkg/validation"
	"getarcane.dev/types/v3"
)

type (
	Argon2Params struct {
		memory      uint32
		iterations  uint32
		parallelism uint8
		saltLength  uint32
		keyLength   uint32
	}

	UserService struct {
		db                   *database.DB
		roleService          *role.RoleService
		argon2Params         *Argon2Params
		argon2Slots          chan struct{}
		revokeSessionsExcept func(context.Context, *gorm.DB, string, string) error
	}

	DefaultAdminOptions struct {
		Username       string
		Password       string
		PasswordPolicy string
	}
)

const BackdropMaxBytes = 10 << 20

var (
	ErrCannotRemoveLastAdmin = errors.New("cannot remove the last admin user")
	ErrUsernameRequired      = errors.New("username is required")
	ErrUsernameTaken         = errors.New("username already in use")
	ErrEmailTaken            = common.Classify(common.ErrConflict, errors.New("email already in use"))
	ErrInsufficientPrivilege = errors.New("insufficient privilege to modify this user")
	ErrBackdropTooLarge      = common.Classify(common.ErrValidation, fmt.Errorf("background image must be %d MB or smaller", BackdropMaxBytes>>20))
	ErrBackdropUnsupported   = common.Classify(common.ErrValidation, errors.New("unsupported image format: only PNG, JPEG and WebP are accepted"))
)

func DefaultArgon2Params() *Argon2Params {
	return &Argon2Params{
		memory:      64 * 1024,
		iterations:  3,
		parallelism: 2,
		saltLength:  16,
		keyLength:   32,
	}
}

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
	if localUser.OidcSubjectId == nil {
		email, emailErr := NormalizeOptionalEmail(localUser.Email)
		if emailErr != nil {
			return nil, emailErr
		}
		localUser.Email = email
	}

	err = s.db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if localUser.OidcSubjectId == nil {
			if ensureEmailErr := ensureEmailAvailable(tx, localUser.ID, localUser.Email); ensureEmailErr != nil {
				return ensureEmailErr
			}
		}
		if createUserErr := tx.Create(localUser).Error; createUserErr != nil {
			if database.IsDuplicateKeyError(tx, createUserErr) {
				return ErrUsernameTaken
			}
			return fmt.Errorf("failed to create user: %w", createUserErr)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return localUser, nil
}

func (s *UserService) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	return sqlite.FirstWhere[User](ctx, s.db.DB, common.ErrUserNotFound, "username = ?", username)
}

func (s *UserService) GetUserByID(ctx context.Context, id string) (*User, error) {
	return sqlite.FirstWhere[User](ctx, s.db.DB, common.ErrUserNotFound, "id = ?", id)
}

func (s *UserService) GetUserByOidcSubjectId(ctx context.Context, subjectId string) (*User, error) {
	return sqlite.FirstWhere[User](ctx, s.db.DB, common.ErrUserNotFound, "oidc_subject_id = ?", subjectId)
}

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

func (s *UserService) checkTargetPrivilege(ctx context.Context, tx *gorm.DB, actorPerms *authz.PermissionSet, targetID string) error {
	revalidateActor := actorPerms != nil && !actorPerms.Sudo && actorPerms.IsGlobalAdmin()
	actorID := ""
	if revalidateActor {
		actor, ok := handlerutil.CurrentUserFromContext(ctx)
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

	if actorPerms == nil || actorPerms.Sudo {
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

func (s *UserService) UpdateUser(ctx context.Context, localUser *User, actorPerms *authz.PermissionSet) (*User, error) {
	if err := normalization.Normalize(localUser); err != nil {
		return nil, err
	}
	if err := validateCustomThemes(kit.FromPtr(localUser.Preferences.CustomThemes)); err != nil {
		return nil, err
	}
	if layout := localUser.Preferences.DashboardLayout; layout != nil {
		localUser.Preferences.DashboardLayout = new(normalizeDashboardLayout(*layout))
	}
	var updated User
	err := s.db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.checkTargetPrivilege(ctx, tx, actorPerms, localUser.ID); err != nil {
			return err
		}
		var current User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", localUser.ID).First(&current).Error; err != nil {
			return fmt.Errorf("failed to load user: %w", err)
		}
		if !strings.EqualFold(kit.FromPtr(current.Email), strings.TrimSpace(kit.FromPtr(localUser.Email))) {
			email, normalizeErr := NormalizeOptionalEmail(localUser.Email)
			if normalizeErr != nil {
				return normalizeErr
			}
			localUser.Email = email
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
			if database.IsDuplicateKeyError(tx, err) {
				return ErrUsernameTaken
			}
			return fmt.Errorf("failed to update user: %w", err)
		}
		if reloadErr := tx.Where("id = ?", localUser.ID).First(&updated).Error; reloadErr != nil {
			return fmt.Errorf("failed to reload user: %w", reloadErr)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	*localUser = updated
	return localUser, nil
}

func NormalizeOptionalEmail(email *string) (*string, error) {
	trimmedEmail := strings.TrimSpace(kit.FromPtr(email))
	if trimmedEmail == "" {
		return nil, nil
	}
	if !validation.IsValidUserEmail(trimmedEmail) {
		return nil, common.Classify(common.ErrValidation, &types.FieldError{Field: "email", Err: errors.New("email must be a valid email address")})
	}
	return &trimmedEmail, nil
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

func (s *UserService) SetPassword(ctx context.Context, localUser *User, password string) (*User, error) {
	if localUser == nil {
		return nil, errors.New("user is nil")
	}

	hashedPassword, err := s.HashPassword(ctx, password)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	var updated User
	err = s.db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
		return nil
	})
	if err != nil {
		return nil, err
	}
	*localUser = updated
	return localUser, nil
}

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
	err = s.db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
		return s.revokeSessionsExcept(ctx, tx, updatedUser.ID, exceptSessionID)
	})
	if err != nil {
		return nil, err
	}

	*localUser = updatedUser
	return localUser, nil
}

func (s *UserService) AttachOidcSubjectTransactional(ctx context.Context, userID, subject string, updateFn func(u *User)) (*User, error) {
	var out *User
	err := s.db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var u User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", userID).
			First(&u).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return common.ErrUserNotFound
			}
			return fmt.Errorf("failed to load user for OIDC merge: %w", err)
		}

		if current := kit.FromPtr(u.OidcSubjectId); current != "" && current != subject {
			return common.Classify(common.ErrConflict, errors.New("user already linked to another OIDC subject"))
		}

		u.OidcSubjectId = new(subject)

		if updateFn != nil {
			updateFn(&u)
		}

		if err := normalization.Normalize(&u); err != nil {
			return err
		}
		if err := tx.Save(&u).Error; err != nil {
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

func (s *UserService) CreateDefaultAdmin(ctx context.Context, opts DefaultAdminOptions) error {
	var count int64
	if err := s.db.WithContext(ctx).Model(&User{}).Count(&count).Error; err != nil {
		return fmt.Errorf("failed to count users: %w", err)
	}
	if count > 0 {
		admins, err := s.roleService.CountGlobalAdmins(ctx, s.db.DB, "")
		if err != nil {
			return fmt.Errorf("failed to count global admins: %w", err)
		}
		if admins > 0 {
			return nil
		}
	}

	var adminUserID string
	err := s.db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var userCount int64
		if err := tx.Model(&User{}).Count(&userCount).Error; err != nil {
			return fmt.Errorf("failed to count users: %w", err)
		}

		if userCount == 0 {
			if opts.Password != "" {
				if err := validation.ValidatePasswordPolicy(opts.Password, opts.PasswordPolicy); err != nil {
					return fmt.Errorf("invalid ARCANE_DEFAULT_ADMIN_PASSWORD: %w", err)
				}
			}
			password := cmp.Or(opts.Password, rand.Text())
			hashedPassword, err := s.HashPassword(ctx, password)
			if err != nil {
				return fmt.Errorf("failed to hash default admin password: %w", err)
			}
			email := "admin@localhost"
			displayName := "Arcane Admin"
			userModel := &User{
				Username:               opts.Username,
				Email:                  new(email),
				DisplayName:            new(displayName),
				PasswordHash:           hashedPassword,
				RequiresPasswordChange: opts.Password == "",
			}
			if createAdminUserErr := tx.Create(userModel).Error; createAdminUserErr != nil {
				return fmt.Errorf("failed to create default admin user: %w", createAdminUserErr)
			}
			adminUserID = userModel.ID
			slog.InfoContext(ctx, "👑 Default admin user created!")
			slog.InfoContext(ctx, "🔑 Default admin username", "username", opts.Username)
			if opts.Password == "" {
				slog.InfoContext(ctx, "🔑 Default admin password", "generatedPassword", password)
				slog.InfoContext(ctx, "⚠️  User will be prompted to change password on first login")
			}
			return nil
		}

		// Users exist but no global admin does (recovery only — the early
		// return above skips this path on healthy installs). See if the default
		// admin is present; if not, leave the install alone.
		var existing User
		err := tx.Where("username = ?", opts.Username).First(&existing).Error
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

	if adminUserID == "" {
		return nil
	}
	return s.grantDefaultAdminRole(ctx, adminUserID)
}

func (s *UserService) grantDefaultAdminRole(ctx context.Context, adminUserID string) error {
	assignments, err := s.roleService.ListUserAssignments(ctx, adminUserID)
	if err != nil {
		return fmt.Errorf("failed to list default admin assignments: %w", err)
	}
	for _, a := range assignments {
		if a.RoleID == authz.BuiltInRoleAdmin && a.EnvironmentID == nil {
			return nil
		}
	}
	manual := make([]role.UserRoleAssignment, 0, len(assignments)+1)
	for _, a := range assignments {
		if a.Source == role.RoleAssignmentSourceManual {
			manual = append(manual, role.UserRoleAssignment{RoleID: a.RoleID, EnvironmentID: a.EnvironmentID})
		}
	}
	manual = append(manual, role.UserRoleAssignment{
		RoleID:        authz.BuiltInRoleAdmin,
		EnvironmentID: nil,
	})
	if setUserAssignmentsErr := s.roleService.ReplaceUserAssignments(ctx, adminUserID, role.RoleAssignmentSourceManual, manual); setUserAssignmentsErr != nil {
		return fmt.Errorf("failed to grant default admin global role: %w", setUserAssignmentsErr)
	}
	slog.InfoContext(ctx, "Default admin granted global Admin role assignment", "userId", adminUserID)
	return nil
}

func (s *UserService) DeleteUser(ctx context.Context, id string, actorPerms *authz.PermissionSet) error {
	err := s.db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
		if deleteErr := tx.Delete(&User{}, "id = ?", id).Error; deleteErr != nil {
			return fmt.Errorf("failed to delete user: %w", deleteErr)
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
	if dto.IsGlobalAdmin {
		remaining, err := s.roleService.CountGlobalAdmins(ctx, s.db.DB, u.ID)
		if err != nil {
			return types.User{}, fmt.Errorf("failed to check whether user can be deleted: %w", err)
		}
		dto.CanDelete = remaining > 0
	}
	return dto, nil
}

func (s *UserService) toUserResponseDtos(ctx context.Context, users []User) []types.User {
	result := make([]types.User, len(users))
	for i, u := range users {
		result[i] = s.toUserResponseDto(ctx, u)
	}
	if total, err := s.roleService.CountGlobalAdmins(ctx, s.db.DB, ""); err == nil && total == 1 {
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
		RoleAssignments:        []types.RoleAssignmentSummary{},
		PermissionsByEnv:       map[string][]string{},
	}
	if u.LastLogin != nil {
		lastLogin := u.LastLogin.Format("2006-01-02T15:04:05.999999Z")
		dto.LastLogin = &lastLogin
	}
	if u.HasAvatar {
		avatarURL := fmt.Sprintf("/api/users/%s/avatar", u.ID)
		dto.AvatarURL = &avatarURL
	}
	if u.BackdropHash != nil {
		// The content hash makes the URL change on every upload, so it can be cached as immutable.
		dto.BackdropURL = new("/api/auth/me/backdrop?v=" + *u.BackdropHash)
	}
	if rows, err := s.roleService.ListUserAssignments(ctx, u.ID); err == nil {
		dto.RoleAssignments = make([]types.RoleAssignmentSummary, len(rows))
		for i, r := range rows {
			dto.RoleAssignments[i] = types.RoleAssignmentSummary{
				RoleID:        r.RoleID,
				EnvironmentID: r.EnvironmentID,
				Source:        r.Source,
			}
		}
	}
	if ps, err := s.roleService.ResolvePermissions(ctx, u.ID); err == nil && ps != nil {
		dto.IsGlobalAdmin = ps.IsGlobalAdmin()
		dto.PermissionsByEnv = permissionSetToMap(ps)
	}
	return dto
}

func permissionSetToMap(ps *authz.PermissionSet) map[string][]string {
	out := map[string][]string{}
	if ps == nil {
		return out
	}
	if ps.Sudo {
		out["global"] = []string{"*"}
		return out
	}
	if len(ps.Global) > 0 {
		globals := slices.Collect(maps.Keys(ps.Global))
		out["global"] = globals
	}
	for envID, perms := range ps.PerEnv {
		list := slices.Collect(maps.Keys(perms))
		out[envID] = list
	}
	return out
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

func (s *UserService) UploadAvatar(ctx context.Context, userID string, data []byte, mimeType string) error {
	return s.db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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

func (s *UserService) DeleteAvatar(ctx context.Context, userID string) error {
	return s.db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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

// UploadBackdrop stores the user's background image, replacing any previous upload.
func (s *UserService) UploadBackdrop(ctx context.Context, userID string, r io.Reader) error {
	// Read one byte past the limit so oversized files are rejected without buffering them fully.
	data, readErr := io.ReadAll(io.LimitReader(r, BackdropMaxBytes+1))
	if readErr != nil {
		return fmt.Errorf("failed to read background image: %w", readErr)
	}
	if len(data) > BackdropMaxBytes {
		return ErrBackdropTooLarge
	}
	mimeType := http.DetectContentType(data)
	if !slices.Contains([]string{"image/png", "image/jpeg", "image/webp"}, mimeType) {
		return ErrBackdropUnsupported
	}
	sum := sha256.Sum256(data)

	return s.db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&UserBackdrop{UserID: userID, Data: data, MimeType: mimeType}).Error; err != nil {
			return fmt.Errorf("failed to save background image: %w", err)
		}
		if err := tx.Model(&User{}).Where("id = ?", userID).Update("backdrop_hash", hex.EncodeToString(sum[:8])).Error; err != nil {
			return fmt.Errorf("failed to update user background image hash: %w", err)
		}
		return nil
	})
}

func (s *UserService) DeleteBackdrop(ctx context.Context, userID string) error {
	return s.db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", userID).Delete(&UserBackdrop{}).Error; err != nil {
			return fmt.Errorf("failed to delete background image: %w", err)
		}
		if err := tx.Model(&User{}).Where("id = ?", userID).Update("backdrop_hash", nil).Error; err != nil {
			return fmt.Errorf("failed to clear user background image hash: %w", err)
		}
		return nil
	})
}

// GetBackdrop returns the user's uploaded background image, or common.ErrNotFound when none is uploaded.
func (s *UserService) GetBackdrop(ctx context.Context, userID string) ([]byte, string, error) {
	var backdrop UserBackdrop
	if err := s.db.DB.WithContext(ctx).Where("user_id = ?", userID).First(&backdrop).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, "", common.Classify(common.ErrNotFound, errors.New("no background image uploaded"))
		}
		return nil, "", fmt.Errorf("failed to load background image: %w", err)
	}
	return backdrop.Data, backdrop.MimeType, nil
}
