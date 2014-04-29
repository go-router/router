//
// Copyright (c) 2010 - 2012 Yigong Liu
//
// Distributed under New BSD License
//

package main

import (
	"fmt"
	"net"
	"github.com/go-router/router"
	"strings"
	"time"
)

func test_IntId() {
	rout := router.New(router.IntID(), 32, router.BroadcastPolicy)
	chi1 := make(chan string)
	chi2 := make(chan string)
	cho1 := make(chan string)
	cho2 := make(chan string)
	done := make(chan bool)
	rout.AttachSendChan(router.IntID(10), cho1)
	rout.AttachSendChan(router.IntID(10), cho2)
	rout.AttachRecvChan(router.IntID(10), chi1)
	rout.AttachRecvChan(router.IntID(10), chi2)
	go func() {
		cho1 <- "hello"
		cho1 <- "from IntID/src1"
		close(cho1)
	}()
	go func() {
		for v := range chi1 {
			fmt.Println("IntId/sink1 got: ", v)
		}
		done <- true
	}()
	go func() {
		for v := range chi2 {
			fmt.Println("IntId/sink2 got: ", v)
		}
		done <- true
	}()
	go func() {
		cho2 <- "hello"
		cho2 <- "from IntID/src2"
		close(cho2)
	}()
	<-done
	<-done
}

func test_StrId() {
	rout := router.New(router.StrID(), 32, router.BroadcastPolicy)
	chi1 := make(chan string)
	chi2 := make(chan string)
	cho := make(chan string)
	done := make(chan bool)
	rout.AttachSendChan(router.StrID("test"), cho)
	rout.AttachRecvChan(router.StrID("test"), chi1)
	rout.AttachRecvChan(router.StrID("test"), chi2)
	go func() {
		cho <- "hello"
		cho <- "from StrID"
		close(cho)
	}()
	go func() {
		for v := range chi1 {
			fmt.Println("StrId/sink1 got: ", v)
		}
		done <- true
	}()
	go func() {
		for v := range chi2 {
			fmt.Println("StrId/sink2 got: ", v)
		}
		done <- true
	}()
	<-done
	<-done
}

func test_PathId() {
	rout := router.New(router.PathID(), 32, router.BroadcastPolicy)
	chi1 := make(chan string)
	chi2 := make(chan string)
	chi3 := make(chan string)
	cho1 := make(chan string)
	cho2 := make(chan string)
	cho3 := make(chan string)
	done := make(chan bool)
	rout.AttachSendChan(router.PathID("/sport/basketball"), cho1)
	rout.AttachSendChan(router.PathID("/sport/baseball"), cho2)
	rout.AttachSendChan(router.PathID("/sport/basketball/Jordan"), cho3)
	rout.AttachRecvChan(router.PathID("/sport/*"), chi1) //subscribe to all sports news
	rout.AttachRecvChan(router.PathID("/sport/baseball"), chi2)
	rout.AttachRecvChan(router.PathID("/sport/basketball*"), chi3) //subscribe to all basketball news
	go func() {
		cho1 <- "want to play"
		cho1 <- "basketball?"
		close(cho1)
	}()
	go func() {
		cho2 <- "want to watch"
		cho2 <- "baseball?"
		close(cho2)
	}()
	go func() {
		cho3 <- "hello there, anybody know"
		cho3 <- "will Michael Jordan play again?"
		close(cho3)
	}()
	go func() {
		for v := range chi1 {
			fmt.Println("sport fan read: ", v)
		}
		done <- true
	}()
	go func() {
		for v := range chi2 {
			fmt.Println("baseball fan read: ", v)
		}
		done <- true
	}()
	go func() {
		for v := range chi3 {
			fmt.Println("basketball fan read: ", v)
		}
		done <- true
	}()
	<-done
	<-done
	<-done
}

func test_MsgId() {
	rout := router.New(router.MsgID(), 32, router.BroadcastPolicy)
	chi1 := make(chan string)
	chi2 := make(chan string)
	cho := make(chan string)
	done := make(chan bool)
	rout.AttachSendChan(router.MsgID(10, 20), cho)
	rout.AttachRecvChan(router.MsgID(10, 20), chi1)
	rout.AttachRecvChan(router.MsgID(10, 20), chi2)
	go func() {
		cho <- "hello"
		cho <- "from MsgID"
		close(cho)
	}()
	go func() {
		for v := range chi1 {
			fmt.Println("MsgId/sink1 got: ", v)
		}
		done <- true
	}()
	go func() {
		for v := range chi2 {
			fmt.Println("MsgId/sink2 got: ", v)
		}
		done <- true
	}()
	<-done
	<-done
}

