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
	"time"
)

/*
 SystemManagerTask: perform overall system management
 SysteManager at standby servant monitors the heartbeat from active server, if 2 heartbeat miss in a row, standby will become active and start serving user requests
 SystemManager at active servant will send heartbeats to standby, send command msgs to subordinate tasks to control their life cycle
 SystemManager will monitor client connections and create ServiceTask on demand
 SysMgrTask has the following messaging interface:
 A> messages sent
 /Sys/Ctrl/Heartbeat
 /Sys/Command
 B> messages recved
 /Sys/Ctrl/Heartbeat
 /Sys/OutOfService
*/
type SysMgrTask struct {
	//output_intf or send chans
	htbtSendChan chan time.Time
	sysCmdChan   chan string
	//input_intf or recv chans
	htbtRecvChan chan time.Time
	sysOOSChan   chan string
	//private state
	role          ServantRole
	rot           router.Router
	name          string
	childBindChan chan *router.BindEvent
	stopChan      chan bool
	startChan     chan bool
	//remote pub/unpub chans monitor client join/leave
	rmtPubChan   chan *router.ChanInfoMsg
	rmtUnpubChan chan *router.ChanInfoMsg
	//local unpub chans monitor local service clean up when client disconn
	locUnpubChan  chan *router.ChanInfoMsg
	pubBindChan   chan *router.BindEvent
	unpubBindChan chan *router.BindEvent
	//map of active services and its exit channels
	//to avoid starting duplicate service, and
	//for stress testing, when continuously starting a service in script
	//make sure the last instance of same service has exited cleanly
	exitChanMap map[string]chan bool
}

func NewSysMgrTask(r router.Router, n string, role ServantRole) *SysMgrTask {
	smt := &SysMgrTask{}
	go smt.Run(r, n, role)
	return smt
}

func (smt *SysMgrTask) Run(r router.Router, n string, role ServantRole) {
	//init phase
	smt.init(r, n, role)
	//wait for child tasks to bind, at least two (DB, Fault)
	for {
		if (<-smt.childBindChan).Count >= 2 {
			break
		}
	}
	if smt.role == Active {
		//in active servant
		//ask all child tasks coming up to service
		smt.sysCmdChan <- "Start"
		//start sending heartbeat to standby
		go smt.sendHeartbeat()
	} else {
		//in standby servant
		//start monitoring heartbeats from active servant
		go smt.monitorActiveHeartbeat()
	}
	//service mainLoop
	//fmt.Println("SysMgrTask [", n, "] starts mainloop")
	cont := true
	for cont {
		select {
		case _ = <-smt.startChan:
			//at standby servant, SysMgrTask will monitor heartbeats
			//active servant stopped, change my role to active
			smt.role = Active
			//fmt.Println("servant wake up 1")
			//cleanup, drain old out-of-date OOS msgs
		L:
			for {
				select {
				case _ = <-smt.sysOOSChan:
				default:
					break L
				}
			}
			//fmt.Println("servant wake up 2")
			//ask all child tasks coming up to service
			smt.sysCmdChan <- "Start"
			//fmt.Println("servant wake up 3")
			//start sending heartbeat to standby
			go smt.sendHeartbeat()
			fmt.Println("!!!! Servant [", smt.name, "] come up in service ...")
		case _, oosOpen := <-smt.sysOOSChan:
			//at active servant, per request from FaultMgrTask, SysMgrTask can put the servant
			//out of service by stopping heartbeating and asking subordinate tasks to stop
			if oosOpen {
				smt.role = Standby
				smt.stopHeartbeat() //1 second later, standby will come up
				fmt.Println("xxxx Servant [", smt.name, "] will take a break and standby ...")
				//start monitoring heartbeats from active servant
				go smt.monitorActiveHeartbeat()
				smt.sysCmdChan <- "Stop" //?move into monitorActiveHeartbeat() wait for active coming up
			} else {
				fmt.Println("error: OOS chan closed")
				cont = false
			}
		case pub, pubOpen := <-smt.rmtPubChan:
			if pubOpen {
				for _, v := range pub.Info {
					id := v.Id.(*router.StrId)
					//fmt.Printf("%s get pubMsg id:%s\n", smt.name, id)
					data := strings.Split(id.Val, "/")
					//fmt.Printf("%s split got:%s\n", smt.name, data)
					if data[1] == "App" && data[3] == "Request" {
						svcName := data[2]
						exitCh, ok := smt.exitChanMap[svcName]
						if ok {
							//wait for old instances at both servants to exit
							to := time.NewTimer(30e9)
							select {
							case <-exitCh:
								fmt.Printf("%s OK to AddService:%s, old instance cleaned up\n", smt.name, svcName)
							case <-to.C:
								fmt.Printf("%s failed to AddService:%s, old instance not exit/cleanup, time out\n", smt.name, svcName)
								continue
							}
							to.Stop()
						} else {
							exitCh = make(chan bool, 3)
							smt.exitChanMap[svcName] = exitCh
						}
						smt.sysCmdChan <- fmt.Sprintf("AddService:%s", svcName)
						fmt.Printf("%s AddService:%s\n", smt.name, svcName)
						NewServiceTask(smt.rot, smt.name, svcName, smt.role)
					}
				}
			} else {
				fmt.Println("error: rmtPubChan closed")
				cont = false
			}
		case unpub, unpubOpen := <-smt.rmtUnpubChan:
			if unpubOpen {
				for _, v := range unpub.Info {
					id := v.Id.(*router.StrId)
					data := strings.Split(id.Val, "/")
					if data[1] == "App" && data[3] == "Request" {
						smt.sysCmdChan <- fmt.Sprintf("DelService:%s", data[2])
						fmt.Printf("%s DelService:%s\n", smt.name, data[2])
					}
				}
			} else {
				fmt.Println("error: rmtUnpubChan closed")
				cont = false
			}
		case unpub2, unpubOpen2 := <-smt.locUnpubChan:
			if unpubOpen2 {
				for _, v := range unpub2.Info {
					id := v.Id.(*router.StrId)
					data := strings.Split(id.Val, "/")
					if data[1] == "App" && data[3] == "Response" {
						//one service exits and namespace is cleaned up
						smt.exitChanMap[data[2]] <- true
						fmt.Printf("%s Service[%s] exit and cleanup\n", smt.name, data[2])
					}
				}
			} else {
				fmt.Println("error: locUnpubChan closed")
				cont = false
			}
		}
	}
	//shutdown phase
	smt.shutdown()
	fmt.Println("SysMgr at [", smt.name, "] exit")
}

