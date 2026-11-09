package runner_test

import (
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/ssa"

	"github.com/dgageot/rubocop-go/config"
	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
	"github.com/dgageot/rubocop-go/runner"
)

// exclusionModule has a package whose functions call a helper declared in
// another file, and two gen directories at different depths.
var exclusionModule = map[string]string{
	"go.mod":         "module example.test\n\ngo 1.26\n",
	"p.go":           "package p\n\nfunc f() { helper() }\n",
	"helper.go":      "package p\n\nfunc helper() {}\n\nfunc g() { helper() }\n",
	"gen/gen.go":     "package gen\n\nfunc h() {}\n",
	"sub/gen/gen.go": "package gen\n\nfunc h() {}\n",
}

// recordingReporter records the reporter events of one run.
type recordingReporter struct {
	numCops   int
	files     map[string][]cop.Offense
	finished  bool
	offenses  []cop.Offense
	inspected int
}

func (r *recordingReporter) Start(numCops int) { r.numCops = numCops }

func (r *recordingReporter) FileFinished(filename string, offenses []cop.Offense) {
	if r.files == nil {
		r.files = make(map[string][]cop.Offense)
	}
	r.files[filepath.ToSlash(filename)] = offenses
}

func (r *recordingReporter) Finish(allOffenses []cop.Offense, filesInspected int) {
	r.finished, r.offenses, r.inspected = true, allOffenses, filesInspected
}

// run runs r with a recording reporter and checks the reported count.
func run(t *testing.T, r *runner.Runner, paths ...string) *recordingReporter {
	t.Helper()
	rec := &recordingReporter{}
	r.Reporter = rec
	count, err := r.Run(paths)
	require.NoError(t, err)
	require.True(t, rec.finished)
	assert.Len(t, rec.offenses, count)
	return rec
}

// findings formats offenses as "<cop> <severity> <path>:<line>", with the path
// relative to root, and sorts them. An offense without a filename has the
// path "-".
func findings(t *testing.T, root string, offenses []cop.Offense) []string {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	var out []string
	for _, o := range offenses {
		name := "-"
		if o.Pos.Filename != "" {
			abs := o.Pos.Filename
			if !filepath.IsAbs(abs) {
				abs = filepath.Join(wd, abs)
			}
			rel, err := filepath.Rel(root, abs)
			require.NoError(t, err)
			name = filepath.ToSlash(rel)
		}
		out = append(out, o.CopName+" "+o.Severity.String()+" "+name+":"+strconv.Itoa(o.Pos.Line))
	}
	slices.Sort(out)
	return out
}

