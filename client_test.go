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
	inbound  chan string
	outbound chan string
	closed   bool
}

func NewTestingConn() *TestingConn {
	inbound := make(chan string, 8)
	outbound := make(chan string, 8)
	return &TestingConn{inbound: inbound, outbound: outbound}
}

func (conn TestingConn) Error() string {
	return "i am finished"
}

func (conn *TestingConn) Read(b []byte) (n int, err error) {
	msg := <-conn.inbound
	if msg == "" {
		return 0, conn
	}
	for n, bt := range []byte(msg + CRLF) {
		b[n] = bt
	}
	return len(msg), nil
}

type MyAddr struct{}

func (a MyAddr) String() string {
	return "someclient"
}
func (a MyAddr) Network() string {
	return "somenet"
}

func (conn *TestingConn) Write(b []byte) (n int, err error) {
	conn.outbound <- string(b)
	return len(b), nil
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
	conn := NewTestingConn()
	sink := make(chan ClientEvent)
	client := NewClient("foohost", conn)
	go client.Processor(sink)

	event := <-sink
	if event.event_type != EVENT_NEW {
		t.Fatal("no NEW event", event)
	}
	conn.inbound <- "foo"
	event = <-sink
	ts1 := client.timestamp
	if (event.event_type != EVENT_MSG) || (event.text != "foo") {
		t.Fatal("no first MSG", event)
	}
	conn.inbound <- "bar"
	event = <-sink
	if (event.event_type != EVENT_MSG) || (event.text != "bar") {
		t.Fatal("no second MSG", event)
	}
	conn.inbound <- ""
	if client.timestamp.Before(ts1) || client.timestamp.Equal(ts1) {
		t.Fatal("timestamp updating")
	}
	event = <-sink
	if event.event_type != EVENT_DEL {
		t.Fatal("no client termination", event)
	}
}

// Test replies formatting
func TestClientReplies(t *testing.T) {
	conn := NewTestingConn()
	client := NewClient("foohost", conn)
	client.nickname = "мойник"

	client.Reply("hello")
	if r := <-conn.outbound; r != ":foohost hello\r\n" {
		t.Fatal("did not recieve hello message", r)
	}

	client.ReplyParts("200", "foo", "bar")
	if r := <-conn.outbound; r != ":foohost 200 foo :bar\r\n" {
		t.Fatal("did not recieve 200 message", r)
	}

	client.ReplyNicknamed("200", "foo", "bar")
	if r := <-conn.outbound; r != ":foohost 200 мойник foo :bar\r\n" {
		t.Fatal("did not recieve nicknamed message", r)
	}

	client.ReplyNotEnoughParameters("CMD")
	if r := <-conn.outbound; r != ":foohost 461 мойник CMD :Not enough parameters\r\n" {
		t.Fatal("did not recieve 461 message", r)
	}
}
