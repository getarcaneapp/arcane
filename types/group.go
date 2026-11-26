package types

import "time"

type (
	Group struct {
		CreatedAt           time.Time                 `json:"createdAt" readOnly:"true" doc:"Creation timestamp"`
		Description         *string                   `json:"description,omitempty" maxLength:"500" doc:"Optional human description" unorm:"nfc" trim:"true"`
		UpdatedAt           *time.Time                `json:"updatedAt,omitempty" readOnly:"true" doc:"Last update timestamp"`
		ID                  string                    `json:"id" readOnly:"true" doc:"Unique identifier of the group"`
		Name                string                    `json:"name" required:"false" minLength:"1" maxLength:"100" doc:"Display name of the group" example:"Platform Team" unorm:"nfc" trim:"true"`
		MemberUserIDs       []string                  `json:"memberUserIds,omitempty" writeOnly:"true" doc:"Optional manual member user IDs to set atomically"`
		RoleAssignments     []UserRoleAssignmentInput `json:"roleAssignments,omitempty" writeOnly:"true" doc:"Optional manual role assignments to set atomically"`
		MemberCount         int                       `json:"memberCount" readOnly:"true" doc:"Number of users in this group"`
		RoleAssignmentCount int                       `json:"roleAssignmentCount" readOnly:"true" doc:"Number of role assignments granted to this group"`
	}

	GroupMember struct {
		CreatedAt   time.Time `json:"createdAt" doc:"Membership creation timestamp"`
		DisplayName *string   `json:"displayName,omitempty" doc:"Optional display name of the user"`
		Email       *string   `json:"email,omitempty" doc:"Optional email address of the user"`
		ID          string    `json:"id" doc:"Unique identifier of the membership"`
		GroupID     string    `json:"groupId" doc:"ID of the group"`
		UserID      string    `json:"userId" doc:"ID of the member user"`
		Username    string    `json:"username" doc:"Username of the member user"`
		Source      string    `json:"source" doc:"How the membership was created" enum:"manual,oidc,ldap"`
	}

	GroupRoleAssignment struct {
		CreatedAt     time.Time `json:"createdAt" doc:"Creation timestamp"`
		EnvironmentID *string   `json:"environmentId,omitempty" doc:"Environment ID this assignment is scoped to; omit for a global assignment"`
		ID            string    `json:"id" doc:"Unique identifier of the assignment"`
		GroupID       string    `json:"groupId" doc:"ID of the group holding this assignment"`
		RoleID        string    `json:"roleId" doc:"ID of the granted role"`
		Source        string    `json:"source" doc:"How the assignment was created" enum:"manual,oidc,ldap"`
	}

	SetGroupMembers struct {
		UserIDs []string `json:"userIds" doc:"Desired manual member user IDs for the group"`
	}

	SetGroupRoleAssignments struct {
		Assignments []UserRoleAssignmentInput `json:"assignments" doc:"Desired manual role assignments for the group"`
	}
)
