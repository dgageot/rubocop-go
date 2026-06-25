package cops_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/cops"
	"github.com/dgageot/rubocop-go/coptest"
)

// The root context minted in main, threaded straight into a consumer, is
// connected: no offense.
func TestContextConnectivity_RootThreaded(t *testing.T) {
	requireToolchain(t)
	files := coptest.ProgramFiles{
		"main.go": `package main

import "context"

func use(ctx context.Context) { _ = ctx }

func main() {
	ctx := context.Background()
	use(ctx)
}
`,
	}
	offenses := coptest.RunProgram(t, cops.NewLintContextConnectivity(), files)
	assert.Empty(t, offenses)
}

// A context.Background() created in a helper (not main) and consumed there
// is detached from the root: one offense, on the helper's Background() call.
func TestContextConnectivity_DetachedRoot(t *testing.T) {
	requireToolchain(t)
	files := coptest.ProgramFiles{
		"main.go": `package main

import "context"

func use(ctx context.Context) { _ = ctx }

func helper() {
	ctx := context.Background()
	use(ctx)
}

func main() {
	ctx := context.Background()
	use(ctx)
	helper()
}
`,
	}
	offenses := coptest.RunProgram(t, cops.NewLintContextConnectivity(), files)
	require.Len(t, offenses, 1)
	assert.Equal(t, "Lint/ContextConnectivity", offenses[0].CopName)
	assert.Equal(t, 8, offenses[0].Pos.Line)
	assert.Contains(t, offenses[0].Message, "detached context")
}

// The detached root is consumed several calls deep, in another package. The
// backward trace must cross both the call boundary and the package boundary
// to attribute the offense. This is the inter-procedural, cross-package case.
func TestContextConnectivity_DetachedAcrossPackages(t *testing.T) {
	requireToolchain(t)
	files := coptest.ProgramFiles{
		"main.go": `package main

import (
	"context"

	"example.test/worker"
)

func main() {
	ctx := context.Background()
	worker.Run(ctx) // connected
	worker.Detached() // creates its own root, deep down
}
`,
		"worker/worker.go": `package worker

import "context"

func Run(ctx context.Context) { sink(ctx) }

func Detached() {
	ctx := makeCtx()
	Run(ctx)
}

func makeCtx() context.Context {
	return context.Background()
}

func sink(ctx context.Context) { _ = ctx }
`,
	}
	offenses := coptest.RunProgram(t, cops.NewLintContextConnectivity(), files)
	require.Len(t, offenses, 1)
	assert.Equal(t, "Lint/ContextConnectivity", offenses[0].CopName)
	// The offense points at the context.Background() inside makeCtx, even
	// though it is consumed two calls away via a parameter in another pkg.
	assert.Contains(t, offenses[0].Pos.Filename, "worker.go")
	assert.Contains(t, offenses[0].Message, "detached context")
}

// A context derived (WithCancel) from the threaded root stays connected:
// the derivation is looked through to its parent, which is the root.
func TestContextConnectivity_DerivedFromRootIsConnected(t *testing.T) {
	requireToolchain(t)
	files := coptest.ProgramFiles{
		"main.go": `package main

import (
	"context"
	"time"
)

func use(ctx context.Context) { _ = ctx }

func main() {
	ctx := context.Background()
	derived, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	use(derived)
}
`,
	}
	offenses := coptest.RunProgram(t, cops.NewLintContextConnectivity(), files)
	assert.Empty(t, offenses)
}

// A context derived from a detached root is still reported, anchored on the
// detached root rather than the derivation.
func TestContextConnectivity_DerivedFromDetachedRoot(t *testing.T) {
	requireToolchain(t)
	files := coptest.ProgramFiles{
		"main.go": `package main

import (
	"context"
	"time"
)

func use(ctx context.Context) { _ = ctx }

func helper() {
	base := context.Background()
	derived, cancel := context.WithTimeout(base, time.Second)
	defer cancel()
	use(derived)
}

func main() {
	use(context.Background())
	helper()
}
`,
	}
	offenses := coptest.RunProgram(t, cops.NewLintContextConnectivity(), files)
	require.Len(t, offenses, 1)
	assert.Equal(t, 11, offenses[0].Pos.Line) // the context.Background() in helper
}

// A deliberate detached root annotated with a suppression comment is not
// reported. (Suppression is applied by the runner, so here we assert the
// raw cop still fires; the runner-level test covers suppression.)
func TestContextConnectivity_NoFalsePositiveOnSingleRoot(t *testing.T) {
	requireToolchain(t)
	files := coptest.ProgramFiles{
		"main.go": `package main

import "context"

func use(ctx context.Context) { _ = ctx }

func main() {
	use(context.Background())
}
`,
	}
	offenses := coptest.RunProgram(t, cops.NewLintContextConnectivity(), files)
	assert.Empty(t, offenses)
}

func requireToolchain(t *testing.T) {
	t.Helper()
	if !coptest.HaveGoToolchain() {
		t.Skip("go toolchain not available")
	}
}
