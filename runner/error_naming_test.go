package runner_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/config"
	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/cops"
	"github.com/dgageot/rubocop-go/runner"
)

func TestRunnerErrorNamingImportedResults(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := `package sample
import (
	"context"
	"strconv"
	"testing"
	"time"
)
func test(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_ = ctx
	_, e := strconv.Atoi("1")
	_ = e
}
`
	path := filepath.Join(dir, "sample.go")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))
	var out bytes.Buffer
	r := runner.New([]cop.Cop{cops.NewStyleErrorNaming()}, config.DefaultConfig(), &out)
	count, err := r.Run([]string{dir})
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	assert.Contains(t, out.String(), "error variable 'e'")
	assert.NotContains(t, out.String(), "error variable 'cancel'")
}
