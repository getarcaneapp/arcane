package bitwarden

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"getarcane.dev/arcane/v3/pkg/bitwarden"
	"getarcane.dev/types/v3"
)

func strPtrInternal(v string) *string { return &v }

func TestItemsToValuesUsesPasswordOrNoteAndSkipsEmptyItems(t *testing.T) {
	values, skipped, err := ItemsToValues([]bitwarden.Item{
		{Name: "DB_PASSWORD", Type: bitwarden.ItemTypeLogin, Login: &bitwarden.Login{Password: strPtrInternal("pw")}},
		{Name: " TLS_NOTE ", Type: bitwarden.ItemTypeSecureNote, Notes: strPtrInternal("text")},
		{Name: "NO_PASSWORD", Type: bitwarden.ItemTypeLogin, Login: &bitwarden.Login{}},
		{Name: "A_CARD", Type: 3},
		{Name: "", Type: bitwarden.ItemTypeSecureNote, Notes: strPtrInternal("x")},
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"DB_PASSWORD": "pw", "TLS_NOTE": "text"}, values)
	assert.Equal(t, []string{"NO_PASSWORD", "A_CARD", "(unnamed item)"}, skipped)
}

func TestItemsToValuesRejectsDuplicateNames(t *testing.T) {
	_, _, err := ItemsToValues([]bitwarden.Item{
		{Name: "DB_PASSWORD", Type: bitwarden.ItemTypeLogin, Login: &bitwarden.Login{Password: strPtrInternal("a")}},
		{Name: "DB_PASSWORD", Type: bitwarden.ItemTypeLogin, Login: &bitwarden.Login{Password: strPtrInternal("b")}},
	})
	require.ErrorContains(t, err, "DB_PASSWORD")
}

func TestFieldsToValuesSkipsLinkedFieldsAndRejectsDuplicates(t *testing.T) {
	values, skipped, err := FieldsToValues([]bitwarden.Field{
		{Name: "API_KEY", Value: strPtrInternal("k"), Type: bitwarden.FieldTypeHidden},
		{Name: "DEBUG", Value: strPtrInternal("true"), Type: bitwarden.FieldTypeBoolean},
		{Name: "EMPTY", Type: bitwarden.FieldTypeText},
		{Name: "username", Type: bitwarden.FieldTypeLinked},
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"API_KEY": "k", "DEBUG": "true", "EMPTY": ""}, values)
	assert.Equal(t, []string{"username"}, skipped)

	_, _, err = FieldsToValues([]bitwarden.Field{
		{Name: "API_KEY", Value: strPtrInternal("a")},
		{Name: "API_KEY", Value: strPtrInternal("b")},
	})
	require.ErrorContains(t, err, "API_KEY")
}

func TestNormalizeTargetRequiresKnownScopeAndID(t *testing.T) {
	target, err := NormalizeTarget(&types.SecretSourceBitwardenTarget{Scope: " folder ", ID: " f1 ", Name: "billing"})
	require.NoError(t, err)
	assert.Equal(t, &types.SecretSourceBitwardenTarget{Scope: "folder", ID: "f1", Name: "billing"}, target)

	_, err = NormalizeTarget(&types.SecretSourceBitwardenTarget{Scope: "vault", ID: "x"})
	require.Error(t, err)
	_, err = NormalizeTarget(&types.SecretSourceBitwardenTarget{Scope: "item"})
	require.Error(t, err)
	_, err = NormalizeTarget(nil)
	require.Error(t, err)
}
