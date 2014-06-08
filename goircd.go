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
	"crypto/tls"
	"flag"
	"log"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
)

var (
	hostname = flag.String("hostname", "localhost", "Hostname")
	bind     = flag.String("bind", ":6667", "Address to bind to")
	motd     = flag.String("motd", "", "Path to MOTD file")
	logdir   = flag.String("logdir", "", "Absolute path to directory for logs")
	statedir = flag.String("statedir", "", "Absolute path to directory for states")

	ssl     = flag.Bool("ssl", false, "Use SSL only.")
	sslKey  = flag.String("ssl_key", "", "SSL keyfile.")
	sslCert = flag.String("ssl_cert", "", "SSL certificate.")

	verbose = flag.Bool("v", false, "Enable verbose logging.")
)

func Run() {
	var client *Client
	events := make(chan ClientEvent)
	log.SetFlags(log.Ldate | log.Lmicroseconds | log.Lshortfile)

	log_sink := make(chan LogEvent)
	if *logdir == "" {
		// Dummy logger
		go func() {
			for _ = range log_sink {
			}
		}()
	} else {
		if !path.IsAbs(*logdir) {
			log.Fatalln("Need absolute path for logdir")
			return
		}
		go Logger(*logdir, log_sink)
		log.Println(*logdir, "logger initialized")
	}

	state_sink := make(chan StateEvent)
	daemon := NewDaemon(*hostname, *motd, log_sink, state_sink)
	daemon.Verbose = *verbose
	if *statedir == "" {
		// Dummy statekeeper
		go func() {
			for _ = range state_sink {
			}
		}()
	} else {
		if !path.IsAbs(*statedir) {
			log.Fatalln("Need absolute path for statedir")
		}
		states, err := filepath.Glob(*statedir + "/#*")
		if err != nil {
			log.Fatalln("Can not read statedir", err)
		}
		for _, state := range states {
			fd, err := os.Open(state)
			if err != nil {
				log.Fatalln("Can not open state", state, err)
			}
			buf := make([]byte, 1024)
			_, err = fd.Read(buf)
			fd.Close()
			if err != nil {
				log.Fatalln("Can not read state", state, err)
			}
			room, _ := daemon.RoomRegister(path.Base(state))
			buf = bytes.TrimRight(buf, "\x00")
			contents := strings.Split(string(buf), "\n")
			room.topic = contents[0]
			room.key = contents[1]
			log.Println("Loaded state for room", room.name)
		}
		go StateKeeper(*statedir, state_sink)
		log.Println(*statedir, "statekeeper initialized")
	}

	var listener net.Listener
	if *ssl {
		cert, err := tls.LoadX509KeyPair(*sslCert, *sslKey)
		if err != nil {
			log.Fatalf("Could not load SSL keys from %s and %s: %s", *sslCert, *sslKey, err)
		}
		config := tls.Config{Certificates: []tls.Certificate{cert}}
		listener, err = tls.Listen("tcp", *bind, &config)
		if err != nil {
			log.Fatalf("Can not listen on %s: %v", *bind, err)
		}
	} else {
		var err error
		listener, err = net.Listen("tcp", *bind)
		if err != nil {
			log.Fatalf("Can not listen on %s: %v", *bind, err)
		}
	}
	log.Println("Listening on", *bind)

	go daemon.Processor(events)
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Println("Error during accepting connection", err)
			continue
		}
		client = NewClient(*hostname, conn)
		go client.Processor(events)
	}
}

func main() {
	flag.Parse()
	Run()
}
