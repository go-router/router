//
// Copyright (c) 2010 - 2012 Yigong Liu
//
// Distributed under New BSD License
//
package main

import (
	"flag"
	"fmt"
	"net"
	"github.com/go-router/router"
	"strconv"
	"time"
)

const (
	ServantAddr1 = "/tmp/dummy.servant.1"
	ServantAddr2 = "/tmp/dummy.servant.2"
)

func main() {
	flag.Parse()
	if flag.NArg() < 2 {
		fmt.Println("Usage: client ServiceName numRuns")
		return
	}
	svcName := flag.Arg(0)
	numRuns, _ := strconv.Atoi(flag.Arg(1))

	conn1, _ := net.Dial("unix", ServantAddr1)
	fmt.Println("conn to servant1 up")
	conn2, _ := net.Dial("unix", ServantAddr2)
	fmt.Println("conn to servant2 up")

	//create router and connect it to both active and standby servants
	rot := router.New(router.StrID(), 32, router.BroadcastPolicy /* , "cli", router.ScopeLocal*/ )
	proxy1 := router.NewProxy(rot, "proxy1", nil, nil)
	proxy2 := router.NewProxy(rot, "proxy2", nil, nil)
	proxy1.ConnectRemote(conn1, router.GobMarshaling, router.XOnOffFlowController)
	proxy2.ConnectRemote(conn2, router.GobMarshaling, router.XOnOffFlowController)

	reqChan := make(chan string)
	rspChan := make(chan string)
	bindChan := make(chan *router.BindEvent, 1)
	rot.AttachSendChan(router.StrID("/App/"+svcName+"/Request"), reqChan, bindChan)
	rot.AttachRecvChan(router.StrID("/App/"+svcName+"/Response"), rspChan)
	//make sure client connect to 2 servants before sending requests
	for {
		if (<-bindChan).Count > 1 {
			break
		}
	}
	cont := true
	for i := 0; i < numRuns && cont; i++ {
		req := fmt.Sprintf("request %d", i)
		fmt.Printf("client sent request [%s] to serivce [%s]\n", req, svcName)
		reqChan <- req
		timer := time.NewTimer(6e8) //the wait for response will time out in less than 1 sec
		select {
		case rsp, chOpen := <-rspChan:
			if !chOpen {
				fmt.Println("client response closed, exit")
				cont = false
			} else {
				fmt.Printf("client recv response ( %s )\n", rsp)
			}
		case <-timer.C:
			fmt.Printf("time out for reqest [%s]\n", req)
			i-- //resend it
		}
		timer.Stop()
	}
	fmt.Printf("client exit\n")
	conn1.Close()
	conn2.Close()
	rot.Close()
}
