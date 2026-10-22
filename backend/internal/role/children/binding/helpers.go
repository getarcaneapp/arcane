package binding

import (
	"context"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"go.getarcane.app/kit/pkg"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
)

const (
	SubjectUser  = "user"
	SubjectGroup = "group"
	sourceManual = "manual"
	sourceEnv    = "env"
)

type RoleLookup struct {
	Role         func(ctx context.Context, roleID string) (types.Role, error)
	LockRole     func(ctx context.Context, tx *gorm.DB, roleID string) (types.Role, error)
	UserBindings func(ctx context.Context, userID string) ([]types.RoleBindingSummary, error)
}

type Guard struct {
	Lock        func(tx *gorm.DB) error
	AssertAdmin func(ctx context.Context, tx *gorm.DB) error
	Authorize   func(ctx context.Context, tx *gorm.DB, caller *authz.PermissionSet, roleID string, environmentID *string) error
	Invalidate  func(ctx context.Context, userID, groupID *string)
}

type bindingRow struct {
	ID              string    `gorm:"column:id"`
	UserID          *string   `gorm:"column:user_id"`
	GroupID         *string   `gorm:"column:group_id"`
	RoleID          string    `gorm:"column:role_id"`
	EnvironmentID   *string   `gorm:"column:environment_id"`
	Source          string    `gorm:"column:source"`
	CreatedBy       *string   `gorm:"column:created_by"`
	CreatedAt       time.Time `gorm:"column:created_at"`
	Username        *string   `gorm:"column:username"`
	GroupName       *string   `gorm:"column:group_name"`
	RoleName        *string   `gorm:"column:role_name"`
	EnvironmentName *string   `gorm:"column:environment_name"`
}

func (r bindingRow) toDTO() types.RoleBinding {
	builtIn, isBuiltIn := authz.BuiltInRole(r.RoleID)
	return types.RoleBinding{
		ID:              r.ID,
		SubjectType:     kit.Ternary(r.UserID != nil, SubjectUser, SubjectGroup),
		SubjectID:       kit.FromPtr(kit.Ternary(r.UserID != nil, r.UserID, r.GroupID)),
		SubjectName:     kit.FromPtr(kit.Ternary(r.UserID != nil, r.Username, r.GroupName)),
		RoleID:          r.RoleID,
		RoleName:        kit.Ternary(isBuiltIn, builtIn.Name, kit.FromPtr(r.RoleName)),
		EnvironmentID:   r.EnvironmentID,
		EnvironmentName: kit.FromPtr(r.EnvironmentName),
		Source:          r.Source,
		CreatedBy:       r.CreatedBy,
		CreatedAt:       r.CreatedAt,
	}
}
