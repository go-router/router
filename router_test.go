//
// Copyright (c) 2010 - 2012 Yigong Liu
//
// Distributed under New BSD License
//

package router

import (
	"net"
	"strings"
	"testing"
)

func TestStrId(t *testing.T) {
	rout := New(StrID(), 32, BroadcastPolicy)
	chi1 := make(chan string)
	chi2 := make(chan string)
	cho := make(chan string)
	done := make(chan bool)
	_, err := rout.AttachSendChan(StrID("test"), cho)
	if err != nil {
		t.Fatal("TestStrId failed at router.AttachSendChan()")
	}
	_, err = rout.AttachRecvChan(StrID("test"), chi1)
	if err != nil {
		t.Fatal("TestStrId failed at router.AttachRecvChan-chi1")
	}
	_, err = rout.AttachRecvChan(StrID("test"), chi2)
	if err != nil {
		t.Fatal("TestStrId failed at router.AttachRecvChan-chi2")
	}
	go func() {
		cho <- "hello"
		close(cho)
	}()
	go func() {
		for v := range chi1 {
			if v != "hello" {
				t.Errorf("TestStrId failed at chi1, expected [hello], recv : %v", v)
			}
		}
		done <- true
	}()
	go func() {
		for v := range chi2 {
			if v != "hello" {
				t.Errorf("TestStrId failed at chi2, expected [hello], recv : %v", v)
			}
		}
		done <- true
	}()
	<-done
	<-done
}

func TestRemoteConn(t *testing.T) {
	listening := make(chan string)
	srvdone := make(chan int)
	clidone := make(chan int)
	//run server
	go func() {
		l, err := net.Listen("tcp", ":0")
		listening <- l.Addr().String()
		conn, err := l.Accept()
		if err != nil {
			t.Fatal(err)
		}
		rout1 := New(IntID(), 32, BroadcastPolicy /*, "router1", ScopeLocal*/ )
		_, err = rout1.ConnectRemote(conn, GobMarshaling)
		if err != nil {
			t.Fatal(err)
		} else {
			cho := make(chan string)
			chi1 := make(chan string)
			bound := make(chan *BindEvent, 1)
			done := make(chan bool)
			_, err = rout1.AttachSendChan(IntID(10), cho, bound)
			if err != nil {
				t.Fatal("TestRemoteConn failed at server : router.AttachSendChan()")
			}
			_, err = rout1.AttachRecvChan(IntID(10), chi1)
			if err != nil {
				t.Fatal("TestRemoteConn failed at server : router.AttachRecvChan()")
			}
			//wait for recvers connecting
			for {
				if (<-bound).Count == 2 {
					break
				}
			}
			go func() {
				cho <- "hello"
				close(cho)
			}()
			go func() {
				for v := range chi1 {
					if v != "hello" {
						t.Errorf("TestStrId failed at chi1, expected [hello], recv : %v", v)
					}
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
			t.Fatal(err)
		}
		rout2 := New(IntID(), 32, BroadcastPolicy /*, "router2", ScopeLocal*/ )
		_, err = rout2.ConnectRemote(conn, GobMarshaling)
		if err != nil {
			t.Fatal(err)
		} else {
			chi2 := make(chan string)
			chi3 := make(chan string)
			done := make(chan bool)
			_, err = rout2.AttachRecvChan(IntID(10), chi2)
			if err != nil {
				t.Fatal("TestRemoteConn failed at client chi2 : router.AttachRecvChan()")
			}
			_, err = rout2.AttachRecvChan(IntID(10), chi3)
			if err != nil {
				t.Fatal("TestRemoteConn failed at client chi3 : router.AttachRecvChan()")
			}
			go func() {
				for v := range chi2 {
					if v != "hello" {
						t.Errorf("TestStrId failed at chi2, expected [hello], recv : %v", v)
					}
				}
				done <- true
			}()
			go func() {
				for v := range chi3 {
					if v != "hello" {
						t.Errorf("TestStrId failed at chi3, expected [hello], recv : %v", v)
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