func test_notification() {
	rout := router.New(router.StrID(), 32, router.BroadcastPolicy)
	chi1 := make(chan string)
	chi2 := make(chan string)
	chiN := make(chan *router.ChanInfoMsg)
	cho := make(chan string)
	bound := make(chan *router.BindEvent, 1)
	done := make(chan bool)
	//subscribe to recver attach events
	rout.AttachRecvChan(rout.SysID(router.SubId), chiN)
	//
	rout.AttachSendChan(router.StrID("test"), cho, bound)
	rout.AttachRecvChan(router.StrID("test"), chi1)
	rout.AttachRecvChan(router.StrID("test"), chi2)
	//wait for two recvers to connect
	for {
		if (<-bound).Count == 2 {
			break
		}
	}
	go func() {
		cho <- "hello1"
		cho <- "hello2"
		cho <- "hello3"
		cho <- "from notif"
		close(cho)
	}()
	go func() {
		for v := range chi1 {
			fmt.Println("notif/sink1 got: ", v)
		}
		done <- true
	}()
	go func() {
		for v := range chi2 {
			fmt.Println("notif/sink2 got: ", v)
			rout.DetachChan(router.StrID("test"), chi2)
		}
		done <- true
	}()
	go func() {
		count := 0
		for m := range chiN {
			for _, v := range m.Info {
				fmt.Println("got sub notif: ", v.Id)
			}
			count++
			if count >= 2 {
				break
			}
		}
		done <- true
	}()
	<-done
	<-done
	<-done
	rout.Close()
}

func test_local_conn() {
	rout1 := router.New(router.IntID(), 32, router.BroadcastPolicy /* , "router1", router.ScopeLocal*/ )
	rout2 := router.New(router.IntID(), 32, router.BroadcastPolicy /* , "router2", router.ScopeLocal*/ )
	rout1.Connect(rout2)
	chi1 := make(chan string)
	chi2 := make(chan string)
	chi3 := make(chan string)
	chiN := make(chan *router.ChanInfoMsg)
	cho := make(chan string)
	done := make(chan bool)
	bound := make(chan *router.BindEvent, 1)
	//subscribe to recver attach events
	rout1.AttachRecvChan(rout1.SysID(router.SubId), chiN)
	//when attaching sending chan, add a (chan BindEvent) for notifying recver connecting
	rout1.AttachSendChan(router.IntID(10), cho, bound)
	rout1.AttachRecvChan(router.IntID(10), chi1)
	rout2.AttachRecvChan(router.IntID(10), chi2)
	rout2.AttachRecvChan(router.IntID(10), chi3)
	//wait for some recvers connecting
	for {
		if (<-bound).Count == 2 {
			break
		}
	}
	go func() {
		cho <- "hello1"
		cho <- "hello2"
		cho <- "hello3"
		cho <- "from router1/src"
		close(cho)
	}()
	go func() {
		for v := range chi1 {
			fmt.Println("router1/sink1 got: ", v)
		}
		done <- true
	}()
	go func() {
		for v := range chi2 {
			fmt.Println("router2/sink2 got: ", v)
		}
		done <- true
	}()
	go func() {
		i := 0
		for v := range chi3 {
			fmt.Println("router2/sink3 got: ", v)
			i++
			if i == 2 {
				rout2.DetachChan(router.IntID(10), chi3)
			}
		}
		done <- true
	}()
	go func() {
		count := 0
		for m := range chiN {
			for _, v := range m.Info {
				fmt.Println("got sub notif: ", v.Id)
			}
			count++
			if count >= 2 {
				break
			}
		}
		done <- true
	}()
	//waiting goroutines can exit
	<-done
	<-done
	<-done
	<-done
	rout1.Close()
	rout2.Close()
}

