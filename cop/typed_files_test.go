package cop_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/cop"
)

func TestTypedFilesProvider(t *testing.T) {
	t.Parallel()
	var pass cop.Pass
	views, err := pass.TypedFiles()
	require.EqualError(t, err, "typed file views unavailable")
	assert.Nil(t, views)

	calls := 0
	want := []*cop.Pass{{}}
	pass.SetTypedFiles(func() ([]*cop.Pass, error) {
		calls++
		return want, nil
	})
	assert.Zero(t, calls, "setting the provider must not call it")
	views, err = pass.TypedFiles()
	require.NoError(t, err)
	assert.Same(t, want[0], views[0])
	assert.Equal(t, 1, calls)

	failure := errors.New("load failed")
	pass.SetTypedFiles(func() ([]*cop.Pass, error) { return nil, failure })
	views, err = pass.TypedFiles()
	require.ErrorIs(t, err, failure)
	assert.Nil(t, views)

	pass.SetTypedFiles(func() ([]*cop.Pass, error) { return nil, nil })
	views, err = pass.TypedFiles()
	require.NoError(t, err)
	assert.Nil(t, views)

	pass.SetTypedFiles(nil)
	views, err = pass.TypedFiles()
	require.EqualError(t, err, "typed file views unavailable")
	assert.Nil(t, views)
}
