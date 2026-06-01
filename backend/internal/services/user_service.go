package services

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/pkg/utils/dbutil"
	"github.com/getarcaneapp/arcane/types/user"
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
	db           *database.DB
	roleService  *RoleService
	argon2Params *Argon2Params
}

var ErrCannotRemoveLastAdmin = errors.New("cannot remove the last admin user")

func NewUserService(db *database.DB) *UserService {
	return &UserService{
		db:           db,
		argon2Params: DefaultArgon2Params(),
	}
}

// WithRoleService wires the RoleService dependency. Separated from the
// constructor so the bootstrap can construct UserService first (RoleService
// itself has no UserService dependency).
func (s *UserService) WithRoleService(roleService *RoleService) *UserService {
	s.roleService = roleService
	return s
}

func (s *UserService) hashPassword(password string) (string, error) {
	salt := make([]byte, s.argon2Params.saltLength)
	_, err := rand.Read(salt)
	if err != nil {
		return "", err
	}

	hash := argon2.IDKey([]byte(password), salt, s.argon2Params.iterations, s.argon2Params.memory, s.argon2Params.parallelism, s.argon2Params.keyLength)

	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	encodedHash := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, s.argon2Params.memory, s.argon2Params.iterations, s.argon2Params.parallelism, b64Salt, b64Hash)

	return encodedHash, nil
}

func (s *UserService) ValidatePassword(encodedHash, password string) error {
	// Check if it's a bcrypt hash (starts with $2a$, $2b$, or $2y$)
	if strings.HasPrefix(encodedHash, "$2a$") || strings.HasPrefix(encodedHash, "$2b$") || strings.HasPrefix(encodedHash, "$2y$") {
		return s.validateBcryptPassword(encodedHash, password)
	}

	// Otherwise, assume it's Argon2
	return s.validateArgon2Password(encodedHash, password)
}

func (s *UserService) validateBcryptPassword(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

func (s *UserService) validateArgon2Password(encodedHash, password string) error {
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

	comparisonHash := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(hashLen))

	// constant-time compare
	if subtle.ConstantTimeCompare(comparisonHash, decodedHash) != 1 {
		return errors.New("invalid password")
	}

	return nil
}

