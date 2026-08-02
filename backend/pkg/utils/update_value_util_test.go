package utils

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
)

func TestApplyChanged(t *testing.T) {
	target := "initial"

	assert.True(t, ApplyChanged(&target, mo.Some("new")))
	assert.Equal(t, "new", target)
	assert.False(t, ApplyChanged(&target, mo.Some("new")))
	assert.False(t, ApplyChanged(&target, mo.None[string]()))
	assert.Equal(t, "new", target)
}

func TestApplySliceChanged(t *testing.T) {
	target := []string{"a"}

	assert.True(t, ApplySliceChanged(&target, mo.Some([]string{"a", "b"})))
	assert.Equal(t, []string{"a", "b"}, target)
	assert.False(t, ApplySliceChanged(&target, mo.Some([]string{"a", "b"})))
	assert.False(t, ApplySliceChanged(&target, mo.None[[]string]()))
	assert.True(t, ApplySliceChanged(&target, mo.Some([]string{})))
	assert.False(t, ApplySliceChanged(&target, mo.Some[[]string](nil)))
}

func TestApplyNullable(t *testing.T) {
	target := new("initial")

	assert.True(t, ApplyNullable(&target, mo.Some("new")))
	assert.Equal(t, "new", *target)
	assert.False(t, ApplyNullable(&target, mo.Some("new")))
	assert.True(t, ApplyNullable(&target, mo.None[string]()))
	assert.Nil(t, target)
	assert.False(t, ApplyNullable(&target, mo.None[string]()))
}
