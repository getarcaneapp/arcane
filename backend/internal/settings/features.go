package settings

import (
	"context"
	"strconv"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/types/v2/features"
)

// IsFeatureEnabled resolves feature availability on this environment.
func (s *SettingsService) IsFeatureEnabled(ctx context.Context, id features.ID) bool {
	definition, ok := features.Lookup(id)
	if !ok {
		return false
	}
	if s == nil {
		return definition.DefaultEnabled
	}
	cfg := s.GetSettingsOrDefaults(ctx)
	value, _, _, err := cfg.FieldByKey(definition.SettingKey)
	if err != nil || value == "" {
		return definition.DefaultEnabled
	}
	enabled, err := strconv.ParseBool(value)
	if err != nil {
		return definition.DefaultEnabled
	}
	return enabled
}

// RequireFeature rejects operations when their runtime feature is disabled.
func (s *SettingsService) RequireFeature(ctx context.Context, id features.ID) error {
	if s.IsFeatureEnabled(ctx, id) {
		return nil
	}
	return errors.WrapIff(common.ErrFeatureDisabled, "feature %s is disabled", id)
}

func validateFeatureSettingInternal(key, value string) error {
	for _, definition := range features.All() {
		if definition.SettingKey == key && value != "true" && value != "false" {
			return common.Classify(common.ErrValidation, errors.Errorf("%s must be true or false", key))
		}
	}
	return nil
}