func writeExclusionConfig(t *testing.T, dir, content string) *config.Config {
	t.Helper()
	path := filepath.Join(dir, ".rubocop-go.yml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	cfg, err := config.Load(path)
	require.NoError(t, err)
	return cfg
}

func newModule(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	writeModule(t, dir, files)
	return dir
}

func newClauseCop(name string, opts ...cop.FuncOption) *cop.Func {
	return cop.New(cop.Meta{Name: name, Severity: cop.Warning},
		func(p *cop.Pass) { p.Report(p.File.Name, "package clause") }, opts...)
}

// newResolvedCallCop reports calls whose function the type checker resolves.
// A call to a function declared in another file resolves only when the type
// checker also receives that file.
func newResolvedCallCop(name string) *cop.Func {
	return cop.New(cop.Meta{Name: name, Severity: cop.Warning}, func(p *cop.Pass) {
		p.ForEachCall(func(call *ast.CallExpr) {
			if id, ok := call.Fun.(*ast.Ident); ok {
				if _, ok := p.Info.Uses[id].(*types.Func); ok {
					p.Reportf(call, "resolved call to %s", id.Name)
				}
			}
		})
	}, cop.WithTypes())
}

// newHelperCallsCop reports each static call to a function named helper. SSA
// resolves the callee only when the program contains its declaration.
func newHelperCallsCop(name string) prog.Cop {
	return prog.New(cop.Meta{Name: name, Severity: cop.Warning}, func(p *prog.Pass) {
		for _, fn := range p.Program.AllFunctions() {
			for _, block := range fn.Blocks {
				for _, instr := range block.Instrs {
					call, ok := instr.(ssa.CallInstruction)
					if !ok {
						continue
					}
					if callee := call.Common().StaticCallee(); callee != nil && callee.Name() == "helper" {
						p.Reportf(call.Pos(), "calls helper")
					}
				}
			}
		}
	})
}

// newFunctionsCop reports every source function, and one offense without a
// position.
func newFunctionsCop(name string) prog.Cop {
	return prog.New(cop.Meta{Name: name, Severity: cop.Warning}, func(p *prog.Pass) {
		p.Reportf(token.NoPos, "no position")
		for _, fn := range p.Program.AllFunctions() {
			if fn.Synthetic == "" {
				p.Reportf(fn.Pos(), "function %s", fn.Name())
			}
		}
	})
}

func TestRunner_ExclusionsIsolateFileCops(t *testing.T) {
	root := newModule(t, exclusionModule)
	t.Chdir(root)
	cfg := writeExclusionConfig(t, root, "cops:\n  Custom/A:\n    exclude: [gen/, helper.go]\n  Custom/Recorder:\n    exclude: [gen/, helper.go]\n")
	var checked []string
	recorder := cop.New(cop.Meta{Name: "Custom/Recorder"}, func(p *cop.Pass) {
		checked = append(checked, filepath.ToSlash(p.Filename()))
	})

	r := runner.New([]cop.Cop{newClauseCop("Custom/A"), newClauseCop("Custom/B"), recorder}, cfg, nil)
	rec := run(t, r, ".")

	assert.Equal(t, []string{"p.go", "sub/gen/gen.go"}, checked, "a cop must not check the files that it excludes")
	assert.Equal(t, []string{
		"Custom/A W p.go:1",
		"Custom/A W sub/gen/gen.go:1",
		"Custom/B W gen/gen.go:1",
		"Custom/B W helper.go:1",
		"Custom/B W p.go:1",
		"Custom/B W sub/gen/gen.go:1",
	}, findings(t, root, rec.offenses))
	assert.Equal(t, 3, rec.numCops)
	assert.Equal(t, 4, rec.inspected, "excluded files are still inspected by the other cops")
	assert.Len(t, rec.files, 4)
	for name, want := range map[string]int{"p.go": 2, "helper.go": 1, "gen/gen.go": 1, "sub/gen/gen.go": 2} {
		assert.Len(t, rec.files[name], want, name)
	}
}

func TestRunner_ExcludedFilesKeepTypeInformation(t *testing.T) {
	root := newModule(t, exclusionModule)
	t.Chdir(root)
	cfg := writeExclusionConfig(t, root, "cops:\n  Custom/Excluding:\n    exclude: [helper.go]\n")
	cops := func() []cop.Cop {
		return []cop.Cop{newResolvedCallCop("Custom/Excluding"), newResolvedCallCop("Custom/Including")}
	}

	rec := run(t, runner.New(cops(), cfg, nil), ".")
	assert.Equal(t, []string{
		"Custom/Excluding W p.go:3",
		"Custom/Including W helper.go:5",
		"Custom/Including W p.go:3",
	}, findings(t, root, rec.offenses))

	// Control: without helper.go as an input, the call in p.go does not resolve.
	rec = run(t, runner.New(cops(), cfg, nil), "p.go")
	assert.Empty(t, rec.offenses)
}

func TestRunner_ExclusionsFilterProgramCops(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run("shared="+strconv.FormatBool(shared), func(t *testing.T) {
			root := newModule(t, exclusionModule)
			t.Chdir(root)
			cfg := writeExclusionConfig(t, root, `cops:
  Custom/ExcludingCalls:
    exclude: [helper.go]
  Custom/ExcludingFunctions:
    exclude: ["**"]
`)

			r := runner.New(nil, cfg, nil).WithProgramCops([]prog.Cop{
				newHelperCallsCop("Custom/ExcludingCalls"),
				newHelperCallsCop("Custom/IncludingCalls"),
				newFunctionsCop("Custom/ExcludingFunctions"),
			})
			if shared {
				r.WithSharedProgramLoad()
			}
			rec := run(t, r, ".")

			assert.Equal(t, []string{
				"Custom/ExcludingCalls W p.go:3",
				"Custom/ExcludingFunctions W -:0",
				"Custom/IncludingCalls W helper.go:5",
				"Custom/IncludingCalls W p.go:3",
			}, findings(t, root, rec.offenses))
			assert.Equal(t, 3, rec.numCops)
		})
	}
}

