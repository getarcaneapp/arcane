package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUpdateIfChanged(t *testing.T) {
	target := "initial"
	assert.True(t, UpdateIfChanged(&target, "new"))
	assert.Equal(t, "new", target)

	assert.False(t, UpdateIfChanged(&target, "new"))
	assert.Equal(t, "new", target)

	count := 10
	assert.True(t, UpdateIfChanged(&count, 20))
	assert.Equal(t, 20, count)
}

func TestUpdateIfChangedPtr(t *testing.T) {
	target := "initial"
	newValue := "new"
	assert.True(t, UpdateIfChangedPtr(&target, &newValue))
	assert.Equal(t, "new", target)

	assert.False(t, UpdateIfChangedPtr(&target, &newValue))

	assert.False(t, UpdateIfChangedPtr(&target, nil))
	assert.Equal(t, "new", target)
}

func TestUpdatePtrIfChanged(t *testing.T) {
	target := new("initial")
	newValue := "new"

	assert.True(t, UpdatePtrIfChanged(&target, &newValue))
	assert.Equal(t, "new", *target)

	assert.False(t, UpdatePtrIfChanged(&target, &newValue))

	assert.True(t, UpdatePtrIfChanged(&target, nil))
	assert.Nil(t, target)

	assert.True(t, UpdatePtrIfChanged(&target, &newValue))
	assert.Equal(t, "new", *target)
}