func test_logger() {
	rout1 := router.New(router.IntID(), 32, router.BroadcastPolicy, "router1", router.ScopeLocal)
	rout2 := router.New(router.IntID(), 32, router.BroadcastPolicy, "router2", router.ScopeLocal)
	rout1.Connect(rout2)
	chi1 := make(chan string)
	chi2 := make(chan string)
	chi3 := make(chan string)
	chiN := make(chan *router.ChanInfoMsg)
	cho := make(chan string)
	done := make(chan bool)
	bound := make(chan *router.BindEvent, 1)
	//subscribe to recver attach events
	rout1.AttachRecvChan(rout1.SysID(router.SubId), chiN)
	//when attaching sending chan, add a (chan BindEvent) for notifying recver connecting
	rout1.AttachSendChan(router.IntID(10), cho, bound)
	rout1.AttachRecvChan(router.IntID(10), chi1)
	rout2.AttachRecvChan(router.IntID(10), chi2)
	rout2.AttachRecvChan(router.IntID(10), chi3)
	//wait for some recvers connecting
	for {
		if (<-bound).Count == 2 {
			break
		}
	}
	go func() {
		cho <- "hello1"
		cho <- "hello2"
		cho <- "from router1/src"
		close(cho)
	}()
	go func() {
		for v := range chi1 {
			fmt.Println("router1/sink1 got: ", v)
		}
		done <- true
	}()
	go func() {
		for v := range chi2 {
			fmt.Println("router2/sink2 got: ", v)
		}
		done <- true
	}()
	go func() {
		i := 0
		for v := range chi3 {
			fmt.Println("router2/sink3 got: ", v)
			i++
			if i == 2 {
				rout2.DetachChan(router.IntID(10), chi3)
			}
		}
		done <- true
	}()
	go func() {
		count := 0
		for m := range chiN {
			for _, v := range m.Info {
				fmt.Println("got sub notif: ", v.Id)
			}
			count++
			if count >= 2 {
				break
			}
		}
		done <- true
	}()
	//waiting goroutines can exit
	<-done
	<-done
	<-done
	<-done
	rout1.Close()
	rout2.Close()
}

func test_remote_conn() {
	listening := make(chan string)
	srvdone := make(chan int)
	clidone := make(chan int)
	//run server
	go func() {
		l, err := net.Listen("tcp", ":0")
		listening <- l.Addr().String()
		conn, err := l.Accept()
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Println("server start")
		//
		rout1 := router.New(router.IntID(), 32, router.BroadcastPolicy /* , "router1", router.ScopeLocal*/ )
		_, err = rout1.ConnectRemote(conn, router.JsonMarshaling)
		if err != nil {
			fmt.Println(err)
		} else {
			cho := make(chan string)
			chi1 := make(chan string)
			bound := make(chan *router.BindEvent, 1)
			done := make(chan bool)
			rout1.AttachSendChan(router.IntID(10), cho, bound)
			rout1.AttachRecvChan(router.IntID(10), chi1)
			//wait for recvers connecting
			for {
				if (<-bound).Count == 2 {
					break
				}
			}
			go func() {
				cho <- "hello1"
				cho <- "hello2"
				cho <- "hello3"
				cho <- "hello4"
				cho <- "from router1/src"
				close(cho)
			}()
			go func() {
				for v := range chi1 {
					fmt.Println("router1/sink1 got: ", v)
				}
				done <- true
			}()
			<-done
		}
		<-clidone
		conn.Close()
		l.Close()
		srvdone <- 1
		rout1.Close()
	}()
	//run client
	go func() {
		addr := <-listening // wait for server to start
		dialaddr := "127.0.0.1" + addr[strings.LastIndex(addr, ":"):]
		conn, err := net.Dial("tcp", dialaddr)
		if err != nil {
			fmt.Println(err)
		}
		fmt.Println("client connect")
		//
		rout2 := router.New(router.IntID(), 32, router.BroadcastPolicy /* , "router2", router.ScopeLocal*/ )
		_, err = rout2.ConnectRemote(conn, router.JsonMarshaling)
		if err != nil {
			fmt.Println(err)
		} else {
			chi2 := make(chan string)
			chi3 := make(chan string)
			done := make(chan bool)
			rout2.AttachRecvChan(router.IntID(10), chi2)
			rout2.AttachRecvChan(router.IntID(10), chi3)
			go func() {
				for v := range chi2 {
					fmt.Println("router2/sink2 got: ", v)
				}
				done <- true
			}()
			go func() {
				i := 0
				for v := range chi3 {
					fmt.Println("router2/sink3 got: ", v)
					i++
					if i == 2 {
						rout2.DetachChan(router.IntID(10), chi3)
					}
				}
				done <- true
			}()
			<-done
			<-done
		}
		clidone <- 1
		conn.Close()
		rout2.Close()
	}()
	<-srvdone
}