func (smt *SysMgrTask) init(r router.Router, n string, role ServantRole) {
	smt.rot = r
	smt.name = n
	smt.role = role
	smt.htbtSendChan = make(chan time.Time)
	smt.htbtRecvChan = make(chan time.Time)
	smt.sysCmdChan = make(chan string)
	smt.sysOOSChan = make(chan string)
	smt.childBindChan = make(chan *router.BindEvent, 1)
	smt.startChan = make(chan bool, 1)
	smt.stopChan = make(chan bool, 1)
	smt.exitChanMap = make(map[string]chan bool)
	//output_intf or send chans
	smt.rot.AttachSendChan(router.StrID("/Sys/Command"), smt.sysCmdChan, smt.childBindChan)
	smt.rot.AttachSendChan(router.StrID("/Sys/Ctrl/Heartbeat", router.ScopeRemote), smt.htbtSendChan)
	//input_intf or recv chans
	smt.rot.AttachRecvChan(router.StrID("/Sys/Ctrl/Heartbeat", router.ScopeRemote), smt.htbtRecvChan)
	smt.rot.AttachRecvChan(router.StrID("/Sys/OutOfService"), smt.sysOOSChan)
	//
	smt.rmtPubChan = make(chan *router.ChanInfoMsg)
	smt.rmtUnpubChan = make(chan *router.ChanInfoMsg)
	smt.locUnpubChan = make(chan *router.ChanInfoMsg)
	//use pubBindChan/unpubBindChan when attaching chans to PubId/UnPubId, so that they will not be
	//closed when all clients close and leave
	smt.pubBindChan = make(chan *router.BindEvent, 1)
	smt.unpubBindChan = make(chan *router.BindEvent, 1)
	smt.rot.AttachRecvChan(smt.rot.NewSysID(router.PubId, router.ScopeRemote), smt.rmtPubChan, smt.pubBindChan)
	smt.rot.AttachRecvChan(smt.rot.NewSysID(router.UnPubId, router.ScopeRemote), smt.rmtUnpubChan, smt.unpubBindChan)
	smt.rot.AttachRecvChan(smt.rot.NewSysID(router.UnPubId, router.ScopeLocal), smt.locUnpubChan)
}

func (smt *SysMgrTask) shutdown() {
	//output_intf or send chans
	smt.rot.DetachChan(router.StrID("/Sys/Command"), smt.sysCmdChan)
	smt.rot.DetachChan(router.StrID("/Sys/Ctrl/Heartbeat", router.ScopeRemote), smt.htbtSendChan)
	//input_intf or recv chans
	smt.rot.DetachChan(router.StrID("/Sys/Ctrl/Heartbeat", router.ScopeRemote), smt.htbtRecvChan)
	smt.rot.DetachChan(router.StrID("/Sys/OutOfService"), smt.sysOOSChan)
	//
	smt.rot.DetachChan(smt.rot.NewSysID(router.PubId, router.ScopeRemote), smt.rmtPubChan)
	smt.rot.DetachChan(smt.rot.NewSysID(router.UnPubId, router.ScopeRemote), smt.rmtUnpubChan)
	smt.rot.DetachChan(smt.rot.NewSysID(router.UnPubId, router.ScopeLocal), smt.locUnpubChan)
}

//standby servant will monitor heartbeat from active Servant, if missing 2 in a row, come up active
func (smt *SysMgrTask) monitorActiveHeartbeat() {
	fmt.Println(smt.name, " enter monitor heartbeat")
	miss := 0
	//first block wait for active servant coming up
	<-smt.htbtRecvChan
	//drain remaining out-of-date heartbeats, if the standby coming up late
L1:
	for {
		select {
		case _ = <-smt.htbtRecvChan:
		default:
			break L1
		}
	}
	//start heartbeat monitoring, standby servant should recv about 4-5 heartbeats per second from
	//active servant, otherwise, it will come up as active
L2:
	for {
		time.Sleep(2e8)
		select {
		case _ = <-smt.htbtRecvChan:
			//fmt.Println("standby [", smt.name, "] recv heartbeat from active")
			miss = 0
		default:
			miss++
			if miss > 1 {
				break L2
			}
		}
	}
	fmt.Println(smt.name, " exit monitor heartbeat")
	smt.startChan <- true
}

func (smt *SysMgrTask) sendHeartbeat() {
	fmt.Println(smt.name, " enter send heartbeat")
	//send a heartbeat to standby servant every 1/5 second
L1:
	for {
		//check if should stop heartbeating
		select {
		case _ = <-smt.stopChan:
			break L1
		default:
		}
		//send heartbeat
		select {
		case smt.htbtSendChan <- time.Now().Local():
		default:
		}
		time.Sleep(2e8)
	}
	fmt.Println(smt.name, " exit send heartbeat")
}

func (smt *SysMgrTask) stopHeartbeat() { smt.stopChan <- true }
