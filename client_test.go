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
	"net"
	"testing"
	"time"
)

// Testing network connection that satisfies net.Conn interface
// Can send predefined messages and store all written ones
type TestingConn struct {
	msgs     []string
	msg_ptr  int
	incoming []string
	closed   bool
}

func NewTestingConn(msgs ...string) *TestingConn {
	msgs_crlf := []string{}
	for _, msg := range msgs {
		msgs_crlf = append(msgs_crlf, msg+"\r\n")
	}
	return &TestingConn{msgs: msgs_crlf, msg_ptr: -1}
}

func (conn TestingConn) Error() string {
	return "i am out"
}

func (conn *TestingConn) Read(b []byte) (n int, err error) {
	conn.msg_ptr++
	if len(conn.msgs) == conn.msg_ptr {
		return 0, TestingConn{}
	}
	for n, bt := range []byte(conn.msgs[conn.msg_ptr]) {
		b[n] = bt
	}
	return len(conn.msgs[conn.msg_ptr]), nil
}

type MyAddr struct{}

func (a MyAddr) String() string {
	return "someclient"
}
func (a MyAddr) Network() string {
	return "somenet"
}

func (conn *TestingConn) Write(b []byte) (n int, err error) {
	conn.incoming = append(conn.incoming, string(b))
	return 0, nil
}

func (conn *TestingConn) Close() error {
	conn.closed = true
	return nil
}

func (conn TestingConn) LocalAddr() net.Addr {
	return nil
}

func (conn TestingConn) RemoteAddr() net.Addr {
	return MyAddr{}
}

func (conn TestingConn) SetDeadline(t time.Time) error {
	return nil
}

func (conn TestingConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (conn TestingConn) SetWriteDeadline(t time.Time) error {
	return nil
}

// New client creation test. It must send an event about new client,
// two predefined messages from it and deletion one
func TestNewClient(t *testing.T) {
	conn := NewTestingConn("foo", "bar")
	sink := make(chan ClientEvent)
	client := NewClient("foohost", conn)
	go client.Processor(sink)

	event := <-sink
	if event.event_type != EVENT_NEW {
		t.Fail()
	}
	event = <-sink
	if (event.event_type != EVENT_MSG) || (event.text != "foo") {
		t.Fail()
	}
	event = <-sink
	if (event.event_type != EVENT_MSG) || (event.text != "bar") {
		t.Fail()
	}
	event = <-sink
	if event.event_type != EVENT_DEL {
		t.Fail()
	}
}

// Test replies formatting
func TestClientReplies(t *testing.T) {
	conn := NewTestingConn("foo", "bar")
	client := NewClient("foohost", conn)
	client.nickname = "мойник"

	client.Reply("hello")
	if (len(conn.incoming) != 1) || (conn.incoming[0] != ":foohost hello\r\n") {
		t.Fatal("did not recieve hello message")
	}

	client.ReplyParts("200", "foo", "bar")
	if (len(conn.incoming) != 2) || (conn.incoming[1] != ":foohost 200 foo :bar\r\n") {
		t.Fatal("did not recieve 200 message")
	}

	client.ReplyNicknamed("200", "foo", "bar")
	if (len(conn.incoming) != 3) || (conn.incoming[2] != ":foohost 200 мойник foo :bar\r\n") {
		t.Fatal("did not recieve nicknamed message")
	}

	client.ReplyNotEnoughParameters("CMD")
	if (len(conn.incoming) != 4) || (conn.incoming[3] != ":foohost 461 мойник CMD :Not enough parameters\r\n") {
		t.Fatal("did not recieve 461 message")
	}
}
