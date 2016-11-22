// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at http://mozilla.org/MPL/2.0/.

package model

import (
	"time"
	"github.com/syncthing/syncthing/lib/config"
	"github.com/syncthing/syncthing/lib/fswatcher"
)

type folder struct {
	stateTracker
	scan      folderScanner
	model     *Model
	stop      chan struct{}
	fsWatcher *fswatcher.FsWatcher
}

func newFolder(model *Model, cfg config.FolderConfiguration) folder {
	return folder{
		stateTracker: newStateTracker(cfg.ID),
		scan:         newFolderScanner(cfg),
		stop:         make(chan struct{}),
		model:        model,
		fsWatcher:    fswatcher.NewFsWatcher(cfg.Path(), cfg.ID,
			model.folderIgnores[cfg.ID], defTempNamer,
			model.folderCfgs[cfg.ID].LongRescanIntervalS),
	}
}

func (f *folder) IndexUpdated() {
}

func (f *folder) IgnoresChanged() {
	f.model.fmut.RLock()
	f.fsWatcher.UpdateIgnores(f.model.folderIgnores[f.folderID])
	f.model.fmut.RUnlock()
}

func (f *folder) DelayScan(next time.Duration) {
	f.scan.Delay(next)
}

func (f *folder) Reschedule() {
	if f.fsWatcher.WatchingFs {
		f.scan.LongReschedule()
	} else {
		f.scan.Reschedule()
	}
}

func (f *folder) Scan(subdirs []string) error {
	return f.scan.Scan(subdirs)
}
func (f *folder) Stop() {
	close(f.stop)
}

func (f *folder) Jobs() ([]string, []string) {
	return nil, nil
}

func (f *folder) BringToFront(string) {}

func (f *folder) scanSubdirsIfHealthy(subDirs []string) error {
	if err := f.model.CheckFolderHealth(f.folderID); err != nil {
		l.Infoln("Skipping folder", f.folderID, "scan due to folder error:", err)
		return err
	}
	l.Debugln(f, "Scanning subdirectories")
	if err := f.model.internalScanFolderSubdirs(f.folderID, subDirs); err != nil {
		// Potentially sets the error twice, once in the scanner just
		// by doing a check, and once here, if the error returned is
		// the same one as returned by CheckFolderHealth, though
		// duplicate set is handled by setError.
		f.setError(err)
		return err
	}
	return nil
}