func TestRunner_ExclusionsFilterOtherFilenames(t *testing.T) {
	root := newModule(t, exclusionModule)
	t.Chdir(root)
	cfg := writeExclusionConfig(t, root, "cops:\n  Custom/Elsewhere:\n    exclude: [helper.go, gen/]\n")
	helper := filepath.Join(root, "helper.go")

	elsewhere := cop.New(cop.Meta{Name: "Custom/Elsewhere", Severity: cop.Warning}, func(p *cop.Pass) {
		if filepath.Base(p.Filename()) != "p.go" {
			return
		}
		p.Report(p.File.Name, "current file")
		p.ReportOffense(cop.Offense{Pos: token.Position{Filename: helper, Line: 3}, Message: "absolute name"})
		p.ReportOffense(cop.Offense{Pos: token.Position{Filename: "helper.go", Line: 5}, Message: "relative name"})
		p.ReportOffense(cop.Offense{Pos: token.Position{Filename: filepath.Join("sub", "gen", "gen.go"), Line: 3}, Message: "anchored name"})
		p.ReportOffense(cop.Offense{Message: "no filename"})
	})
	rec := run(t, runner.New([]cop.Cop{elsewhere}, cfg, nil), ".")

	assert.Equal(t, []string{
		"Custom/Elsewhere W -:0",
		"Custom/Elsewhere W p.go:1",
		"Custom/Elsewhere W sub/gen/gen.go:3",
	}, findings(t, root, rec.offenses))
	assert.Len(t, rec.files["p.go"], 3, "progress events receive only the retained offenses")
}

func TestRunner_ExclusionsKeepScopesSuppressionsAndSeverity(t *testing.T) {
	root := newModule(t, map[string]string{
		"go.mod":         "module example.test\n\ngo 1.26\n",
		"p.go":           "package p\n",
		"sub/x.go":       "package sub\n",
		"sub/y.go":       "package sub //rubocop:disable Custom/File, Custom/Program\n",
		"sub/gen/gen.go": "package gen\n",
	})
	t.Chdir(root)
	cfg := writeExclusionConfig(t, root, `cops:
  Custom/File:
    severity: error
    exclude: [sub/gen/]
  Custom/Program:
    severity: convention
    exclude: [sub/gen/]
`)
	scope := cop.WithScope(cop.UnderDir("sub"))

	for _, shared := range []bool{false, true} {
		t.Run("shared="+strconv.FormatBool(shared), func(t *testing.T) {
			r := runner.New([]cop.Cop{newClauseCop("Custom/File", scope)}, cfg, nil).
				WithProgramCops([]prog.Cop{prog.FromFile(newClauseCop("Custom/Program", scope))})
			if shared {
				r.WithSharedProgramLoad()
			}
			rec := run(t, r, ".")

			assert.Equal(t, []string{"Custom/File E sub/x.go:1", "Custom/Program C sub/x.go:1"}, findings(t, root, rec.offenses))
			assert.Equal(t, 4, rec.inspected)
			assert.Len(t, rec.files["sub/x.go"], 1)
			for _, name := range []string{"p.go", "sub/y.go", "sub/gen/gen.go"} {
				assert.Empty(t, rec.files[name], name)
			}
		})
	}
}

func TestRunner_ExclusionsUseConfigDirectory(t *testing.T) {
	root := newModule(t, exclusionModule)
	cfg := writeExclusionConfig(t, root, `cops:
  Custom/RootGen:
    exclude: [gen/]
  Custom/SubGen:
    exclude: [sub/gen/]
`)
	newRunner := func() *runner.Runner {
		return runner.New([]cop.Cop{newClauseCop("Custom/RootGen"), newClauseCop("Custom/SubGen")}, cfg, nil).
			WithProgramCops([]prog.Cop{prog.FromFile(newClauseCop("Custom/RootGen")), prog.FromFile(newClauseCop("Custom/SubGen"))})
	}

	// From a subdirectory, the patterns stay anchored at the config directory.
	t.Chdir(filepath.Join(root, "sub"))
	for _, path := range []string{".", filepath.Join(root, "sub")} {
		rec := run(t, newRunner(), path)
		assert.Equal(t, []string{"Custom/RootGen W sub/gen/gen.go:1", "Custom/RootGen W sub/gen/gen.go:1"},
			findings(t, root, rec.offenses), path)
	}

	// The absolute root path from a subdirectory.
	rec := run(t, newRunner(), root)
	want := []string{
		"Custom/RootGen W helper.go:1", "Custom/RootGen W p.go:1", "Custom/RootGen W sub/gen/gen.go:1",
		"Custom/SubGen W gen/gen.go:1", "Custom/SubGen W helper.go:1", "Custom/SubGen W p.go:1",
	}
	got := findings(t, root, rec.offenses)
	for _, finding := range want {
		assert.Equal(t, 2, countOf(got, finding), "file and program cops must agree on %s", finding)
	}
	assert.Len(t, got, 2*len(want))
}

func countOf(list []string, value string) int {
	n := 0
	for _, v := range list {
		if v == value {
			n++
		}
	}
	return n
}

