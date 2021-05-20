package sumfile

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/mr-tron/base58"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSumfile(t *testing.T) {
	t.Run("adds entries", func(t *testing.T) {
		var sf Sumfile

		sf.Add("ab", "x", []byte{1, 2, 3})
		sf.Add("b", "x", []byte{4, 5, 6})

		algo, data, ok := sf.Lookup("ab")
		require.True(t, ok)

		assert.Equal(t, "x", algo)
		assert.Equal(t, []byte{1, 2, 3}, data)

		algo, data, ok = sf.Lookup("b")
		require.True(t, ok)

		assert.Equal(t, "x", algo)
		assert.Equal(t, []byte{4, 5, 6}, data)

		_, data, ok = sf.Lookup("c")
		require.False(t, ok)

		_, data, ok = sf.Lookup("a")
		require.False(t, ok)
	})

	t.Run("loads entites", func(t *testing.T) {
		var buf bytes.Buffer

		fmt.Fprintf(&buf, "x:%s a\n", base58.Encode([]byte{1, 2, 3}))
		fmt.Fprintf(&buf, "x:%s b\n", base58.Encode([]byte{4, 5, 6}))

		var sf Sumfile

		err := sf.Load(bytes.NewReader(buf.Bytes()))
		require.NoError(t, err)

		require.Equal(t, 2, len(sf.entities))

		he := sf.entities[0]

		assert.Equal(t, "a", he.entity)
		assert.Equal(t, "x", he.algo)
		assert.Equal(t, []byte{1, 2, 3}, he.hash)

		he = sf.entities[1]

		assert.Equal(t, "b", he.entity)
		assert.Equal(t, "x", he.algo)
		assert.Equal(t, []byte{4, 5, 6}, he.hash)
	})

	t.Run("saves entries", func(t *testing.T) {
		var sf Sumfile

		sf.Add("a", "x", []byte{1, 2, 3})
		sf.Add("b", "x", []byte{4, 5, 6})

		var buf bytes.Buffer

		err := sf.Save(&buf)
		require.NoError(t, err)

		expected := fmt.Sprintf("x:%s a\nx:%s b\n",
			base58.Encode([]byte{1, 2, 3}),
			base58.Encode([]byte{4, 5, 6}),
		)

		assert.Equal(t, expected, buf.String())
	})

}
