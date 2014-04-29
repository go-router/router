//
// Copyright (c) 2010 - 2012 Yigong Liu
//
// Distributed under New BSD License
//

package router

import (
	"fmt"
	"reflect"
	"sync"
)

type notifyChan struct {
	nchan   chan *ChanInfoMsg
	routCh  *RoutedChan
	asyncCh Channel
}

func (n *notifyChan) Close() {
	n.asyncCh.Close()
}

func newNotifyChan(idx int, r *routerImpl) *notifyChan {
	nc := new(notifyChan)
	nc.nchan = make(chan *ChanInfoMsg, r.defChanBufSize)
	nc.routCh, _ = r.AttachSendChan(r.NewSysID(idx, ScopeGlobal, MemberLocal), nc.nchan)
	//use async chan to send namespace notifications so that it is nonblocking
	nc.asyncCh = &asyncChan{Channel: nc.routCh}
	return nc
}

type notifier struct {
	router      *routerImpl
	notifyChans [4]*notifyChan
	closed      bool
	sync.Mutex
}

func newNotifier(s *routerImpl) *notifier {
	n := new(notifier)
	n.Lock()
	defer n.Unlock()
	n.router = s
	for i := 0; i < 4; i++ {
		n.notifyChans[i] = newNotifyChan(PubId+i, s)
	}
	return n
}

func (n *notifier) Close() {
	n.Lock()
	n.closed = true
	n.Unlock()
	for i := 0; i < 4; i++ {
		n.notifyChans[i].Close()
	}
}

func (n notifier) notify(idx int, info *ChanInfo) {
	n.Lock()
	defer n.Unlock()
	if n.closed {
		return
	}
	n.router.Log(LOG_INFO, fmt.Sprintf("notify: %v, %v", sysIdxString[idx], info.Id))
	nc := n.notifyChans[idx-PubId]
	if nc.routCh.NumPeers() > 0 {
		nc.asyncCh.Send(reflect.ValueOf(&ChanInfoMsg{Info: []*ChanInfo{info}}))
	}
}
