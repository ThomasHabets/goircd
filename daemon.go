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
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	PING_TIMEOUT    = time.Second * 180 // Max time deadline for client's unresponsiveness
	PING_THRESHOLD  = time.Second * 90  // Max idle client's time before PING are sent
	ALIVENESS_CHECK = time.Second * 10  // Client's aliveness check period
)

var (
	RE_NICKNAME = regexp.MustCompile("^[a-zA-Z0-9-]{1,9}$")
)

type Daemon struct {
	Verbose              bool
	hostname             string
	motd                 string
	clients              map[*Client]bool
	rooms                map[string]*Room
	room_sinks           map[*Room]chan ClientEvent
	last_aliveness_check time.Time
	log_sink             chan<- LogEvent
	state_sink           chan<- StateEvent
}

func NewDaemon(hostname, motd string, log_sink chan<- LogEvent, state_sink chan<- StateEvent) *Daemon {
	daemon := Daemon{hostname: hostname, motd: motd}
	daemon.clients = make(map[*Client]bool)
	daemon.rooms = make(map[string]*Room)
	daemon.room_sinks = make(map[*Room]chan ClientEvent)
	daemon.log_sink = log_sink
	daemon.state_sink = state_sink
	return &daemon
}

func (daemon *Daemon) SendLusers(client *Client) {
	lusers := 0
	for client := range daemon.clients {
		if client.registered {
			lusers++
		}
	}
	client.ReplyNicknamed("251", fmt.Sprintf("There are %d users and 0 invisible on 1 servers", lusers))
}

func (daemon *Daemon) SendMotd(client *Client) {
	if daemon.motd != "" {
		fd, err := os.Open(daemon.motd)
		if err == nil {
			defer fd.Close()
			motd := []byte{}
			var err error
			for err != io.EOF {
				buf := make([]byte, 1024)
				_, err = fd.Read(buf)
				motd = append(motd, bytes.TrimRight(buf, "\x00")...)
			}

			client.ReplyNicknamed("375", "- "+daemon.hostname+" Message of the day -")
			for _, s := range bytes.Split(bytes.TrimRight(motd, "\n"), []byte("\n")) {
				client.ReplyNicknamed("372", "- "+string(s))
			}
			client.ReplyNicknamed("376", "End of /MOTD command")
			return
		} else {
			log.Println("Can not open motd file", daemon.motd, err)
		}
	}
	client.ReplyNicknamed("422", "MOTD File is missing")
}

func (daemon *Daemon) SendWhois(client *Client, nicknames []string) {
	for _, nickname := range nicknames {
		nickname = strings.ToLower(nickname)
		found := false
		for c := range daemon.clients {
			if strings.ToLower(c.nickname) != nickname {
				continue
			}
			found = true
			client.ReplyNicknamed("311", c.nickname, c.username, c.conn.RemoteAddr().String(), "*", c.realname)
			client.ReplyNicknamed("312", c.nickname, daemon.hostname, daemon.hostname)
			subscriptions := []string{}
			for _, room := range daemon.rooms {
				for subscriber := range room.members {
					if subscriber.nickname == nickname {
						subscriptions = append(subscriptions, room.name)
					}
				}
			}
			sort.Strings(subscriptions)
			client.ReplyNicknamed("319", c.nickname, strings.Join(subscriptions, " "))
			client.ReplyNicknamed("318", c.nickname, "End of /WHOIS list")
		}
		if !found {
			client.ReplyNoNickChan(nickname)
		}
	}
}

func (daemon *Daemon) SendList(client *Client, cols []string) {
	var rooms []string
	if (len(cols) > 1) && (cols[1] != "") {
		rooms = strings.Split(strings.Split(cols[1], " ")[0], ",")
	} else {
		rooms = []string{}
		for room := range daemon.rooms {
			rooms = append(rooms, room)
		}
	}
	sort.Strings(rooms)
	for _, room := range rooms {
		r, found := daemon.rooms[room]
		if found {
			client.ReplyNicknamed("322", room, fmt.Sprintf("%d", len(r.members)), r.topic)
		}
	}
	client.ReplyNicknamed("323", "End of /LIST")
}

