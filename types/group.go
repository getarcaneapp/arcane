package types

import "time"

type Group struct {
	ID           string     `json:"id" readOnly:"true" doc:"Unique identifier of the group"`
	Name         string     `json:"name" doc:"Unique group name"`
	Description  *string    `json:"description,omitempty" doc:"Optional group description"`
	MemberCount  int        `json:"memberCount" readOnly:"true" doc:"Number of users in the group"`
	BindingCount int        `json:"bindingCount" readOnly:"true" doc:"Number of role bindings on the group"`
	CreatedAt    time.Time  `json:"createdAt" readOnly:"true" doc:"Creation timestamp"`
	UpdatedAt    *time.Time `json:"updatedAt,omitempty" readOnly:"true" doc:"Last update timestamp"`
}

type GroupRef struct {
	ID     string `json:"id" doc:"Group ID"`
	Name   string `json:"name" doc:"Group name"`
	Source string `json:"source" enum:"manual,oidc" doc:"How the membership was created"`
}

type GroupMember struct {
	UserID      string    `json:"userId" doc:"ID of the member"`
	Username    string    `json:"username" doc:"Username of the member"`
	DisplayName *string   `json:"displayName,omitempty" doc:"Display name of the member"`
	Source      string    `json:"source" enum:"manual,oidc" doc:"How the membership was created"`
	CreatedAt   time.Time `json:"createdAt" doc:"When the membership was created"`
}

type CreateGroup struct {
	Name        string  `json:"name" minLength:"1" maxLength:"100" doc:"Unique group name" unorm:"nfc" trim:"true"`
	Description *string `json:"description,omitempty" maxLength:"500" doc:"Optional group description" unorm:"nfc" trim:"true"`
}

type UpdateGroup struct {
	Name        *string `json:"name,omitempty" minLength:"1" maxLength:"100" doc:"New group name" unorm:"nfc" trim:"true"`
	Description *string `json:"description,omitempty" maxLength:"500" doc:"New group description" unorm:"nfc" trim:"true"`
}

type AddGroupMembers struct {
	UserIDs []string `json:"userIds" minItems:"1" doc:"IDs of the users to add"`
}

type OidcGroupMapping struct {
	ID         string     `json:"id" readOnly:"true" doc:"Unique identifier of the mapping"`
	ClaimValue string     `json:"claimValue" doc:"OIDC claim value that grants membership" example:"docker-admins"`
	GroupID    string     `json:"groupId" doc:"Group the claim value maps to"`
	GroupName  string     `json:"groupName" doc:"Name of the mapped group"`
	Source     string     `json:"source" enum:"manual,env" doc:"Origin: manual rows are editable; env rows come from ARCANE_OIDC_GROUP_MAPPINGS and are read-only"`
	CreatedAt  time.Time  `json:"createdAt" readOnly:"true" doc:"Creation timestamp"`
	UpdatedAt  *time.Time `json:"updatedAt,omitempty" readOnly:"true" doc:"Last update timestamp"`
}

type OidcGroupMappingSpec struct {
	ClaimValue string `json:"claimValue" minLength:"1" doc:"OIDC claim value to match" example:"docker-admins"`
	GroupID    string `json:"groupId" minLength:"1" doc:"Group to grant membership in when the claim matches"`
}
