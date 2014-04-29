//
// Copyright (c) 2010 - 2012 Yigong Liu
//
// Distributed under New BSD License
//
package main

import (
	"bufio"
	"flag"
	"fmt"
	"net"
	"os"
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

type Chatter struct {
	name       string
	r          router.Router
	subjectMap map[string]*Subject
}

func newChatter(name string, r router.Router) *Chatter {
	c := new(Chatter)
	c.name = name
	c.r = r
	c.subjectMap = make(map[string]*Subject)
	return c
}

func (c *Chatter) Join(s string) {
	_, ok := c.subjectMap[s]
	if ok {
		return
	}
	subj := newSubject()
	c.subjectMap[s] = subj
	//use ScopeRemote to send msgs to peers in connected routers
	c.r.AttachSendChan(router.StrID(s, router.ScopeRemote), subj.sendChan)
	c.r.AttachRecvChan(router.StrID(s, router.ScopeRemote), subj.recvChan)
	//start reading/recving subject msgs
	go func() {
		for {
			msg, chOpen := <-subj.recvChan
			if !chOpen {
				break
			}
			fmt.Printf("recv: [%s]\n", msg)
		}
		fmt.Println("client recv goroutine exit")
	}()
}

func (c *Chatter) Leave(s string) {
	subj, ok := c.subjectMap[s]
	if !ok {
		return
	}
	delete(c.subjectMap, s)
	c.r.DetachChan(router.StrID(s, router.ScopeRemote), subj.recvChan)
	c.r.DetachChan(router.StrID(s, router.ScopeRemote), subj.sendChan)
}

func (c *Chatter) Send(s string, msg string) {
	subj, ok := c.subjectMap[s]
	if !ok {
		return
	}
	subj.sendChan <- fmt.Sprintf("subject[%s] : %s says %s", s, c.name, msg)
}

func main() {
	flag.Parse()
	if flag.NArg() < 3 {
		fmt.Println("Usage: chatcli chatter_name srv_name srv_port")
		return
	}
	myName := flag.Arg(0)
	srvName := flag.Arg(1)
	srvPort := flag.Arg(2)

	dialaddr := srvName + ":" + srvPort
	conn, err := net.Dial("tcp", dialaddr)
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println("client connect")

	rot := router.New(router.StrID(), 32, router.BroadcastPolicy/*, "client", router.ScopeLocal*/)
	_, err = rot.ConnectRemote(conn, router.JsonMarshaling)
	if err != nil {
		fmt.Println(err)
		return
	}

	chatter := newChatter(myName, rot)

	cont := true
	input := bufio.NewReader(os.Stdin)
	for cont {
		fmt.Println("action: 1 - Join, 2 - Leave, 3 - Send, 4 - Exit")
		action, _ := input.ReadString('\n')
		switch action[0] {
		case '1', '2', '3':
			fmt.Println("subject:")
			subj, _ := input.ReadString('\n')
			switch action[0] {
			case '1':
				chatter.Join(subj[0 : len(subj)-1])
			case '2':
				chatter.Leave(subj[0 : len(subj)-1])
			case '3':
				fmt.Println("message:")
				msg, _ := input.ReadString('\n')
				chatter.Send(subj[0:len(subj)-1], msg[0:len(msg)-1])
			default:
				fmt.Println("invalid action")
			}
		case '4':
			cont = false
		default:
			fmt.Println("invalid action")
		}
	}

	fmt.Println("client exit")
	conn.Close()
	rot.Close()
}