// Unregistered client workflow processor. Unregistered client:
// * is not PINGed
// * only QUIT, NICK and USER commands are processed
// * other commands are quietly ignored
// When client finishes NICK/USER workflow, then MOTD and LUSERS are send to him.
func (daemon *Daemon) ClientRegister(client *Client, command string, cols []string) {
	switch command {
	case "NICK":
		if len(cols) == 1 || len(cols[1]) < 1 {
			client.ReplyParts("431", "No nickname given")
			return
		}
		nickname := cols[1]
		for client := range daemon.clients {
			if client.nickname == nickname {
				client.ReplyParts("433", "*", nickname, "Nickname is already in use")
				return
			}
		}
		if !RE_NICKNAME.MatchString(nickname) {
			client.ReplyParts("432", "*", cols[1], "Erroneous nickname")
			return
		}
		client.nickname = nickname
	case "USER":
		if len(cols) == 1 {
			client.ReplyNotEnoughParameters("USER")
			return
		}
		args := strings.SplitN(cols[1], " ", 4)
		if len(args) < 4 {
			client.ReplyNotEnoughParameters("USER")
			return
		}
		client.username = args[0]
		client.realname = strings.TrimLeft(args[3], ":")
	}
	if client.nickname != "*" && client.username != "" {
		client.registered = true
		client.ReplyNicknamed("001", "Hi, welcome to IRC")
		client.ReplyNicknamed("002", "Your host is "+daemon.hostname+", running goircd")
		client.ReplyNicknamed("003", "This server was created sometime")
		client.ReplyNicknamed("004", daemon.hostname+" goircd o o")
		daemon.SendLusers(client)
		daemon.SendMotd(client)
	}
}

// Register new room in Daemon. Create an object, events sink, save pointers
// to corresponding daemon's places and start room's processor goroutine.
func (daemon *Daemon) RoomRegister(name string) (*Room, chan<- ClientEvent) {
	room_new := NewRoom(daemon.hostname, name, daemon.log_sink, daemon.state_sink)
	room_new.Verbose = daemon.Verbose
	room_sink := make(chan ClientEvent)
	daemon.rooms[name] = room_new
	daemon.room_sinks[room_new] = room_sink
	go room_new.Processor(room_sink)
	return room_new, room_sink
}

func (daemon *Daemon) HandlerJoin(client *Client, cmd string) {
	args := strings.Split(cmd, " ")
	rooms := strings.Split(args[0], ",")
	var keys []string
	if len(args) > 1 {
		keys = strings.Split(args[1], ",")
	} else {
		keys = []string{}
	}
	for n, room := range rooms {
		if !RoomNameValid(room) {
			client.ReplyNoChannel(room)
			continue
		}
		var key string
		if (n < len(keys)) && (keys[n] != "") {
			key = keys[n]
		} else {
			key = ""
		}
		denied := false
		joined := false
		for room_existing, room_sink := range daemon.room_sinks {
			if room == room_existing.name {
				if (room_existing.key != "") && (room_existing.key != key) {
					denied = true
				} else {
					room_sink <- ClientEvent{client, EVENT_NEW, ""}
					joined = true
				}
				break
			}
		}
		if denied {
			client.ReplyNicknamed("475", room, "Cannot join channel (+k) - bad key")
		}
		if denied || joined {
			continue
		}
		room_new, room_sink := daemon.RoomRegister(room)
		if key != "" {
			room_new.key = key
			room_new.StateSave()
		}
		room_sink <- ClientEvent{client, EVENT_NEW, ""}
	}
}

