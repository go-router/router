go-router
=========
"router" is a Go package for distributed peer-peer publish/subscribe message passing. We attach a send chan to an id in router to send msgs, and attach a recv chan to an id to recv msgs. If these 2 ids match, the msgs from send chan will be "routed" to recv chan, e.g.

         rot := router.New(...)
         chan1 := make(chan string)
         chan2 := make(chan string)
         chan3 := make(chan string)
         rot.AttachSendChan(PathID("/sports/basketball"), chan1)
         rot.AttachRecvChan(PathID("/sports/basketball"), chan2)
         rot.AttachRecvChan(PathID("/sports/*"), chan3)

We can use integers, strings, pathnames, or structs as Ids in router (maybe regex ids and tuple id in future).

we can connect two routers at two diff machines so that chans attached to routerA can communicate with chans attached to routerB transparently.

In wikis, there are more detailed [Tutorial](https://github.com/go-router/router/wiki/Tutorial) and [UserGuide](https://github.com/go-router/router/wiki/User-Guide); also [notes about an experiment to implement highly available services](https://github.com/go-router/router/wiki/a-dummy-server). There are some sample apps: [chat](https://github.com/go-router/router/tree/master/apps/chat), [ping-pong](https://github.com/go-router/router/tree/master/apps/pingpong), [dummy-server](https://github.com/go-router/router/tree/master/apps/dummyserver).

Installation.

        go get github.com/go-router/router

Example.

        package main

        import (
               "fmt"
               "github.com/go-router/router"
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

App [ping-pong](https://github.com/go-router/router/tree/master/apps/pingpong) shows how router allows pinger/ponger goroutines remain unchanged while their connections change from local channels, to routers connected thru unix domain sockets or tcp sockets.


--- moved from https://code.google.com/p/go-router/ ---
