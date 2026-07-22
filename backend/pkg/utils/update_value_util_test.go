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

func TestApplyNullable(t *testing.T) {
	target := new("initial")

	assert.True(t, ApplyNullable(&target, mo.Some("new")))
	assert.Equal(t, "new", *target)
	assert.False(t, ApplyNullable(&target, mo.Some("new")))
	assert.True(t, ApplyNullable(&target, mo.None[string]()))
	assert.Nil(t, target)
	assert.False(t, ApplyNullable(&target, mo.None[string]()))
}
