package config

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/getlantern/flashlight/v7/embeddedconfig"
)

func TestEmbeddedGlobal(t *testing.T) {

	globalFunc := newGlobalUnmarshaler(make(map[string]interface{}))

	global, err := globalFunc(embeddedconfig.Global)
	assert.NoError(t, err)

	gl := global.(*Global)
	assert.NotNil(t, gl.Client)
}
