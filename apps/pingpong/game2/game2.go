package main

import (
	"flag"
	"fmt"
	"github.com/go-router/router"
	"strconv"
)

var showPingPong bool = true

//Msg instances are bounced between Pinger and Ponger as balls
type Msg struct {
	Data  string
	Count int
}

//pinger: send to ping chan, recv from pong chan
type Pinger struct {
	//Pinger's public interface
	pingChan chan<- *Msg
	pongChan <-chan *Msg
	done     chan<- bool
	//Pinger's private state
	numRuns int //how many times should we ping-pong
}

func (p *Pinger) Run() {
	for v := range p.pongChan {
		if showPingPong {
			fmt.Println("Pinger recv: ", v)
		}
		if v.Count > p.numRuns {
			break
		}
		p.pingChan <- &Msg{"hello from Pinger", v.Count + 1}
	}
	close(p.pingChan)
	p.done <- true
}

func newPinger(rot router.Router, done chan<- bool, numRuns int) {
	//attach chans to router
	pingChan := make(chan *Msg)
	pongChan := make(chan *Msg)
	rot.AttachSendChan(router.StrID("ping"), pingChan)
	rot.AttachRecvChan(router.StrID("pong"), pongChan)
	//start pinger
	ping := &Pinger{pingChan, pongChan, done, numRuns}
	go ping.Run()
}

//ponger: send to pong chan, recv from ping chan
type Ponger struct {
	//Ponger's public interface
	pongChan chan<- *Msg
	pingChan <-chan *Msg
	done     chan<- bool
	//Ponger's private state
}

func (p *Ponger) Run() {
	p.pongChan <- &Msg{"hello from Ponger", 0} //initiate ping-pong
	for v := range p.pingChan {
		if showPingPong {
			fmt.Println("Ponger recv: ", v)
		}
		p.pongChan <- &Msg{"hello from Ponger", v.Count + 1}
	}
	close(p.pongChan)
	p.done <- true
}

func newPonger(rot router.Router, done chan<- bool) {
	//attach chans to router
	pingChan := make(chan *Msg)
	pongChan := make(chan *Msg)
	rot.AttachSendChan(router.StrID("pong"), pongChan)
	rot.AttachRecvChan(router.StrID("ping"), pingChan)
	//start ponger
	pong := &Ponger{pongChan, pingChan, done}
	go pong.Run()
}

func main() {
	flag.Parse()
	if flag.NArg() < 1 {
		fmt.Println("Usage: game2 num_runs hideTrace")
		return
	} else if flag.NArg() > 1 {
		showPingPong = false
	}
	numRuns, _ := strconv.Atoi(flag.Arg(0))
	//alloc a router to connect Pinger and Ponger
	rot := router.New(router.StrID(), 32, router.BroadcastPolicy)
	done := make(chan bool)
	//hook up Pinger and Ponger
	newPonger(rot, done)
	newPinger(rot, done, numRuns)
	//wait for ping-pong to finish
	<-done
	<-done
}
