go-router
=========
"router" is a Go package for remote peer-peer publish/subscribe message passing. We attach a send chan to an id in router to send msgs, and attach a recv chan to an id to recv msgs. If these 2 ids match, the msgs from send chan will be "routed" to recv chan, e.g.

         rot := router.New(...)
         chan1 := make(chan string)
         chan2 := make(chan string)
         chan3 := make(chan string)
         rot.AttachSendChan(PathID("/sports/basketball"), chan1)
         rot.AttachRecvChan(PathID("/sports/basketball"), chan2)
         rot.AttachRecvChan(PathID("/sports/*"), chan3)

We can use integers, strings, pathnames, or structs as Ids in router (maybe regex ids and tuple id in future).

we can connect two routers at two diff machines so that chans attached to routerA can communicate with chans attached to routerB transparently.

In docs, there are more detailed Tutorial and UserGuide; also notes about an experiment to implement highly available services. There are some sample apps: chat, ping-pong, dummy-server.

Installation.

        go get github.com/yigongliu/go-router/router

Example.

        package main

        import (
               "fmt"
               "github.com/yigongliu/go-router/router"
        )

        func main() {
             rot := router.New(router.StrID(), 32, router.BroadcastPolicy)
             chin := make(chan int)
             chout := make(chan int)
             rot.AttachSendChan(router.StrID("A"), chin)
             rot.AttachRecvChan(router.StrID("A"), chout)
             go func() {
                for i:=0; i<=10; i++ {
                    chin <- i;
                }
                close(chin)
             }()
             for v := range chout {
                 fmt.Println("recv ", v)
             }
        }


--- moved from https://code.google.com/p/go-router/ ---
