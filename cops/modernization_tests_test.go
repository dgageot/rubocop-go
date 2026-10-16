package cops

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/coptest"
	"github.com/dgageot/rubocop-go/prog"
)

func TestModernizationPackageLoadPolicies(t *testing.T) {
	t.Parallel()
	check := prog.New(cop.Meta{Name: "test"}, func(p *prog.Pass) {
		_, err := loadModernizationTests(p)
		require.Error(t, err)
		pkgs, err := loadModernizationPackages(p)
		require.NoError(t, err)
		require.NotEmpty(t, pkgs)
		_, err = loadModernizationTests(p)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing")

		cached, err := loadModernizationPackages(p)
		require.NoError(t, err, "strict validation must not poison the shared load")
		require.Len(t, cached, len(pkgs))
		for i := range pkgs {
			assert.Same(t, pkgs[i], cached[i])
		}
	})
	coptest.RunProgram(t, check, coptest.ProgramFiles{
		"source.go":      "package p\n",
		"source_test.go": "package p\nvar _ = missing\n",
	})
}
