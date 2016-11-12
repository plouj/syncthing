// Copyright (C) 2016 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at http://mozilla.org/MPL/2.0/.

package fswatcher

import (
	"errors"
	"github.com/zillode/notify"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/syncthing/syncthing/lib/events"
	"github.com/syncthing/syncthing/lib/scanner"
	"github.com/syncthing/syncthing/lib/ignore"
)

type FsEvent struct {
	path string
}

var Tempnamer scanner.TempNamer

type FsEventsBatch map[string]*FsEvent

type FsWatcher struct {
	folderPath            string
	notifyModelChan       chan<- FsEventsBatch
	fsEvents              FsEventsBatch
	fsEventChan           <-chan notify.EventInfo
	WatchingFs            bool
	notifyDelay           time.Duration
	notifyTimer           *time.Timer
	notifyTimerNeedsReset bool
	inProgress            map[string]struct{}
	ignores               *ignore.Matcher
}

const (
	slowNotifyDelay = time.Duration(60) * time.Second
	fastNotifyDelay = time.Duration(500) * time.Millisecond
)

func NewFsWatcher(folderPath string, ignores *ignore.Matcher) *FsWatcher {
	return &FsWatcher{
		folderPath:            folderPath,
		notifyModelChan:       nil,
		fsEvents:              make(FsEventsBatch),
		fsEventChan:           nil,
		WatchingFs:            false,
		notifyDelay:           fastNotifyDelay,
		notifyTimerNeedsReset: false,
		inProgress:            make(map[string]struct{}),
		ignores:               ignores,
	}
}

func (watcher *FsWatcher) StartWatchingFilesystem() (<-chan FsEventsBatch, error) {
	fsEventChan, err := setupNotifications(watcher.folderPath)
	if err == nil {
		watcher.WatchingFs = true
		watcher.fsEventChan = fsEventChan
		go watcher.watchFilesystem()
	}
	notifyModelChan := make(chan FsEventsBatch)
	watcher.notifyModelChan = notifyModelChan
	return notifyModelChan, err
}

var maxFiles = 512

func setupNotifications(path string) (chan notify.EventInfo, error) {
	c := make(chan notify.EventInfo, maxFiles)
	if err := notify.Watch(filepath.Join(path, "..."), c, notify.All); err != nil {
		notify.Stop(c)
		close(c)
		return nil, interpretNotifyWatchError(err, path)
	}
	l.Debugf("Setup filesystem notification for %s", path)
	return c, nil
}

func (watcher *FsWatcher) watchFilesystem() {
	watcher.notifyTimer = time.NewTimer(watcher.notifyDelay)
	defer watcher.notifyTimer.Stop()
	inProgressItemSubscription := events.Default.Subscribe(
		events.ItemStarted | events.ItemFinished)
	for {
		watcher.resetNotifyTimerIfNeeded()
		select {
		case event, _ := <-watcher.fsEventChan:
			newEvent := watcher.newFsEvent(event.Path())
			if newEvent != nil {
				watcher.speedUpNotifyTimer()
				watcher.storeFsEvent(newEvent)
			}
		case <-watcher.notifyTimer.C:
			watcher.actOnTimer()
		case event := <-inProgressItemSubscription.C():
			watcher.updateInProgressSet(event)
		}
	}
}

func (watcher *FsWatcher) newFsEvent(eventPath string) *FsEvent {
	if isSubpath(eventPath, watcher.folderPath) {
		path, _ := filepath.Rel(watcher.folderPath, eventPath)
		if !watcher.shouldIgnore(path) {
			return &FsEvent{path}
		}
	}
	return nil
}

func isSubpath(path string, folderPath string) bool {
	if len(path) > 1 && os.IsPathSeparator(path[len(path)-1]) {
		path = path[0 : len(path)-1]
	}
	if len(folderPath) > 1 && os.IsPathSeparator(folderPath[len(folderPath)-1]) {
		folderPath = folderPath[0 : len(folderPath)-1]
	}
	return strings.HasPrefix(path, folderPath)
}

func (watcher *FsWatcher) resetNotifyTimerIfNeeded() {
	if watcher.notifyTimerNeedsReset {
		l.Debugf("Resetting notifyTimer to %#v\n", watcher.notifyDelay)
		watcher.notifyTimer.Reset(watcher.notifyDelay)
		watcher.notifyTimerNeedsReset = false
	}
}

func (watcher *FsWatcher) speedUpNotifyTimer() {
	if watcher.notifyDelay != fastNotifyDelay {
		watcher.notifyDelay = fastNotifyDelay
		l.Debugf("Speeding up notifyTimer to %#v\n", fastNotifyDelay)
		watcher.notifyTimerNeedsReset = true
	}
}

func (watcher *FsWatcher) slowDownNotifyTimer() {
	if watcher.notifyDelay != slowNotifyDelay {
		watcher.notifyDelay = slowNotifyDelay
		l.Debugf("Slowing down notifyTimer to %#v\n", watcher.notifyDelay)
		watcher.notifyTimerNeedsReset = true
	}
}

func (watcher *FsWatcher) storeFsEvent(event *FsEvent) {
	if watcher.pathInProgress(event.path) {
		l.Debugf("Skipping notification for finished path: %s\n",
			event.path)
	} else {
		watcher.fsEvents[event.path] = event
	}
}

func (watcher *FsWatcher) actOnTimer() {
	watcher.notifyTimerNeedsReset = true
	if len(watcher.fsEvents) > 0 {
		l.Debugf("Notifying about %d fs events\n", len(watcher.fsEvents))
		watcher.notifyModelChan <- watcher.fsEvents
	} else {
		watcher.slowDownNotifyTimer()
	}
	watcher.fsEvents = make(FsEventsBatch)
}

func (watcher *FsWatcher) events() []*FsEvent {
	list := make([]*FsEvent, 0, len(watcher.fsEvents))
	for _, event := range watcher.fsEvents {
		list = append(list, event)
	}
	return list
}

func (watcher *FsWatcher) updateInProgressSet(event events.Event) {
	if event.Type == events.ItemStarted {
		path := event.Data.(map[string]string)["item"]
		watcher.inProgress[path] = struct{}{}
	} else if event.Type == events.ItemFinished {
		path := event.Data.(map[string]interface{})["item"].(string)
		delete(watcher.inProgress, path)
	}
}

func (watcher *FsWatcher) shouldIgnore(path string) bool {
	return scanner.IsIgnoredPath(path, watcher.ignores) ||
		Tempnamer.IsTemporary(path)
}

func (watcher *FsWatcher) pathInProgress(path string) bool {
	_, exists := watcher.inProgress[path]
	return exists
}

func (watcher *FsWatcher) UpdateIgnores(ignores *ignore.Matcher) {
	watcher.ignores = ignores
}

func (batch FsEventsBatch) GetPaths() []string {
	var paths []string
	for _, event := range batch {
		paths = append(paths, event.path)
	}
	return paths
}

func WatchesLimitTooLowError(folder string) error {
	return errors.New("Failed to install inotify handler for " +
		folder +
		". Please increase inotify limits," +
		" see http://bit.ly/1PxkdUC for more information.")
}
