package cops

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/coptest"
	"github.com/dgageot/rubocop-go/prog"
)

func TestModernizationDeduplicatesPhysicalFiles(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		cop  *prog.Func
		src  string
	}{
		{"shared wrapper", NewLintErrorsAsType(), errorsAsTypeFixture},
		{"slices clone", NewLintSlicesClone(), slicesCloneFixture},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			const directive = "//line /virtual/shared.go:1\n"
			offenses := coptest.RunProgram(t, tc.cop, coptest.ProgramFiles{
				"go.mod":                "module example.test\n\ngo 1.26\n",
				"first/source.go":       directive + tc.src,
				"first/source_test.go":  "package p\n",
				"second/source.go":      directive + tc.src,
				"second/source_test.go": "package p\n",
			})
			require.Len(t, offenses, 2, "both physical files must be reported, each only once despite test variants")
			for _, offense := range offenses {
				assert.Equal(t, "/virtual/shared.go", offense.Pos.Filename, "diagnostics still use logical positions")
			}
		})
	}
}
