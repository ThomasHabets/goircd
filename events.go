/*
goircd -- minimalistic simple Internet Relay Chat (IRC) server
Copyright (C) 2014 Sergey Matveev <stargrave@stargrave.org>

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program.  If not, see <http://www.gnu.org/licenses/>.
*/
package main

import (
	"fmt"
	"log"
	"os"
	"path"
	"time"
)

const (
	EVENT_NEW   = iota
	EVENT_DEL   = iota
	EVENT_MSG   = iota
	EVENT_TOPIC = iota
	EVENT_WHO   = iota
	EVENT_MODE  = iota
	FORMAT_MSG  = "[%s] <%s> %s\n"
	FORMAT_META = "[%s] * %s %s\n"
)

// Client events going from each of client
// They can be either NEW, DEL or unparsed MSG
type ClientEvent struct {
	client     *Client
	event_type int
	text       string
}

func (m ClientEvent) String() string {
	return string(m.event_type) + ": " + m.client.String() + ": " + m.text
}

// Logging in-room events
// Intended to tell when, where and who send a message or meta command
type LogEvent struct {
	where string
	who   string
	what  string
	meta  bool
}

// Logging events logger itself
// Each room's events are written to separate file in logdir
// Events include messages, topic and keys changes, joining and leaving
func Logger(logdir string, events <-chan LogEvent) {
	mode := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	perm := os.FileMode(0660)
	var format string
	for event := range events {
		logfile := path.Join(logdir, event.where)
		fd, err := os.OpenFile(logfile, mode, perm)
		if err != nil {
			log.Println("Can not open logfile", logfile, err)
			continue
		}
		if event.meta {
			format = FORMAT_META
		} else {
			format = FORMAT_MSG
		}
		_, err = fd.WriteString(fmt.Sprintf(format, time.Now(), event.who, event.what))
		fd.Close()
		if err != nil {
			log.Println("Error writing to logfile", logfile, err)
		}
	}
}

type StateEvent struct {
	where string
	topic string
	key   string
}

// Room state events saver
// Room states shows that either topic or key has been changed
// Each room's state is written to separate file in statedir
func StateKeeper(statedir string, events <-chan StateEvent) {
	mode := os.O_CREATE | os.O_TRUNC | os.O_WRONLY
	perm := os.FileMode(0660)
	for event := range events {
		state_path := path.Join(statedir, event.where)
		fd, err := os.OpenFile(state_path, mode, perm)
		if err != nil {
			log.Println("Can not open statefile", state_path, err)
			continue
		}
		fd.WriteString(event.topic + "\n" + event.key + "\n")
		fd.Close()
	}
}