func (s *UserService) CreateUser(ctx context.Context, user *models.User) (*models.User, error) {
	err := dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		if err := tx.Create(user).Error; err != nil {
			return fmt.Errorf("failed to create user: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (s *UserService) GetUserByUsername(ctx context.Context, username string) (*models.User, error) {
	return dbutil.FirstWhere[models.User](ctx, s.db.DB, ErrUserNotFound, "username = ?", username)
}

func (s *UserService) GetUserByID(ctx context.Context, id string) (*models.User, error) {
	return dbutil.FirstWhere[models.User](ctx, s.db.DB, ErrUserNotFound, "id = ?", id)
}

func (s *UserService) GetUserByOidcSubjectId(ctx context.Context, subjectId string) (*models.User, error) {
	return dbutil.FirstWhere[models.User](ctx, s.db.DB, ErrUserNotFound, "oidc_subject_id = ?", subjectId)
}

func (s *UserService) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	return dbutil.FirstWhere[models.User](ctx, s.db.DB, ErrUserNotFound, "email = ?", email)
}

func (s *UserService) UpdateUser(ctx context.Context, user *models.User) (*models.User, error) {
	err := dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		if err := tx.
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", user.ID).
			First(&models.User{}).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrUserNotFound
			}
			return fmt.Errorf("failed to load user: %w", err)
		}
		if err := tx.Save(user).Error; err != nil {
			return fmt.Errorf("failed to update user: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return user, nil
}

// AttachOidcSubjectTransactional safely links an OIDC subject to the given user inside a DB transaction.
// It uses a row lock (FOR UPDATE) to prevent concurrent merges from racing and validates that the
// user isn't already linked to a different subject. The provided updateFn can mutate the user (e.g.,
// roles, display name, tokens, last login) before persisting.
//
// Note: The clause.Locking{Strength: "UPDATE"} statement is used to acquire a row-level lock.
// This MUST be done inside a transaction to ensure the lock is held until the update is committed.
func (s *UserService) AttachOidcSubjectTransactional(ctx context.Context, userID string, subject string, updateFn func(u *models.User)) (*models.User, error) {
	var out *models.User
	err := dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		var u models.User
		if err := tx.
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", userID).
			First(&u).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrUserNotFound
			}
			return fmt.Errorf("failed to load user for OIDC merge: %w", err)
		}

		// If already linked to a different subject, abort
		if u.OidcSubjectId != nil && *u.OidcSubjectId != "" && *u.OidcSubjectId != subject {
			return errors.New("user already linked to another OIDC subject")
		}

		// Link subject
		u.OidcSubjectId = new(subject)

		if updateFn != nil {
			updateFn(&u)
		}

		if err := tx.Save(&u).Error; err != nil {
			// Bubble up uniqueness violations with a clearer message
			if strings.Contains(strings.ToLower(err.Error()), "unique") || strings.Contains(strings.ToLower(err.Error()), "duplicate key") {
				return fmt.Errorf("oidc subject is already linked to another user: %w", err)
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
	// Hash password outside transaction to minimize lock time
	hashedPassword, err := s.hashPassword("arcane-admin")
	if err != nil {
		return fmt.Errorf("failed to hash default admin password: %w", err)
	}

	// Step 1: ensure the default admin user row exists. If the users table is
	// empty we create it; otherwise we find the existing `arcane` user (if any).
	// Either way the role assignment is reconciled below — idempotently — so
	// upgrades from older builds that didn't grant the role get patched up.
	var adminUserID string
	err = dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&models.User{}).Count(&count).Error; err != nil {
			return fmt.Errorf("failed to count users: %w", err)
		}

		if count == 0 {
			email := "admin@localhost"
			displayName := "Arcane Admin"
			userModel := &models.User{
				Username:               "arcane",
				Email:                  new(email),
				DisplayName:            new(displayName),
				PasswordHash:           hashedPassword,
				RequiresPasswordChange: true,
			}
			if err := tx.Create(userModel).Error; err != nil {
				return fmt.Errorf("failed to create default admin user: %w", err)
			}
			adminUserID = userModel.ID
			slog.InfoContext(ctx, "👑 Default admin user created!")
			slog.InfoContext(ctx, "🔑 Username: arcane")
			slog.InfoContext(ctx, "🔑 Password: arcane-admin")
			slog.InfoContext(ctx, "⚠️  User will be prompted to change password on first login")
			return nil
		}

		// Users already exist — see if `arcane` is one of them. If not,
		// someone removed the default admin on purpose and we leave it alone.
		var existing models.User
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

	// Step 2: ensure the default admin holds the global Admin role assignment.
	// Idempotent — if the assignment already exists, SetUserAssignments is a
	// no-op write (it dedupes via the unique index). This recovers users that
	// were created by older builds before the role-grant on bootstrap was
	// wired up.
	assignments, err := s.roleService.ListUserAssignments(ctx, adminUserID)
	if err != nil {
		return fmt.Errorf("failed to list default admin assignments: %w", err)
	}
	for _, a := range assignments {
		if a.RoleID == authz.BuiltInRoleAdmin && a.EnvironmentID == nil {
			return nil // already has it
		}
	}
	desired := append([]models.UserRoleAssignment{}, assignments...)
	desired = append(desired, models.UserRoleAssignment{
		RoleID:        authz.BuiltInRoleAdmin,
		EnvironmentID: nil,
	})
	// Strip the source field so SetUserAssignments classifies everything as manual.
	manual := make([]models.UserRoleAssignment, 0, len(desired))
	for _, a := range desired {
		manual = append(manual, models.UserRoleAssignment{RoleID: a.RoleID, EnvironmentID: a.EnvironmentID})
	}
	if err := s.roleService.SetUserAssignments(ctx, adminUserID, manual); err != nil {
		return fmt.Errorf("failed to grant default admin global role: %w", err)
	}
	slog.InfoContext(ctx, "Default admin granted global Admin role assignment", "user_id", adminUserID)
	return nil
}

func (s *UserService) DeleteUser(ctx context.Context, id string) error {
	// Last-admin guard: if this user holds the only global Admin assignment,
	// refuse the delete. Checked OUTSIDE the transaction because RoleService
	// uses its own session — and the guard is informational only (the unique
	// index in user_role_assignments wouldn't catch this cross-row condition).
	if s.roleService != nil {
		var holdsGlobalAdmin bool
		assignments, err := s.roleService.ListUserAssignments(ctx, id)
		if err == nil {
			for _, a := range assignments {
				if a.RoleID == authz.BuiltInRoleAdmin && a.EnvironmentID == nil {
					holdsGlobalAdmin = true
					break
				}
			}
		}
		if holdsGlobalAdmin {
			remaining, err := s.roleService.CountGlobalAdminsExcludingUser(ctx, id)
			if err != nil {
				return err
			}
			if remaining == 0 {
				return ErrCannotRemoveLastAdmin
			}
		}
	}
	return dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		if err := tx.
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", id).
			First(&models.User{}).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrUserNotFound
			}
			return fmt.Errorf("failed to load user: %w", err)
		}

		if err := tx.Delete(&models.User{}, "id = ?", id).Error; err != nil {
			return fmt.Errorf("failed to delete user: %w", err)
		}
		return nil
	})
}

