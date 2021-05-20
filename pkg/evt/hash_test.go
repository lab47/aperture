package evt

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHash(t *testing.T) {
	assertDiff := func(t *testing.T, a, b interface{}) {
		h1, err := Hash(a)
		require.NoError(t, err)

		h2, err := Hash(b)
		require.NoError(t, err)

		assert.NotEqual(t, h1, h2)
	}

	assertSame := func(t *testing.T, a, b interface{}) {
		h1, err := Hash(a)
		require.NoError(t, err)

		h2, err := Hash(b)
		require.NoError(t, err)

		assert.Equal(t, h1, h2)
	}

	t.Run("bottom values", func(t *testing.T) {
		assertSame(t, "foo", "foo")
		assertSame(t, 1, 1)
		assertSame(t, true, true)

		assertDiff(t, "foo", "bar")
		assertDiff(t, 1, 2)
		assertDiff(t, true, false)

		assertDiff(t, "foo", 1)
		assertDiff(t, 1, "bar")
		assertDiff(t, true, "qux")
	})

	t.Run("ints of different sizes are the same", func(t *testing.T) {
		assertSame(t, uint16(1), uint32(1))
	})

	t.Run("map is different than slice", func(t *testing.T) {
		assertDiff(t, map[string]string{"foo": "bar"}, []string{"foo", "bar"})
	})

	t.Run("structs aren't invisible", func(t *testing.T) {
		type a struct {
			_    struct{} `hash:"foo"`
			Name string
		}

		assertDiff(t, a{Name: "bar"}, "bar")
	})

	t.Run("struct fields are a set", func(t *testing.T) {
		type a struct {
			_    struct{} `hash:"foo"`
			Name string
			Age  int
		}

		type b struct {
			_    struct{} `hash:"foo"`
			Age  int
			Name string
		}

		assertSame(t, a{Name: "bar"}, b{Name: "bar"})
		assertSame(t, a{Name: "bar", Age: 12}, b{Name: "bar", Age: 12})

		assertDiff(t, a{Name: "bar", Age: 12}, b{Name: "foo", Age: 12})
		assertDiff(t, a{Name: "bar", Age: 12}, b{Name: "bar", Age: 120})
	})

	t.Run("maps are a set", func(t *testing.T) {
		type a map[string]interface{}

		assertSame(t, a{"name": "bar"}, a{"name": "bar"})

		for i := 0; i < 100; i++ {
			assertSame(t, a{"name": "bar", "age": 12}, a{"name": "bar", "age": 12})
		}

		assertDiff(t, a{"name": "bar", "age": 12}, a{"name": "foo", "age": 12})
		assertDiff(t, a{"name": "bar", "age": 12}, a{"name": "bar", "age": 120})
	})

	t.Run("slices are not a set", func(t *testing.T) {
		assertSame(t, []string{"foo", "bar"}, []string{"foo", "bar"})
		assertDiff(t, []string{"foo", "bar"}, []string{"bar", "foo"})
	})
}