// TestRunner_ConfigWithoutLoadHasNoExclusions checks that a configuration
// created in Go excludes nothing, also when it has the fields of a loaded one.
func TestRunner_ConfigWithoutLoadHasNoExclusions(t *testing.T) {
	root := newModule(t, exclusionModule)
	t.Chdir(root)
	loaded := writeExclusionConfig(t, root, "cops:\n  Custom/A:\n    exclude: [gen/]\n")
	newRunner := func(cfg *config.Config) *runner.Runner {
		return runner.New([]cop.Cop{newClauseCop("Custom/A")}, cfg, nil).
			WithProgramCops([]prog.Cop{prog.FromFile(newClauseCop("Custom/A"))})
	}

	rec := run(t, newRunner(loaded), ".")
	assert.Equal(t, []string{
		"Custom/A W helper.go:1", "Custom/A W helper.go:1",
		"Custom/A W p.go:1", "Custom/A W p.go:1",
		"Custom/A W sub/gen/gen.go:1", "Custom/A W sub/gen/gen.go:1",
	}, findings(t, root, rec.offenses))

	rec = run(t, newRunner(&config.Config{DefaultCops: loaded.DefaultCops, Cops: loaded.Cops}), ".")
	assert.Equal(t, []string{
		"Custom/A W gen/gen.go:1", "Custom/A W gen/gen.go:1",
		"Custom/A W helper.go:1", "Custom/A W helper.go:1",
		"Custom/A W p.go:1", "Custom/A W p.go:1",
		"Custom/A W sub/gen/gen.go:1", "Custom/A W sub/gen/gen.go:1",
	}, findings(t, root, rec.offenses))
}

func TestRunner_ExclusionsForOtherNamesChangeNothing(t *testing.T) {
	root := newModule(t, exclusionModule)
	t.Chdir(root)
	newRunner := func(cfg *config.Config) *runner.Runner {
		return runner.New([]cop.Cop{newClauseCop("Custom/A")}, cfg, nil).WithProgramCops([]prog.Cop{newFunctionsCop("Custom/P")})
	}
	want := run(t, newRunner(config.DefaultConfig()), ".")
	assert.Len(t, want.offenses, 4+6)

	for name, cfg := range map[string]*config.Config{
		"zero value":   {},
		"unknown name": writeExclusionConfig(t, root, "cops:\n  Custom/Unknown:\n    exclude: ['**']\n"),
		"empty lists":  writeExclusionConfig(t, root, "cops:\n  Custom/A:\n    exclude: []\n  Custom/P:\n    exclude:\n"),
	} {
		t.Run(name, func(t *testing.T) {
			got := run(t, newRunner(cfg), ".")
			assert.Equal(t, findings(t, root, want.offenses), findings(t, root, got.offenses))
			assert.Equal(t, want.files, got.files)
			assert.Equal(t, want.inspected, got.inspected)
		})
	}

	// A runner literal without a configuration or cops still runs.
	rec := &recordingReporter{}
	count, err := (&runner.Runner{Reporter: rec}).Run([]string{"."})
	require.NoError(t, err)
	assert.Zero(t, count)
	assert.Equal(t, 4, rec.inspected)
}

// TestRunner_ExclusionsMatchReportedFilenames documents that offenses are
// filtered by their reported filename, which a //line directive can change,
// while a file cop skips its check by the physical filename.
func TestRunner_ExclusionsMatchReportedFilenames(t *testing.T) {
	root := newModule(t, map[string]string{
		"go.mod":   "module example.test\n\ngo 1.26\n",
		"lined.go": "package p\n\n//line virtual/v.go:10\nfunc lined() {}\n",
	})
	t.Chdir(root)
	reportFuncs := func(name string) *cop.Func {
		return cop.New(cop.Meta{Name: name, Severity: cop.Warning}, func(p *cop.Pass) {
			p.ForEachFunc(func(fn *ast.FuncDecl) { p.Report(fn.Name, "function") })
		})
	}
	cfg := writeExclusionConfig(t, root, `cops:
  Custom/Virtual:
    exclude: [virtual/]
  Custom/Physical:
    exclude: [lined.go]
  Custom/ProgramVirtual:
    exclude: [virtual/]
  Custom/ProgramPhysical:
    exclude: [lined.go]
`)
	r := runner.New([]cop.Cop{reportFuncs("Custom/Virtual"), reportFuncs("Custom/Physical"), reportFuncs("Custom/Kept")}, cfg, nil).
		WithProgramCops([]prog.Cop{prog.FromFile(reportFuncs("Custom/ProgramVirtual")), prog.FromFile(reportFuncs("Custom/ProgramPhysical"))})
	rec := run(t, r, ".")

	assert.Equal(t, []string{
		"Custom/Kept W virtual/v.go:10",
		"Custom/ProgramPhysical W virtual/v.go:10",
	}, findings(t, root, rec.offenses))
}