func (daemon *Daemon) Processor(events <-chan ClientEvent) {
	for event := range events {

		// Check for clients aliveness
		now := time.Now()
		if daemon.last_aliveness_check.Add(ALIVENESS_CHECK).Before(now) {
			for c := range daemon.clients {
				if c.timestamp.Add(PING_TIMEOUT).Before(now) {
					log.Println(c, "ping timeout")
					c.conn.Close()
					continue
				}
				if !c.ping_sent && c.timestamp.Add(PING_THRESHOLD).Before(now) {
					if c.registered {
						c.Msg("PING :" + daemon.hostname)
						c.ping_sent = true
					} else {
						log.Println(c, "ping timeout")
						c.conn.Close()
					}
				}
			}
			daemon.last_aliveness_check = now
		}

		client := event.client
		switch event.event_type {
		case EVENT_NEW:
			daemon.clients[client] = true
		case EVENT_DEL:
			delete(daemon.clients, client)
			for _, room_sink := range daemon.room_sinks {
				room_sink <- event
			}
		case EVENT_MSG:
			cols := strings.SplitN(event.text, " ", 2)
			command := strings.ToUpper(cols[0])
			if daemon.Verbose {
				log.Println(client, "command", command)
			}
			if command == "QUIT" {
				delete(daemon.clients, client)
				client.conn.Close()
				continue
			}
			if !client.registered {
				go daemon.ClientRegister(client, command, cols)
				continue
			}
			switch command {
			case "AWAY":
				continue
			case "JOIN":
				if len(cols) == 1 || len(cols[1]) < 1 {
					client.ReplyNotEnoughParameters("JOIN")
					continue
				}
				go daemon.HandlerJoin(client, cols[1])
			case "LIST":
				daemon.SendList(client, cols)
			case "LUSERS":
				go daemon.SendLusers(client)
			case "MODE":
				if len(cols) == 1 || len(cols[1]) < 1 {
					client.ReplyNotEnoughParameters("MODE")
					continue
				}
				cols = strings.SplitN(cols[1], " ", 2)
				if cols[0] == client.username {
					if len(cols) == 1 {
						client.Msg("221 " + client.nickname + " +")
					} else {
						client.ReplyNicknamed("501", "Unknown MODE flag")
					}
					continue
				}
				room := cols[0]
				r, found := daemon.rooms[room]
				if !found {
					client.ReplyNoChannel(room)
					continue
				}
				if len(cols) == 1 {
					daemon.room_sinks[r] <- ClientEvent{client, EVENT_MODE, ""}
				} else {
					daemon.room_sinks[r] <- ClientEvent{client, EVENT_MODE, cols[1]}
				}
			case "MOTD":
				go daemon.SendMotd(client)
			case "PART":
				if len(cols) == 1 || len(cols[1]) < 1 {
					client.ReplyNotEnoughParameters("PART")
					continue
				}
				for _, room := range strings.Split(cols[1], ",") {
					r, found := daemon.rooms[room]
					if !found {
						client.ReplyNoChannel(room)
						continue
					}
					daemon.room_sinks[r] <- ClientEvent{client, EVENT_DEL, ""}
				}
			case "PING":
				if len(cols) == 1 {
					client.ReplyNicknamed("409", "No origin specified")
					continue
				}
				client.Reply(fmt.Sprintf("PONG %s :%s", daemon.hostname, cols[1]))
			case "PONG":
				continue
			case "NOTICE", "PRIVMSG":
				if len(cols) == 1 {
					client.ReplyNicknamed("411", "No recipient given ("+command+")")
					continue
				}
				cols = strings.SplitN(cols[1], " ", 2)
				if len(cols) == 1 {
					client.ReplyNicknamed("412", "No text to send")
					continue
				}
				msg := ""
				target := strings.ToLower(cols[0])
				for c := range daemon.clients {
					if c.nickname == target {
						msg = fmt.Sprintf(":%s %s %s :%s", client, command, c.nickname, cols[1])
						c.Msg(msg)
						break
					}
				}
				if msg != "" {
					continue
				}
				r, found := daemon.rooms[target]
				if !found {
					client.ReplyNoNickChan(target)
				}
				daemon.room_sinks[r] <- ClientEvent{client, EVENT_MSG, command + " " + strings.TrimLeft(cols[1], ":")}
			case "TOPIC":
				if len(cols) == 1 {
					client.ReplyNotEnoughParameters("TOPIC")
					continue
				}
				cols = strings.SplitN(cols[1], " ", 2)
				r, found := daemon.rooms[cols[0]]
				if !found {
					client.ReplyNoChannel(cols[0])
					continue
				}
				var change string
				if len(cols) > 1 {
					change = cols[1]
				} else {
					change = ""
				}
				daemon.room_sinks[r] <- ClientEvent{client, EVENT_TOPIC, change}
			case "WHO":
				if len(cols) == 1 || len(cols[1]) < 1 {
					client.ReplyNotEnoughParameters("WHO")
					continue
				}
				room := strings.Split(cols[1], " ")[0]
				r, found := daemon.rooms[room]
				if !found {
					client.ReplyNoChannel(room)
					continue
				}
				daemon.room_sinks[r] <- ClientEvent{client, EVENT_WHO, ""}
			case "WHOIS":
				if len(cols) == 1 || len(cols[1]) < 1 {
					client.ReplyNotEnoughParameters("WHOIS")
					continue
				}
				cols := strings.Split(cols[1], " ")
				nicknames := strings.Split(cols[len(cols)-1], ",")
				go daemon.SendWhois(client, nicknames)
			default:
				client.ReplyNicknamed("421", command, "Unknown command")
			}
		}
	}
}
