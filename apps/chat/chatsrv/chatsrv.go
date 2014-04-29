//
// Copyright (c) 2010 - 2012 Yigong Liu
// Distributed under New BSD License
//
package main

import (
	"fmt"
	"net"
	"github.com/go-router/router"
)

type Subject struct {
	sendChan chan string
	recvChan chan string
}

func newSubject() *Subject {
	s := new(Subject)
	s.sendChan = make(chan string)
	s.recvChan = make(chan string)
	return s
}

func main() {
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(l.Addr().String())

	subjMap := make(map[string]*Subject)

	rot := router.New(router.StrID(), 32, router.BroadcastPolicy/*, "chatsrv", router.ScopeLocal*/)

	//start server mainloop in a separate goroutine, and recv client conn in main goroutine
	go func() {
		//subscribe to remote publications, so learn what subjects are created
		pubChan := make(chan *router.ChanInfoMsg)
		//attach a recv chan with a "chan BindEvent"
		//this recv chan will not be closed when all senders detach
		bindChan := make(chan *router.BindEvent, 1)
		rot.AttachRecvChan(rot.NewSysID(router.PubId, router.ScopeRemote), pubChan, bindChan)
		//stopChan to notify when all people leave a subject
		stopChan := make(chan string, 36)

		for {
			select {
			case idstr := <-stopChan:
				delete(subjMap, idstr)
			case pub := <-pubChan:
				//process recved client publication of subjects
				for _, v := range pub.Info {
					id := v.Id.(*router.StrId) //get the real id type
					subj, ok := subjMap[id.Val]
					if ok {
						continue
					}
					fmt.Printf("add subject: %v\n", id.Val)
					//add a new subject with ScopeRemote, so that msgs are forwarded
					//to peers in connected routers
					id.ScopeVal = router.ScopeRemote
					id.MemberVal = router.MemberLocal
					subj = newSubject()
					subjMap[id.Val] = subj
					//subscribe to new subjects, forward recved msgs to other
					rot.AttachSendChan(id, subj.sendChan)
					rot.AttachRecvChan(id, subj.recvChan)
					//start forwarding
					go func(subjname string) {
						for val := range subj.recvChan {
							fmt.Printf("chatsrv forward: subject[%v], msg[%s]\n", subjname, val)
							subj.sendChan <- val
						}
						stopChan <- subjname
						fmt.Printf("chatsrv stop forwarding for : %v\n", subjname)
					}(id.Val)
				}
			}
		}
	}()

	//keep accepting client conn and connect local router to it
	for {
		conn, err := l.Accept()
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Println("one client connect")

		_, err = rot.ConnectRemote(conn, router.JsonMarshaling)
		if err != nil {
			fmt.Println(err)
			return
		}
	}

	//in fact never reach here
	rot.Close()
	l.Close()
}
