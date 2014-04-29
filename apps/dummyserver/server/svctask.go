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
ServiceTask: a dummy application service running inside server
         recv a request string and return response (request_string + "[processed: trans number]")
         a real server can have multiple ServiceTasks for diff services
ServiceTask has the following messaging interface:
   A> messages sent
      /App/ServiceName/Response
      /Fault/AppService/Exception
      /DB/Request
   B> messages recved
      /App/ServiceName/Request
      /Fault/Command/App/ServiceName
      /DB/Response/App/ServiceName
      /Sys/Command
*/
type ServiceTask struct {
	//output_intf or send chans
	dbReqChan   chan *DbReq
	svcRespChan chan string
	*router.FaultRaiser
	//input_intf or recv chans
	svcReqChan chan string
	sysCmdChan chan string
	svcCmdChan chan string
	dbRespChan chan string
	//some private state
	rot         router.Router
	name        string //unique ServiceName
	servantName string
	numTrans    int //how many requests have been handled
	role        ServantRole
	random      *rand.Rand //for generating fake db requests and fake fault report
}

func NewServiceTask(r router.Router, sn string, n string, role ServantRole) *ServiceTask {
	st := &ServiceTask{}
	go st.Run(r, sn, n, role)
	return st
}

func (at *ServiceTask) Run(r router.Router, sn string, n string, role ServantRole) {
	fmt.Println("App task [", n, "] at [", sn, "] starts")
	//init phase
	at.init(r, sn, n, role)
	//service mainLoop
	cont := true
	svcCmdChan := at.svcCmdChan
	svcReqChan := at.svcReqChan
	for cont {
		select {
		case cmd, sysOpen := <-at.sysCmdChan:
			if sysOpen {
				cont = at.handleCmd(cmd)
			} else {
				cont = false
			}
		case req, reqOpen := <-svcReqChan:
			if reqOpen {
				if at.role == Active {
					at.handleSvcReq(req)
				}
			} else {
				//cont = false
				svcReqChan = nil
			}
		//case cmd, svcOpen := <-at.svcCmdChan:
		case cmd, svcOpen := <-svcCmdChan:
			if svcOpen {
				if at.role == Active {
					switch cmd {
					case "Reset":
						//right now, the only service command is "Reset"
						at.numTrans = 0
						fmt.Println("App Service [", at.name, "] at [", at.servantName, "] is reset")
					default:
					}
				}
			} else {
				//cont = false
				svcCmdChan = nil
			}
		}
	}
	//shutdown phase
	fmt.Println("App task [", at.name, "] at [", at.servantName, "] start shutting down...")
	at.shutdown()
	fmt.Println("App task [", at.name, "] at [", at.servantName, "] exit")
}

func (at *ServiceTask) init(r router.Router, sn string, n string, role ServantRole) {
	at.rot = r
	at.name = n
	at.servantName = sn
	at.role = role
	at.random = rand.New(rand.NewSource(time.Now().UnixNano()))
	at.svcRespChan = make(chan string)
	at.dbReqChan = make(chan *DbReq)
	at.svcReqChan = make(chan string)
	at.sysCmdChan = make(chan string)
	at.svcCmdChan = make(chan string)
	at.dbRespChan = make(chan string)
	svcname := "/App/" + at.name
	//output_intf or send chans
	at.FaultRaiser = router.NewFaultRaiser(router.StrID("/Fault/AppService/Exception"), at.rot, at.name)
	at.rot.AttachSendChan(router.StrID("/DB/Request"), at.dbReqChan)
	at.rot.AttachSendChan(router.StrID(svcname+"/Response"), at.svcRespChan)
	//input_intf or recv chans
	at.rot.AttachRecvChan(router.StrID("/Sys/Command"), at.sysCmdChan)
	at.rot.AttachRecvChan(router.StrID("/Fault/Command"+svcname), at.svcCmdChan)
	at.rot.AttachRecvChan(router.StrID("/DB/Response"+svcname), at.dbRespChan)
	at.rot.AttachRecvChan(router.StrID(svcname+"/Request"), at.svcReqChan)
}

func (at *ServiceTask) shutdown() {
	svcname := "/App/" + at.name
	//output_intf or send chans
	at.FaultRaiser.Close()
	at.rot.DetachChan(router.StrID(svcname+"/Response"), at.svcRespChan)
	at.rot.DetachChan(router.StrID("/DB/Request"), at.dbReqChan)
	//input_intf or recv chans
	at.rot.DetachChan(router.StrID(svcname+"/Request"), at.svcReqChan)
	at.rot.DetachChan(router.StrID("/Sys/Command"), at.sysCmdChan)
	at.rot.DetachChan(router.StrID("/Fault/Command"+svcname), at.svcCmdChan)
	at.rot.DetachChan(router.StrID("/DB/Response"+svcname), at.dbRespChan)
}

func (at *ServiceTask) handleCmd(cmd string) bool {
	svcname := ""
	idx := strings.Index(cmd, ":")
	if idx > 0 {
		svcname = cmd[idx+1:]
		cmd = cmd[0:idx]
	}
	cont := true
	switch cmd {
	case "Start":
		if at.role == Standby {
			//first drain old req
			more := true
			for more {
				select {
				case _, ok := <-at.svcReqChan:
					if !ok {
						more = false
						cont = false
					}
				default:
					more = false
				}
			}
			at.numTrans = 0
			at.role = Active
			fmt.Println("App Service [", at.name, "] at [", at.servantName, "] is activated")
		}
	case "Stop":
		at.role = Standby
		fmt.Println("App Service [", at.name, "] at [", at.servantName, "] is stopped")
	case "Shutdown":
		cont = false
		fmt.Println("App Service [", at.name, "] at [", at.servantName, "] is shutdown")
	case "DelService":
		if svcname == at.name {
			cont = false
			fmt.Println("App Service [", at.name, "] at [", at.servantName, "] is deleted")
		}
	default:
	}
	return cont
}

func (at *ServiceTask) handleSvcReq(req string) {
	fmt.Println("App Service [", at.name, "] at [", at.servantName, "] process req: ", req)
	//fake random database req
	r := at.random.Intn(6)
	if r == 1 {
		at.dbReqChan <- &DbReq{at.name, "give me data"}
		dbResp := <-at.dbRespChan
		if len(dbResp) == 0 {
			fmt.Println("App Service [", at.name, "] at [", at.servantName, "] standby and failed req: ", req)
			return
		}
	}
	//send response
	at.svcRespChan <- fmt.Sprintf("[%s] is processed at [%s] : transaction_id [%d]", req, at.servantName, at.numTrans)
	at.numTrans++
	//fake random fault report
	if at.numTrans > 3 {
		r = at.random.Intn(1024)
		if r == 99 {
			at.Raise(errors.New("app service got an error"))
		}
	}
}