func (s *UserService) HashPassword(password string) (string, error) {
	return s.hashPassword(password)
}

func (s *UserService) NeedsPasswordUpgrade(hash string) bool {
	return strings.HasPrefix(hash, "$2a$") || strings.HasPrefix(hash, "$2b$") || strings.HasPrefix(hash, "$2y$")
}

func (s *UserService) UpgradePasswordHash(ctx context.Context, userID, password string) error {
	newHash, err := s.hashPassword(password)
	if err != nil {
		return fmt.Errorf("failed to create new hash: %w", err)
	}

	return dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		if err := tx.Model(&models.User{}).
			Where("id = ?", userID).
			Update("password_hash", newHash).Error; err != nil {
			return fmt.Errorf("failed to update password hash: %w", err)
		}
		return nil
	})
}

func (s *UserService) ListUsersPaginated(ctx context.Context, params pagination.QueryParams) ([]user.User, pagination.Response, error) {
	var users []models.User
	query := s.db.WithContext(ctx).
		Model(&models.User{}).
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

	result := s.toUserResponseDtosInternal(ctx, users)

	return result, paginationResp, nil
}

func (s *UserService) ToUserResponseDto(ctx context.Context, u models.User) (user.User, error) {
	return s.toUserResponseDtoInternal(ctx, u), nil
}

func (s *UserService) toUserResponseDtosInternal(ctx context.Context, users []models.User) []user.User {
	result := make([]user.User, len(users))
	for i, u := range users {
		result[i] = s.toUserResponseDtoInternal(ctx, u)
	}
	return result
}

// toUserResponseDtoInternal builds the public User DTO. RoleAssignments and
// PermissionsByEnv come from the RBAC service. CanDelete is false when this
// user holds the only global Admin assignment (i.e. removing them would orphan
// the instance).
func (s *UserService) toUserResponseDtoInternal(ctx context.Context, u models.User) user.User {
	dto := user.User{
		ID:                     u.ID,
		Username:               u.Username,
		DisplayName:            u.DisplayName,
		Email:                  u.Email,
		CanDelete:              true,
		OidcSubjectId:          u.OidcSubjectId,
		Locale:                 u.Locale,
		RequiresPasswordChange: u.RequiresPasswordChange,
		CreatedAt:              u.CreatedAt.Format("2006-01-02T15:04:05.999999Z"),
		UpdatedAt:              u.UpdatedAt.Format("2006-01-02T15:04:05.999999Z"),
		RoleAssignments:        []user.RoleAssignmentSummary{},
		PermissionsByEnv:       map[string][]string{},
	}
	if s.roleService == nil {
		return dto
	}
	if rows, err := s.roleService.ListUserAssignments(ctx, u.ID); err == nil {
		dto.RoleAssignments = make([]user.RoleAssignmentSummary, len(rows))
		for i, r := range rows {
			dto.RoleAssignments[i] = user.RoleAssignmentSummary{
				RoleID:        r.RoleID,
				EnvironmentID: r.EnvironmentID,
				Source:        r.Source,
			}
			// Last-admin guard: this user is non-deletable if they hold the
			// only global Admin assignment.
			if r.RoleID == authz.BuiltInRoleAdmin && r.EnvironmentID == nil {
				if remaining, cerr := s.roleService.CountGlobalAdminsExcludingUser(ctx, u.ID); cerr == nil && remaining == 0 {
					dto.CanDelete = false
				}
			}
		}
	}
	if ps, err := s.roleService.ResolvePermissions(ctx, &u); err == nil && ps != nil {
		dto.IsGlobalAdmin = ps.IsGlobalAdmin()
		dto.PermissionsByEnv = permissionSetToMap(ps)
	}
	return dto
}

// permissionSetToMap flattens a PermissionSet into the wire format consumed
// by the frontend: a map from environment ID (or "global") to a list of
// permission strings. Sudo callers expose "*" under "global" as a sentinel
// meaning "every permission".
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
		globals := make([]string, 0, len(ps.Global))
		for p := range ps.Global {
			globals = append(globals, p)
		}
		out["global"] = globals
	}
	for envID, perms := range ps.PerEnv {
		list := make([]string, 0, len(perms))
		for p := range perms {
			list = append(list, p)
		}
		out[envID] = list
	}
	return out
}

func (s *UserService) GetUser(ctx context.Context, userID string) (*models.User, error) {
	slog.Debug("GetUser called", "user_id", userID)
	return s.getUserInternal(ctx, userID, s.db.DB)
}

func (s *UserService) getUserInternal(ctx context.Context, userID string, tx *gorm.DB) (*models.User, error) {
	var user models.User
	err := tx.
		WithContext(ctx).
		Where("id = ?", userID).
		First(&user).
		Error
	return &user, err
}