func test_async_router() {
	//create a async router by setting buffer size to UnlimitedBuffer(-1) in router.New()
	rout1 := router.New(router.IntID(), router.UnlimitedBuffer, router.BroadcastPolicy /* , "router1", router.ScopeLocal*/ )
	rout2 := router.New(router.IntID(), router.UnlimitedBuffer, router.BroadcastPolicy /* , "router2", router.ScopeLocal*/ )
	rout1.Connect(rout2)
	chi1 := make(chan string)
	chi2 := make(chan string)
	chi3 := make(chan string)
	cho := make(chan string)
	bound := make(chan *router.BindEvent, 1)
	//when attaching sending chan, add a (chan BindEvent) for notifying recver connecting
	rout1.AttachSendChan(router.IntID(10), cho, bound)
	rout1.AttachRecvChan(router.IntID(10), chi1)
	rout2.AttachRecvChan(router.IntID(10), chi2)
	rout2.AttachRecvChan(router.IntID(10), chi3)
	//wait for some recvers connecting
	for {
		if (<-bound).Count == 2 {
			break
		}
	}
	//because we use async router, sending on those router attached channels
	//will never block. we do not need spawn goroutines in the following
	//go func() {
	cho <- "hello1"
	cho <- "hello2"
	cho <- "hello3"
	cho <- "from router1/src"
	close(cho)
	//}()
	//go func() {
	for v := range chi1 {
		fmt.Println("router1/sink1 got: ", v)
	}
	//}()
	//go func() {
	for v := range chi2 {
		fmt.Println("router2/sink2 got: ", v)
	}
	//}()
	//go func() {
	i := 0
	for v := range chi3 {
		fmt.Println("router2/sink3 got: ", v)
		i++
		if i == 2 {
			rout2.DetachChan(router.IntID(10), chi3)
		}
	}
	//}()
	rout1.Close()
	rout2.Close()
}

func test_flow_control() {
	listening := make(chan string)
	srvdone := make(chan int)
	clidone := make(chan int)
	//run server
	go func() {
		l, err := net.Listen("tcp", ":0")
		listening <- l.Addr().String()
		conn, err := l.Accept()
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Println("server start")
		rout1 := router.New(router.IntID(), 5, router.BroadcastPolicy/* , "router1", router.ScopeLocal*/ )
		//set FlowControl to turn on flow control on connection stream
		_, err = rout1.ConnectRemote(conn, router.GobMarshaling, router.WindowFlowController)
		if err != nil {
			fmt.Println(err)
		} else {
			cho := make(chan int)
			bound := make(chan *router.BindEvent, 1)
			done := make(chan bool)
			rout1.AttachSendChan(router.IntID(10), cho, bound)
			//wait for recvers connecting
			<-bound
			//start sending msgs
			go func() {
				for i := 0; i < 30; i++ {
					cho <- i
					fmt.Println("client sent: ", i)
					time.Sleep(1e8)
				}
				close(cho)
				done <- true
			}()
			<-done
		}
		<-clidone
		conn.Close()
		l.Close()
		srvdone <- 1
		rout1.Close()
	}()
	//run client
	go func() {
		addr := <-listening // wait for server to start
		dialaddr := "127.0.0.1" + addr[strings.LastIndex(addr, ":"):]
		conn, err := net.Dial("tcp", dialaddr)
		if err != nil {
			fmt.Println(err)
		}
		fmt.Println("client connect")
		rout2 := router.New(router.IntID(), 5, router.BroadcastPolicy/* , "router2", router.ScopeLocal*/ )
		//turn on flow control
		_, err = rout2.ConnectRemote(conn, router.GobMarshaling, router.WindowFlowController)
		if err != nil {
			fmt.Println(err)
		} else {
			chi2 := make(chan int)
			chi3 := make(chan int)
			done := make(chan bool)
			rout2.AttachRecvChan(router.IntID(10), chi2, 3)
			rout2.AttachRecvChan(router.IntID(10), chi3)
			go func() {
				for v := range chi2 {
					fmt.Println("router2/sink2 got: ", v)
					if v < 10 {
						time.Sleep(1e9)
					}
				}
				done <- true
			}()
			go func() {
				i := 0
				for v := range chi3 {
					fmt.Println("router2/sink3 got: ", v)
					i++
					if i == 2 {
						rout2.DetachChan(router.IntID(10), chi3)
					}
				}
				done <- true
			}()
			<-done
			<-done
		}
		clidone <- 1
		conn.Close()
		rout2.Close()
	}()
	<-srvdone
}

func main() {
	fmt.Println("-------test_IntId-------")
	test_IntId()
	fmt.Println("-------test_StrId-------")
	test_StrId()
	fmt.Println("--------test_PathId------")
	test_PathId()
	fmt.Println("--------test_MsgId------")
	test_MsgId()
	fmt.Println("--------test_notification------")
	test_notification()
	fmt.Println("-------test_local_conn-------")
	test_local_conn()
	fmt.Println("-------test_remote_conn-------")
	test_remote_conn()
	fmt.Println("-------test_async_router-------")
	test_async_router()
        /*
	fmt.Println("-------test_flow_control (internal buffer size = 5)-------")
	test_flow_control()
	 fmt.Println("-------test_logger-------")
	 test_logger()
         */
	fmt.Println("-------All Tests Done------")
}
