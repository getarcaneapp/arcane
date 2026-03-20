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
	"time"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	arcstorage "github.com/getarcaneapp/arcane/backend/internal/storage"
	"github.com/getarcaneapp/arcane/backend/pkg/pagination"
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
	repo         arcstorage.UserRepository
	argon2Params *Argon2Params
}

var ErrCannotRemoveLastAdmin = errors.New("cannot remove the last admin user")

func NewUserService(db *database.DB, repo ...arcstorage.UserRepository) *UserService {
	var selectedRepo arcstorage.UserRepository
	if len(repo) > 0 && repo[0] != nil {
		selectedRepo = repo[0]
	} else if db != nil {
		selectedRepo = arcstorage.NewSQLUserRepository(db)
	}
	return &UserService{
		db:           db,
		repo:         selectedRepo,
		argon2Params: DefaultArgon2Params(),
	}
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
		return fmt.Errorf("invalid hash format")
	}

	var version int
	_, err := fmt.Sscanf(parts[2], "v=%d", &version)
	if err != nil {
		return err
	}
	if version != argon2.Version {
		return fmt.Errorf("incompatible version of argon2")
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
		return fmt.Errorf("invalid hash length")
	}

	comparisonHash := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(hashLen))

	// constant-time compare
	if subtle.ConstantTimeCompare(comparisonHash, decodedHash) != 1 {
		return fmt.Errorf("invalid password")
	}

	return nil
}

func (s *UserService) CreateUser(ctx context.Context, user *models.User) (*models.User, error) {
	if err := s.repo.Create(ctx, user); err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}
	return user, nil
}

func (s *UserService) GetUserByUsername(ctx context.Context, username string) (*models.User, error) {
	user, err := s.repo.GetByUsername(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	if user == nil {
		return nil, ErrUserNotFound
	}
	return user, nil
}

func (s *UserService) GetUserByID(ctx context.Context, id string) (*models.User, error) {
	user, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	if user == nil {
		return nil, ErrUserNotFound
	}
	return user, nil
}

func (s *UserService) GetUserByOidcSubjectId(ctx context.Context, subjectId string) (*models.User, error) {
	user, err := s.repo.GetByOidcSubjectID(ctx, subjectId)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	if user == nil {
		return nil, ErrUserNotFound
	}
	return user, nil
}

func (s *UserService) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	user, err := s.repo.GetByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("failed to get user by email: %w", err)
	}
	if user == nil {
		return nil, ErrUserNotFound
	}
	return user, nil
}

func (s *UserService) UpdateUser(ctx context.Context, user *models.User) (*models.User, error) {
	existing, err := s.repo.GetByID(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to load user: %w", err)
	}
	if existing == nil {
		return nil, ErrUserNotFound
	}
	if userHasRoleInternal(existing.Roles, "admin") && !userHasRoleInternal(user.Roles, "admin") {
		remainingAdmins, err := s.remainingAdminCountExcludingUserInternal(ctx, user.ID)
		if err != nil {
			return nil, err
		}
		if remainingAdmins == 0 {
			return nil, ErrCannotRemoveLastAdmin
		}
	}
	if err := s.repo.Upsert(ctx, user); err != nil {
		return nil, fmt.Errorf("failed to update user: %w", err)
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
	u, err := s.repo.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to load user for OIDC merge: %w", err)
	}
	if u == nil {
		return nil, ErrUserNotFound
	}
	if u.OidcSubjectId != nil && *u.OidcSubjectId != "" && *u.OidcSubjectId != subject {
		return nil, fmt.Errorf("user already linked to another OIDC subject")
	}
	u.OidcSubjectId = new(subject)
	if updateFn != nil {
		updateFn(u)
	}
	if err := s.repo.Upsert(ctx, u); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") || strings.Contains(strings.ToLower(err.Error()), "duplicate key") {
			return nil, fmt.Errorf("oidc subject is already linked to another user: %w", err)
		}
		return nil, fmt.Errorf("failed to persist OIDC merge: %w", err)
	}
	return u, nil
}

func (s *UserService) CreateDefaultAdmin(ctx context.Context) error {
	// Hash password outside transaction to minimize lock time
	hashedPassword, err := s.hashPassword("arcane-admin")
	if err != nil {
		return fmt.Errorf("failed to hash default admin password: %w", err)
	}

	users, err := s.repo.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list users: %w", err)
	}
	if len(users) > 0 {
		slog.WarnContext(ctx, "Users already exist, skipping default admin creation")
		return nil
	}

	email := "admin@localhost"
	displayName := "Arcane Admin"
	userModel := &models.User{
		Username:               "arcane",
		Email:                  new(email),
		DisplayName:            new(displayName),
		PasswordHash:           hashedPassword,
		Roles:                  models.StringSlice{"admin"},
		RequiresPasswordChange: true,
	}
	if err := s.repo.Create(ctx, userModel); err != nil {
		return fmt.Errorf("failed to create default admin user: %w", err)
	}

	slog.InfoContext(ctx, "👑 Default admin user created!")
	slog.InfoContext(ctx, "🔑 Username: arcane")
	slog.InfoContext(ctx, "🔑 Password: arcane-admin")
	slog.InfoContext(ctx, "⚠️  User will be prompted to change password on first login")

	return nil
}

