//
// Copyright (c) 2010 - 2012 Yigong Liu
//
// Distributed under New BSD License
//
package main

import (
	"errors"
	"fmt"
	"math/rand"
	"github.com/go-router/router"
	"strings"
	"time"
)

/*
 DbTask: a task handling requests to backend database
 DbTask has the following messaging interface:
   A> messages sent
      /DB/Response/App/ServiceName
      /Fault/DB/Exception
   B> messages recved
      /DB/Request
      /Sys/Command
*/
type DbReq struct {
	srcName string //the sender of requests
	request string
}

type DbTask struct {
	//output_intf or send chans
	dbRespChans         map[string](chan string) //one resp chan for each ServiceTask
	*router.FaultRaiser                          //for sending "/Fault/DB/Exception" msgs
	//input_intf or recv chans
	dbReqChan  chan *DbReq
	sysCmdChan chan string
	//some private state
	random   *rand.Rand //for generating fake fault report
	rot      router.Router
	numTrans int
	role     ServantRole
	servName string
}

func NewDbTask(r router.Router, sn string, role ServantRole) *DbTask {
	dt := &DbTask{}
	go dt.Run(r, sn, role)
	return dt
}

func (dt *DbTask) Run(r router.Router, sn string, role ServantRole) {
	//init phase
	dt.init(r, sn, role)
	//mainloop
	cont := true
	for cont {
		select {
		case cmd, cmdOpen := <-dt.sysCmdChan:
			if cmdOpen {
				cont = dt.handleCmd(cmd)
			} else {
				cont = false
			}
		case req, dbOpen := <-dt.dbReqChan:
			if dbOpen {
				dt.handleDbReq(req)
			} else {
				cont = false
			}
		}
	}
	//shutdown phase
	dt.shutdown()
	fmt.Println("DbTask at [", dt.servName, "] exit")
}

func (dt *DbTask) init(r router.Router, sn string, role ServantRole) {
	dt.rot = r
	dt.role = role
	dt.servName = sn
	dt.random = rand.New(rand.NewSource(time.Now().UnixNano()))
	dt.dbRespChans = make(map[string](chan string))
	dt.dbReqChan = make(chan *DbReq)
	dt.sysCmdChan = make(chan string)
	//output_intf or send chans
	dt.FaultRaiser = router.NewFaultRaiser(router.StrID("/Fault/DB/Exception"), r, "DbTask")
	//input_intf or recv chans
	r.AttachRecvChan(router.StrID("/Sys/Command"), dt.sysCmdChan)
	//use a bindChan to keep dbReqChan open when all clients detach & exit
	bc := make(chan *router.BindEvent, 1)
	r.AttachRecvChan(router.StrID("/DB/Request"), dt.dbReqChan, bc)
}

func (dt *DbTask) shutdown() {
	//output_intf or send chans
	dt.FaultRaiser.Close()
	for k, v := range dt.dbRespChans {
		dt.rot.DetachChan(router.StrID("/DB/Response/App/"+k), v)
	}
	//input_intf or recv chans
	dt.rot.DetachChan(router.StrID("/Sys/Command"), dt.sysCmdChan)
	dt.rot.DetachChan(router.StrID("/DB/Request"), dt.dbReqChan)
}

func (dt *DbTask) handleCmd(cmd string) bool {
	cont := true
	svcname := ""
	idx := strings.Index(cmd, ":")
	if idx > 0 {
		svcname = cmd[idx+1:]
		cmd = cmd[0:idx]
	}
	switch cmd {
	case "Start":
		dt.numTrans = 0
		dt.role = Active
	case "Stop":
		dt.role = Standby
	case "Shutdown":
		cont = false
	case "AddService":
		if len(svcname) > 0 {
			ch := make(chan string)
			dt.dbRespChans[svcname] = ch
			dt.rot.AttachSendChan(router.StrID("/DB/Response/App/"+svcname), ch)
		}
	case "DelService":
		if len(svcname) > 0 {
			ch, ok := dt.dbRespChans[svcname]
			if ok {
				delete(dt.dbRespChans, svcname)
				dt.rot.DetachChan(router.StrID("/DB/Response/App/"+svcname), ch)
			}
		}
	default:
	}
	return cont
}

func (dt *DbTask) handleDbReq(req *DbReq) {
	//send response
	fmt.Println("DbTask at [", dt.servName, "] handles req from : ", req.srcName)
	ch, ok := dt.dbRespChans[req.srcName]
	if ok {
		if dt.role == Active {
			ch <- fmt.Sprintf("DbReq from [%s] is processed : db_transaction_id [%d]", req.srcName, dt.numTrans)
			dt.numTrans++
			//fake random fault report
			if dt.numTrans > 3 {
				r := dt.random.Intn(1024)
				if r == 31 {
					fmt.Println("DbTask at [", dt.servName, "] report fault")
					dt.Raise(errors.New("DbTask got an error"))
				}
			}
		} else { //return empty string "" telling client we are standby
			ch <- ""
		}
	}
}
