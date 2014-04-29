//
// Copyright (c) 2010 - 2012 Yigong Liu
//
// Distributed under New BSD License
//
package main

import (
	"fmt"
	"github.com/go-router/router"
	"strings"
)

/*
 FaultMgrTask:
 1. recv fault reports from other tasks
 2. do fault correlation and handling
 3. if system become unusable, ask SysMgrTask to go out-of-service (OOS)
 4. often we have a hierarchy of fault managers, low level fault managers handle local faults
 and report to higher level if the faults cannot be handled locally
 FaultMgrTask has the following messaging interface:
   A> messages sent
      /Sys/OutOfService
      /Fault/Command/App/ServiceName
   B> messages recved
      /Fault/DB/Exception
      /Fault/AppService/Exception
      /Sys/Command
*/

type FaultMgrTask struct {
	//output_intf or send chans
	svcCmdChans map[string](chan string) //one for each ServiceTask
	sysOOSChan  chan string
	//input_intf or recv chans
	faultReportChan chan *router.FaultRecord
	sysCmdChan      chan string
	//private data
	role     ServantRole
	rot      router.Router
	servName string
}

func NewFaultMgrTask(r router.Router, sn string, role ServantRole) *FaultMgrTask {
	ft := &FaultMgrTask{}
	go ft.Run(r, sn, role)
	return ft
}

func (ft *FaultMgrTask) Run(r router.Router, sn string, role ServantRole) {
	//init phase
	ft.init(r, sn, role)
	//mainloop
	cont := true
	for cont {
		select {
		case cmd, cmdOpen := <-ft.sysCmdChan:
			if cmdOpen {
				cont = ft.handleCmd(cmd)
			} else {
				cont = false
			}
		case rep, frOpen := <-ft.faultReportChan:
			if frOpen {
				if ft.role == Active { //only handle fault in active mode
					ft.handleFaultReport(rep)
				}
			} else {
				cont = false
			}
		}
	}
	//shutdown phase
	ft.shutdown()
	fmt.Println("Fault manager at [", ft.servName, "] exit")
}

func (ft *FaultMgrTask) init(r router.Router, sn string, role ServantRole) {
	ft.rot = r
	ft.role = role
	ft.servName = sn
	ft.svcCmdChans = make(map[string](chan string))
	ft.sysOOSChan = make(chan string)
	ft.faultReportChan = make(chan *router.FaultRecord)
	ft.sysCmdChan = make(chan string)
	ft.rot.AttachSendChan(router.StrID("/Sys/OutOfService"), ft.sysOOSChan)
	ft.rot.AttachRecvChan(router.StrID("/Sys/Command"), ft.sysCmdChan)
	//use a bindChan to keep faultReportChan open when all clients exit
	bc := make(chan *router.BindEvent, 1)
	ft.rot.AttachRecvChan(router.StrID("/Fault/DB/Exception"), ft.faultReportChan, bc)
	ft.rot.AttachRecvChan(router.StrID("/Fault/AppService/Exception"), ft.faultReportChan, bc)
}

func (ft *FaultMgrTask) shutdown() {
	for k, v := range ft.svcCmdChans {
		ft.rot.DetachChan(router.StrID("/Fault/Command/App/"+k), v)
	}
	ft.rot.DetachChan(router.StrID("/Sys/OutOfService"), ft.sysOOSChan)
	ft.rot.DetachChan(router.StrID("/Fault/DB/Exception"), ft.faultReportChan)
	ft.rot.DetachChan(router.StrID("/Fault/AppService/Exception"), ft.faultReportChan)
	ft.rot.DetachChan(router.StrID("/Sys/Command"), ft.sysCmdChan)
}

func (ft *FaultMgrTask) handleCmd(cmd string) bool {
	cont := true
	svcname := ""
	idx := strings.Index(cmd, ":")
	if idx > 0 {
		svcname = cmd[idx+1:]
		cmd = cmd[0:idx]
	}
	switch cmd {
	case "Start":
		ft.role = Active
	case "Stop":
		ft.role = Standby
	case "Shutdown":
		cont = false
	case "AddService":
		if len(svcname) > 0 {
			ch := make(chan string)
			ft.svcCmdChans[svcname] = ch
			ft.rot.AttachSendChan(router.StrID("/Fault/Command/App/"+svcname), ch)
		}
	case "DelService":
		if len(svcname) > 0 {
			ch, ok := ft.svcCmdChans[svcname]
			if ok {
				delete(ft.svcCmdChans, svcname)
				ft.rot.DetachChan(router.StrID("/Fault/Command/App/"+svcname), ch)
			}
		}
	default:
	}
	return cont
}

func (ft *FaultMgrTask) handleFaultReport(rep *router.FaultRecord) {
	if rep.Source == "DbTask" {
		//fault at DbTask will fail the whole system
		fmt.Println("fault manager at [", ft.servName, "] report OOS")
		ft.sysOOSChan <- "OOS"
	} else {
		//fault at ServiceTask is recoverable, just reset it:)
		ch, ok := ft.svcCmdChans[rep.Source]
		if ok {
			ch <- "Reset"
		}
	}
}
