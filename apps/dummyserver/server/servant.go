//
// Copyright (c) 2010 - 2012 Yigong Liu
//
// Distributed under New BSD License
//
package main

import (
	"fmt"
	"net"
	"os"
	"github.com/go-router/router"
	"strings"
)

type ServantRole int

const (
	Standby ServantRole = iota
	Active
)

func (s ServantRole) String() string {
	switch s {
	case Active:
		return "Active"
	case Standby:
		return "Standby"
	}
	return "InvalidRole"
}

//filter at conn between client and server
type ClientFilter struct {
	blockedPrefix []string
}

func (f *ClientFilter) BlockInward(id0 router.Id) bool {
	return f.blocked(id0)
}

func (f *ClientFilter) BlockOutward(id0 router.Id) bool {
	return f.blocked(id0)
}

func (f *ClientFilter) blocked(id0 router.Id) bool {
	id := id0.(*router.StrId)
	for _, v := range f.blockedPrefix {
		if strings.HasPrefix(id.Val, v) {
			return true
		}
	}
	return false
}

/*
 Servant
*/
const (
	ServantAddr1 = "/tmp/dummy.servant.1"
	ServantAddr2 = "/tmp/dummy.servant.2"
)

type Servant struct {
	Rot  router.Router
	role ServantRole
	name string
}

func NewServant(n string, role ServantRole, done chan bool) *Servant {
	s := new(Servant)
	s.Rot = router.New(router.StrID(), 32, router.BroadcastPolicy /* , n, router.ScopeLocal*/ )
	s.role = role
	s.name = n
	//start system tasks, ServiceTask will be created when clients connect
	NewSysMgrTask(s.Rot, n, role)
	NewDbTask(s.Rot, n, role)
	NewFaultMgrTask(s.Rot, n, role)
	//run Servant mainloop to wait for client connection
	go s.Run(done)
	return s
}

func (s *Servant) Run(done chan bool) {
	addr := ServantAddr1
	if s.role == Standby {
		addr = ServantAddr2
	}
	os.Remove(addr)
	l, _ := net.Listen("unix", addr)
	
	//blocked all internal ctrl msgs
	clientFilter := &ClientFilter{[]string{"/Sys/","/Fault/","/DB/"}}

	//keep accepting client conn and connect local router to it
	for {
		conn, err := l.Accept()
		if err != nil {
			fmt.Println(err)
			continue
		}
		fmt.Println(s.name, "connect one client")

		//proxy use flow control
		//proxies will be cleaned up when client disconnect
		proxy := router.NewProxy(s.Rot, "", clientFilter, nil)
		err = proxy.ConnectRemote(conn, router.GobMarshaling, router.XOnOffFlowController)
		if err != nil {
			fmt.Println(err)
			continue
		}
	}

	//in fact never reach here
	s.Rot.Close()
	l.Close()

	done <- true
}
