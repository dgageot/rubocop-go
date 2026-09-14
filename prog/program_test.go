package prog_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadBuildsInitialPackagesOnly(t *testing.T) {
	p := loadProgram(t, `package main
import "context"
func identity[T any](x T) T { return x }
func use(context.Context) {}
func main() { use(identity(context.Background())) }
`)
	require.False(t, p.HasErrors())
	require.Len(t, p.SSAPackages, 1)
	main := p.SSAPackages[0].Func("main")
	require.NotEmpty(t, main.Blocks)
	require.NotNil(t, p.CallGraph.Nodes[main])

	var external, generic bool
	for _, edge := range p.CallGraph.Nodes[main].Out {
		fn := edge.Callee.Func
		if fn.Name() == "Background" {
			external = true
			assert.Empty(t, fn.Blocks)
		}
		if strings.HasPrefix(fn.Name(), "identity[") {
			generic = true
			assert.NotEmpty(t, fn.Blocks)
		}
	}
	assert.True(t, external)
	assert.True(t, generic)
}
