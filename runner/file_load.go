package runner

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sync"

	"golang.org/x/tools/go/packages"

	"github.com/dgageot/rubocop-go/cop"
)

// sharedFileLoader is lazy and run-local. Its cached ASTs are never the ASTs
// passed to file cops by the regular file loop.
type sharedFileLoader struct {
	mu       sync.Mutex
	files    []string
	contexts map[string]string
	dirs     map[string][]string
	batches  map[string]*typedFileBatch
	err      error
	load     func(*packages.Config, ...string) ([]*packages.Package, error)
}

type typedFileBatch struct {
	files  map[string][]*cop.Pass
	errors map[string]error
	err    error
}

func newSharedFileLoader(files []string) *sharedFileLoader {
	return &sharedFileLoader{files: files, load: packages.Load}
}

func (l *sharedFileLoader) init() {
	l.contexts = make(map[string]string)
	l.dirs = make(map[string][]string)
	l.batches = make(map[string]*typedFileBatch)
	if !canShareImports() {
		l.err = errors.New("typed file views unavailable in this build environment")
		return
	}
	for _, file := range l.files {
		name, err := filepath.Abs(file)
		if err != nil {
			l.err = err
			return
		}
		dir := filepath.Dir(name)
		if _, ok := l.contexts[dir]; ok {
			continue
		}
		key := importContext(dir)
		l.contexts[dir] = key
		if key != "" {
			l.dirs[key] = append(l.dirs[key], dir)
		}
	}
}

func (l *sharedFileLoader) typedFiles(filename string, c cop.Cop) ([]*cop.Pass, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.contexts == nil {
		l.init()
	}
	if l.err != nil {
		return nil, l.err
	}
	name, err := filepath.Abs(filename)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(name)
	key := l.contexts[dir]
	if key == "" {
		return nil, fmt.Errorf("typed file views unavailable for %s", dir)
	}
	batch := l.batches[key]
	if batch == nil {
		batch = l.loadBatch(l.dirs[key])
		l.batches[key] = batch
	}
	if batch.err != nil {
		return nil, batch.err
	}
	if err := batch.errors[dir]; err != nil {
		return nil, err
	}
	if err := batch.errors[name]; err != nil {
		return nil, err
	}
	var views []*cop.Pass
	for _, view := range batch.files[name] {
		views = append(views, &cop.Pass{
			Cop: c, FileSet: view.FileSet, File: view.File,
			Info: view.Info, Package: view.Package,
		})
	}
	return views, nil
}

func (l *sharedFileLoader) loadBatch(dirs []string) *typedFileBatch {
	batch := &typedFileBatch{files: make(map[string][]*cop.Pass), errors: make(map[string]error)}
	loaded, err := l.load(&packages.Config{
		Dir: dirs[0], Tests: true,
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports,
	}, dirs...)
	if err != nil {
		batch.err = err
		return batch
	}
	// Keep every initial-root variant in load order, including test variants.
	// A bad variant invalidates the whole directory, not unrelated roots.
	for _, pkg := range loaded {
		if buildExcludedPackage(pkg) {
			continue
		}
		if len(pkg.Errors) != 0 || pkg.IllTyped {
			err := fmt.Errorf("typed file views unavailable for unhealthy package %s", pkg.ID)
			if pkg.Dir == "" {
				batch.err = err
			} else {
				batch.errors[filepath.Clean(pkg.Dir)] = err
			}
			continue
		}
		var filenames []string
		for _, file := range pkg.Syntax {
			name := filepath.Clean(pkg.Fset.PositionFor(file.Pos(), false).Filename)
			filenames = append(filenames, name)
			batch.files[name] = append(batch.files[name], &cop.Pass{
				FileSet: pkg.Fset, File: file, Info: pkg.TypesInfo, Package: pkg.Types,
			})
		}
		// Cgo can replace active sources with generated syntax at another path.
		for _, name := range pkg.GoFiles {
			if !slices.Contains(filenames, filepath.Clean(name)) {
				batch.errors[filepath.Clean(name)] = fmt.Errorf("typed file views unavailable for rewritten source %s", name)
			}
		}
	}
	return batch
}
