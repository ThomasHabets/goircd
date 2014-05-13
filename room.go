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
	"regexp"
	"sort"
	"strings"
)

// Sanitize room's name. It can consist of 1 to 50 ASCII symbols
// with some exclusions. All room names will have "#" prefix.
func RoomNameSanitize(name string) (n string, valid bool) {
	n = strings.TrimLeft(strings.ToLower(name), "&#+!")
	valid, _ = regexp.MatchString("^[^\x00\x07\x0a\x0d ,:/]{1,50}$", n)
	return "#" + n, valid
}

type Room struct {
	name       string
	topic      string
	key        string
	members    map[*Client]bool
	hostname   string
	log_sink   chan LogEvent
	state_sink chan StateEvent
}

func NewRoom(hostname, name string, log_sink chan LogEvent, state_sink chan StateEvent) *Room {
	room := Room{name: name}
	room.members = make(map[*Client]bool)
	room.topic = ""
	room.key = ""
	room.hostname = hostname
	room.log_sink = log_sink
	room.state_sink = state_sink
	return &room
}

func (room *Room) SendTopic(client *Client) {
	if room.topic == "" {
		client.ReplyNicknamed("331", room.name, "No topic is set")
	} else {
		client.ReplyNicknamed("332", room.name, room.topic)
	}
}

// Send message to all room's subscribers, possibly excluding someone
func (room *Room) Broadcast(msg string, client_to_ignore ...*Client) {
	for member := range room.members {
		if (len(client_to_ignore) > 0) && member == client_to_ignore[0] {
			continue
		}
		member.Msg(msg)
	}
}

func (room *Room) StateSave() {
	room.state_sink <- StateEvent{room.name, room.topic, room.key}
}

func (room *Room) Processor(events chan ClientEvent) {
	var client *Client
	for event := range events {
		client = event.client
		switch event.event_type {
		case EVENT_NEW:
			room.members[client] = true
			log.Println(client, "joined", room.name)
			room.SendTopic(client)
			go room.Broadcast(fmt.Sprintf(":%s JOIN %s", client, room.name))
			room.log_sink <- LogEvent{room.name, client.nickname, "joined", true}
			nicknames := []string{}
			for member := range room.members {
				nicknames = append(nicknames, member.nickname)
			}
			sort.Strings(nicknames)
			client.ReplyNicknamed("353", "=", room.name, strings.Join(nicknames, " "))
			client.ReplyNicknamed("366", room.name, "End of NAMES list")
		case EVENT_DEL:
			if _, subscribed := room.members[client]; !subscribed {
				client.ReplyNicknamed("442", room.name, "You are not on that channel")
				continue
			}
			delete(room.members, client)
			msg := fmt.Sprintf(":%s PART %s :%s", client, room.name, client.nickname)
			go room.Broadcast(msg)
			room.log_sink <- LogEvent{room.name, client.nickname, "left", true}
		case EVENT_TOPIC:
			if _, subscribed := room.members[client]; !subscribed {
				client.ReplyParts("442", room.name, "You are not on that channel")
				continue
			}
			if event.text == "" {
				go room.SendTopic(client)
				continue
			}
			room.topic = strings.TrimLeft(event.text, ":")
			msg := fmt.Sprintf(":%s TOPIC %s :%s", client, room.name, room.topic)
			go room.Broadcast(msg)
			room.log_sink <- LogEvent{room.name, client.nickname, "set topic to " + room.topic, true}
			room.StateSave()
		case EVENT_WHO:
			for m := range room.members {
				client.ReplyNicknamed("352", room.name, m.username, m.conn.RemoteAddr().String(), room.hostname, m.nickname, "H", "0 "+m.realname)
			}
			client.ReplyNicknamed("315", room.name, "End of /WHO list")
		case EVENT_MODE:
			if event.text == "" {
				mode := "+"
				if room.key != "" {
					mode = mode + "k"
				}
				client.Msg(fmt.Sprintf("324 %s %s %s", client.nickname, room.name, mode))
				continue
			}
			if strings.HasPrefix(event.text, "-k") || strings.HasPrefix(event.text, "+k") {
				if _, subscribed := room.members[client]; !subscribed {
					client.ReplyParts("442", room.name, "You are not on that channel")
					continue
				}
			} else {
				client.ReplyNicknamed("472", event.text, "Unknown MODE flag")
				continue
			}
			var msg string
			var msg_log string
			if strings.HasPrefix(event.text, "+k") {
				cols := strings.Split(event.text, " ")
				if len(cols) == 1 {
					client.ReplyNotEnoughParameters("MODE")
					continue
				}
				room.key = cols[1]
				msg = fmt.Sprintf(":%s MODE %s +k %s", client, room.name, room.key)
				msg_log = "set channel key to " + room.key
			} else if strings.HasPrefix(event.text, "-k") {
				room.key = ""
				msg = fmt.Sprintf(":%s MODE %s -k", client, room.name)
				msg_log = "removed channel key"
			}
			go room.Broadcast(msg)
			room.log_sink <- LogEvent{room.name, client.nickname, msg_log, true}
			room.StateSave()
		case EVENT_MSG:
			sep := strings.Index(event.text, " ")
			go room.Broadcast(fmt.Sprintf(":%s %s %s :%s", client, event.text[:sep], room.name, event.text[sep+1:]), client)
			room.log_sink <- LogEvent{room.name, client.nickname, event.text[sep+1:], false}
		}
	}
}