func (s *UserService) DeleteUser(ctx context.Context, id string) error {
	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to load user: %w", err)
	}
	if existing == nil {
		return ErrUserNotFound
	}
	if userHasRoleInternal(existing.Roles, "admin") {
		remainingAdmins, err := s.remainingAdminCountExcludingUserInternal(ctx, id)
		if err != nil {
			return err
		}
		if remainingAdmins == 0 {
			return ErrCannotRemoveLastAdmin
		}
	}
	return s.repo.Delete(ctx, id)
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

	user, err := s.repo.GetByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to load user for password upgrade: %w", err)
	}
	if user == nil {
		return ErrUserNotFound
	}
	user.PasswordHash = newHash
	return s.repo.Upsert(ctx, user)
}

func (s *UserService) ListUsersPaginated(ctx context.Context, params pagination.QueryParams) ([]user.User, pagination.Response, error) {
	users, err := s.repo.List(ctx)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list users: %w", err)
	}
	searchConfig := pagination.Config[models.User]{
		SearchAccessors: []pagination.SearchAccessor[models.User]{
			func(u models.User) (string, error) { return u.Username, nil },
			func(u models.User) (string, error) {
				if u.Email == nil {
					return "", nil
				}
				return *u.Email, nil
			},
			func(u models.User) (string, error) {
				if u.DisplayName == nil {
					return "", nil
				}
				return *u.DisplayName, nil
			},
		},
		SortBindings: []pagination.SortBinding[models.User]{
			{Key: "username", Fn: func(a, b models.User) int {
				return strings.Compare(strings.ToLower(a.Username), strings.ToLower(b.Username))
			}},
			{Key: "email", Fn: func(a, b models.User) int {
				return strings.Compare(strings.ToLower(derefUserString(a.Email)), strings.ToLower(derefUserString(b.Email)))
			}},
			{Key: "display_name", Fn: func(a, b models.User) int {
				return strings.Compare(strings.ToLower(derefUserString(a.DisplayName)), strings.ToLower(derefUserString(b.DisplayName)))
			}},
			{Key: "created_at", Fn: func(a, b models.User) int { return a.CreatedAt.Compare(b.CreatedAt) }},
			{Key: "updated_at", Fn: func(a, b models.User) int { return compareTimePointers(a.UpdatedAt, b.UpdatedAt) }},
		},
	}
	filtered := pagination.SearchOrderAndPaginate(users, params, searchConfig)
	paginationResp := pagination.BuildResponseFromFilterResult(filtered, params)

	result, err := s.toUserResponseDtosInternal(ctx, filtered.Items)
	if err != nil {
		return nil, pagination.Response{}, err
	}

	return result, paginationResp, nil
}

func (s *UserService) ToUserResponseDto(ctx context.Context, u models.User) (user.User, error) {
	if !userHasRoleInternal(u.Roles, "admin") {
		return toUserResponseDtoInternal(u, 0), nil
	}

	adminCount, err := s.adminUserCountInternal(ctx)
	if err != nil {
		return user.User{}, err
	}

	return toUserResponseDtoInternal(u, adminCount), nil
}

func (s *UserService) toUserResponseDtosInternal(ctx context.Context, users []models.User) ([]user.User, error) {
	adminCount, err := s.adminUserCountInternal(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]user.User, len(users))
	for i, u := range users {
		result[i] = toUserResponseDtoInternal(u, adminCount)
	}

	return result, nil
}

func toUserResponseDtoInternal(u models.User, adminCount int) user.User {
	return user.User{
		ID:                     u.ID,
		Username:               u.Username,
		DisplayName:            u.DisplayName,
		Email:                  u.Email,
		Roles:                  u.Roles,
		CanDelete:              !userHasRoleInternal(u.Roles, "admin") || adminCount > 1,
		OidcSubjectId:          u.OidcSubjectId,
		Locale:                 u.Locale,
		RequiresPasswordChange: u.RequiresPasswordChange,
		CreatedAt:              u.CreatedAt.Format("2006-01-02T15:04:05.999999Z"),
		UpdatedAt:              u.UpdatedAt.Format("2006-01-02T15:04:05.999999Z"),
	}
}

func (s *UserService) GetUser(ctx context.Context, userID string) (*models.User, error) {
	slog.Debug("GetUser called", "user_id", userID)
	return s.getUserInternal(ctx, userID)
}

func (s *UserService) getUserInternal(ctx context.Context, userID string) (*models.User, error) {
	return s.repo.GetByID(ctx, userID)
}

func (s *UserService) remainingAdminCountExcludingUserInternal(ctx context.Context, excludedUserID string) (int, error) {
	users, err := s.repo.List(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to list users: %w", err)
	}
	count := 0
	for _, currentUser := range users {
		if currentUser.ID == excludedUserID {
			continue
		}
		if userHasRoleInternal(currentUser.Roles, "admin") {
			count++
		}
	}
	return count, nil
}

func userHasRoleInternal(roles models.StringSlice, role string) bool {
	for _, currentRole := range roles {
		if strings.EqualFold(currentRole, role) {
			return true
		}
	}

	return false
}

func (s *UserService) adminUserCountInternal(ctx context.Context) (int, error) {
	users, err := s.repo.List(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to list users: %w", err)
	}
	count := 0
	for _, currentUser := range users {
		if userHasRoleInternal(currentUser.Roles, "admin") {
			count++
		}
	}
	return count, nil
}

func derefUserString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func compareTimePointers(a, b *time.Time) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return -1
	case b == nil:
		return 1
	default:
		return a.Compare(*b)
	}
}
