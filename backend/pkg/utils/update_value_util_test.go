package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUpdateIfChanged(t *testing.T) {
	t.Run("string target with string value", func(t *testing.T) {
		target := "initial"
		updated := UpdateIfChanged(&target, "new")
		assert.True(t, updated)
		assert.Equal(t, "new", target)

		updated = UpdateIfChanged(&target, "new")
		assert.False(t, updated)
		assert.Equal(t, "new", target)
	})

	t.Run("string target with *string value", func(t *testing.T) {
		target := "initial"
		newValue := "new"
		updated := UpdateIfChanged(&target, &newValue)
		assert.True(t, updated)
		assert.Equal(t, "new", target)

		updated = UpdateIfChanged(&target, &newValue)
		assert.False(t, updated)
		assert.Equal(t, "new", target)

		var nilValue *string
		updated = UpdateIfChanged(&target, nilValue)
		assert.False(t, updated)
		assert.Equal(t, "new", target)
	})

	t.Run("bool target with bool value", func(t *testing.T) {
		target := false
		updated := UpdateIfChanged(&target, true)
		assert.True(t, updated)
		assert.True(t, target)

		updated = UpdateIfChanged(&target, true)
		assert.False(t, updated)
		assert.True(t, target)
	})

	t.Run("bool target with *bool value", func(t *testing.T) {
		target := false
		newValue := true
		updated := UpdateIfChanged(&target, &newValue)
		assert.True(t, updated)
		assert.True(t, target)

		updated = UpdateIfChanged(&target, &newValue)
		assert.False(t, updated)
		assert.True(t, target)

		var nilValue *bool
		updated = UpdateIfChanged(&target, nilValue)
		assert.False(t, updated)
		assert.True(t, target)
	})

	t.Run("*string target with *string value", func(t *testing.T) {
		initial := "initial"
		target := &initial
		newValue := "new"

		// Update from value to new value
		updated := UpdateIfChanged(&target, &newValue)
		assert.True(t, updated)
		assert.Equal(t, "new", *target)

		// Update with same value
		updated = UpdateIfChanged(&target, &newValue)
		assert.False(t, updated)
		assert.Equal(t, "new", *target)

		// Update to nil
		var nilValue *string
		updated = UpdateIfChanged(&target, nilValue)
		assert.True(t, updated)
		assert.Nil(t, target)

		// Update from nil to value
		updated = UpdateIfChanged(&target, &newValue)
		assert.True(t, updated)
		assert.Equal(t, "new", *target)
	})

	t.Run("unsupported types", func(t *testing.T) {
		target := 10
		updated := UpdateIfChanged(&target, 20)
		assert.False(t, updated)
		assert.Equal(t, 10, target)
	})
}
