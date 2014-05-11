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
	"io/ioutil"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRegistrationWorkflow(t *testing.T) {
	daemon := NewDaemon("foohost", "", nil, nil)
	events := make(chan ClientEvent)
	go daemon.Processor(events)
	conn := NewTestingConn()
	client := NewClient("foohost", conn)

	events <- ClientEvent{client, EVENT_NEW, ""}
	events <- ClientEvent{client, EVENT_MSG, "UNEXISTENT CMD"}
	time.Sleep(100)
	if len(conn.incoming) > 0 {
		t.Fail()
	}

	events <- ClientEvent{client, EVENT_MSG, "NICK"}
	time.Sleep(100)
	if (len(conn.incoming) != 1) || (conn.incoming[0] != ":foohost 431 :No nickname given\r\n") {
		t.Fail()
	}

	events <- ClientEvent{client, EVENT_MSG, "NICK meinick"}
	time.Sleep(100)
	if (len(conn.incoming) != 1) || (client.nickname != "meinick") || client.registered {
		t.Fail()
	}

	events <- ClientEvent{client, EVENT_MSG, "USER"}
	time.Sleep(100)
	if (len(conn.incoming) != 2) || (conn.incoming[1] != ":foohost 461 meinick USER :Not enough parameters\r\n") {
		t.Fail()
	}

	events <- ClientEvent{client, EVENT_MSG, "USER 1 2 3"}
	time.Sleep(100)
	if (len(conn.incoming) != 3) || (conn.incoming[2] != ":foohost 461 meinick USER :Not enough parameters\r\n") {
		t.Fail()
	}

	daemon.SendLusers(client)
	if !strings.Contains(conn.incoming[len(conn.incoming)-1], "There are 0 users") {
		t.Fail()
	}

	events <- ClientEvent{client, EVENT_MSG, "USER 1 2 3 :4 5"}
	time.Sleep(100)
	if (len(conn.incoming) < 4) || (client.username != "1") || (client.realname != "4 5") {
		t.Fail()
	}

	statuses := map[int]bool{1: false, 2: false, 3: false, 4: false, 251: false, 422: false}
	for _, msg := range conn.incoming {
		for k, _ := range statuses {
			if strings.HasPrefix(msg, fmt.Sprintf(":foohost %03d", k)) {
				statuses[k] = true
			}
		}
	}
	for _, v := range statuses {
		if !v {
			t.Fail()
		}
	}
	if !client.registered {
		t.Fail()
	}

	events <- ClientEvent{client, EVENT_MSG, "UNEXISTENT CMD"}
	time.Sleep(100)
	if conn.incoming[len(conn.incoming)-1] != ":foohost 421 meinick UNEXISTENT :Unknown command\r\n" {
		t.Fail()
	}

	events <- ClientEvent{client, EVENT_MSG, "AWAY"}
	time.Sleep(100)
	if conn.incoming[len(conn.incoming)-1] == ":foohost 421 meinick AWAY :Unknown command\r\n" {
		t.Fail()
	}

	daemon.SendLusers(client)
	if !strings.Contains(conn.incoming[len(conn.incoming)-1], "There are 1 users") {
		t.Fail()
	}

	events <- ClientEvent{client, EVENT_MSG, "QUIT"}
	time.Sleep(100)
	if !conn.closed {
		t.Fail()
	}
}

func TestMotd(t *testing.T) {
	fd, err := ioutil.TempFile("", "motd")
	if err != nil {
		t.Fatal("can not create temporary file")
	}
	defer os.Remove(fd.Name())
	fd.Write([]byte("catched\n"))
	daemon := NewDaemon("foohost", fd.Name(), nil, nil)
	conn := NewTestingConn()
	client := NewClient("foohost", conn)

	daemon.SendMotd(client)
	catched := false
	for _, msg := range conn.incoming {
		if strings.Contains(msg, "372 * :- catched") {
			catched = true
		}
	}
	if !catched {
		t.Fail()
	}
}
