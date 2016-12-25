// Copyright (C) 2016 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at http://mozilla.org/MPL/2.0/.

package model

import (
	"github.com/syncthing/syncthing/lib/config"
	"math/rand"
	"time"
)

type rescanRequest struct {
	subdirs []string
	err     chan error
}

// bundle all folder scan activity
type folderScanner struct {
	interval     time.Duration
	longInterval time.Duration
	timer        *time.Timer
	now          chan rescanRequest
	delay        chan time.Duration
}

func newFolderScanner(config config.FolderConfiguration) folderScanner {
	return folderScanner{
		interval:     time.Duration(config.RescanIntervalS) * time.Second,
		longInterval: time.Duration(config.LongRescanIntervalS) * time.Second,
		timer:        time.NewTimer(time.Millisecond), // The first scan should be done immediately.
		now:          make(chan rescanRequest),
		delay:        make(chan time.Duration),
	}
}

func (f *folderScanner) LongReschedule() {
	f.reschedule(f.longInterval)
}

func (f *folderScanner) Reschedule() {
	f.reschedule(f.interval)
}

func (f *folderScanner) reschedule(interval time.Duration) {
	if interval == 0 {
		return
	}
	// Sleep a random time between 3/4 and 5/4 of the configured interval.
	sleepNanos := (interval.Nanoseconds()*3 + rand.Int63n(2*interval.Nanoseconds())) / 4
	randInterval := time.Duration(sleepNanos) * time.Nanosecond
	l.Debugln(f, "next rescan in", randInterval)
	f.timer.Reset(randInterval)
}

func (f *folderScanner) Scan(subdirs []string) error {
	req := rescanRequest{
		subdirs: subdirs,
		err:     make(chan error),
	}
	f.now <- req
	return <-req.err
}

func (f *folderScanner) Delay(next time.Duration) {
	f.delay <- next
}

func (f *folderScanner) HasNoInterval() bool {
	return f.interval == 0
}
