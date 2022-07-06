package walker

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"io/ioutil"
	"log"
	"path/filepath"

	"github.com/hashicorp/terraform-ls/internal/document"
	"github.com/hashicorp/terraform-ls/internal/job"
	"github.com/hashicorp/terraform-ls/internal/terraform/ast"
)

var (
	discardLogger = log.New(ioutil.Discard, "", 0)

	// skipDirNames represent directory names which would never contain
	// plugin/module cache, so it's safe to skip them during the walk
	//
	// please keep the list in `SETTINGS.md` in sync
	skipDirNames = map[string]bool{
		".git":                true,
		".idea":               true,
		".vscode":             true,
		"terraform.tfstate.d": true,
		".terragrunt-cache":   true,
	}
)

type pathToWatch struct{}

type Walker struct {
	fs        fs.FS
	pathStore PathStore
	modStore  ModuleStore

	logger   *log.Logger
	walkFunc WalkFunc

	Collector *WalkerCollector

	cancelFunc context.CancelFunc

	excludeModulePaths   map[string]bool
	ignoreDirectoryNames map[string]bool
}

type WalkFunc func(ctx context.Context, modHandle document.DirHandle) (job.IDs, error)

type PathStore interface {
	AwaitNextDir(ctx context.Context) (document.DirHandle, error)
	RemoveDir(dir document.DirHandle) error
}

type ModuleStore interface {
	Exists(dir string) (bool, error)
	Add(dir string) error
}

func NewWalker(fs fs.FS, pathStore PathStore, modStore ModuleStore, walkFunc WalkFunc) *Walker {
	return &Walker{
		fs:                   fs,
		pathStore:            pathStore,
		modStore:             modStore,
		walkFunc:             walkFunc,
		logger:               discardLogger,
		ignoreDirectoryNames: skipDirNames,
	}
}

func (w *Walker) SetLogger(logger *log.Logger) {
	w.logger = logger
}

func (w *Walker) SetExcludeModulePaths(excludeModulePaths []string) {
	w.excludeModulePaths = make(map[string]bool)
	for _, path := range excludeModulePaths {
		w.excludeModulePaths[path] = true
	}
}

func (w *Walker) SetIgnoreDirectoryNames(ignoreDirectoryNames []string) {
	if w.cancelFunc != nil {
		panic("cannot set ignorelist after walking started")
	}
	for _, path := range ignoreDirectoryNames {
		w.ignoreDirectoryNames[path] = true
	}
}

func (w *Walker) Stop() {
	if w.cancelFunc != nil {
		w.cancelFunc()
	}
}

func (w *Walker) StartWalking(ctx context.Context) error {
	ctx, cancelFunc := context.WithCancel(ctx)
	w.cancelFunc = cancelFunc

	go func() {
		for {
			nextDir, err := w.pathStore.AwaitNextDir(ctx)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}
				w.logger.Printf("walker: awaiting next dir failed: %s", err)
				w.collectError(err)
				return
			}

			err = w.walk(ctx, nextDir)
			if err != nil {
				w.logger.Printf("walker: walking through %q failed: %s", nextDir, err)
				w.collectError(err)
				continue
			}

			err = w.pathStore.RemoveDir(nextDir)
			if err != nil {
				w.logger.Printf("walker: removing dir %q from queue failed: %s", nextDir, err)
				w.collectError(err)
				continue
			}
			w.logger.Printf("walker: walking through %q finished", nextDir)

			select {
			case <-ctx.Done():
				return
			default:
			}
		}
	}()

	return nil
}

func (w *Walker) collectError(err error) {
	if w.Collector != nil {
		w.Collector.CollectError(err)
	}
}

func (w *Walker) collectJobIds(jobIds job.IDs) {
	if w.Collector != nil {
		for _, id := range jobIds {
			w.Collector.CollectJobId(id)
		}
	}
}

func (w *Walker) isSkippableDir(dirName string) bool {
	_, ok := w.ignoreDirectoryNames[dirName]
	return ok
}

func (w *Walker) walk(ctx context.Context, dir document.DirHandle) error {
	dirsWalked := make(map[string]struct{}, 0)

	err := fs.WalkDir(w.fs, dir.Path(), func(path string, info fs.DirEntry, err error) error {
		select {
		case <-ctx.Done():
			w.logger.Printf("cancelling walk of %s...", dir)
			return fmt.Errorf("walk cancelled")
		default:
		}

		if err != nil {
			w.logger.Printf("unable to access %s: %s", path, err.Error())
			return nil
		}

		dir, err := filepath.Abs(filepath.Dir(path))
		if err != nil {
			return err
		}

		if w.isSkippableDir(info.Name()) {
			w.logger.Printf("skipping %s", path)
			return filepath.SkipDir
		}

		if _, ok := w.excludeModulePaths[dir]; ok {
			return filepath.SkipDir
		}

		// TODO: replace local map lookup with w.modStore.HasChangedSince(modTime)
		// once available
		// See https://github.com/hashicorp/terraform-ls/issues/989
		_, walked := dirsWalked[dir]

		w.logger.Printf("walker checking file %q; !walked: %t && isModule: %t && !isIgnored: %t",
			info.Name(),
			walked, ast.IsModuleFilename(info.Name()), ast.IsIgnoredFile(info.Name()))

		if !walked && ast.IsModuleFilename(info.Name()) && !ast.IsIgnoredFile(info.Name()) {
			dirsWalked[dir] = struct{}{}

			w.logger.Printf("found module %s", dir)

			exists, err := w.modStore.Exists(dir)
			if err != nil {
				return err
			}
			if !exists {
				err := w.modStore.Add(dir)
				if err != nil {
					return err
				}
			}

			modHandle := document.DirHandleFromPath(dir)
			ids, err := w.walkFunc(ctx, modHandle)
			if err != nil {
				w.collectError(err)
			}
			w.collectJobIds(ids)

			return nil
		}

		if info.IsDir() {
			// All other files are skipped
			return nil
		}

		return nil
	})
	w.logger.Printf("walking of %s finished", dir)
	return err
}
