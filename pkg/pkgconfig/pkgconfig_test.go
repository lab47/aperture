package pkgconfig

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadAll(t *testing.T) {
	configs, err := LoadAll("testdata")
	require.NoError(t, err)

	require.Len(t, configs, 1)

	cfg := configs[0]

	assert.Equal(t, "xau", cfg.Id)
	assert.Equal(t, []string{"xproto"}, cfg.Requires)
	assert.Equal(t, "-I/this/is/a/prefix/include", cfg.Cflags)
	assert.Equal(t, "-L/this/is/a/prefix/lib -lXau", cfg.Libs)
}
