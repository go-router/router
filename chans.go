//
// Copyright (c) 2010 - 2012 Yigong Liu
//
// Distributed under New BSD License
//

package router

import (
	"container/list"
	"reflect"
	"sync"
)

/* 
 Channel interface defines functional api of Go's channel:
 based on reflect.Value's channel related method set
 allow programming "generic" channels with reflect.Value as msgs
 add some utility Channel types
*/
type Channel interface {
	ChanState
	Sender
	Recver
}

//common interface for msg senders
//and senders are responsible for closing channels
type Sender interface {
	Send(reflect.Value)
	TrySend(reflect.Value) bool
	Close()
}

//common interface for msg recvers
//recvers will check if channels are closed or not
type Recver interface {
	Recv() (reflect.Value, bool)
	TryRecv() (reflect.Value, bool)
}

//basic chan state
type ChanState interface {
	Type() reflect.Type
	Interface() interface{}
	IsNil() bool
	Cap() int
	Len() int
}

//SendChan: sending end of Channel (chan<-)
type SendChan interface {
	ChanState
	Sender
}

//RecvChan: recving end of Channel (<-chan)
type RecvChan interface {
	ChanState
	Recver
}

//GenericMsgChan: 
//mux msgs with diff ids into a common "chan *genericMsg"
//and expose Channel api
//for close, will send a special msg to mark close of one sender
//other part is responsible for counting active senders and close underlying
//channels if all senders close
type genericMsgChan struct {
	id      Id
	Channel //must have element type: *genericMsg
}

func newGenMsgChan(id Id, ch Channel) *genericMsgChan {
	//add check to make sure Channel of type: chan *genericMsg
	return &genericMsgChan{id, ch}
}

func newGenericMsgChan(id Id, ch chan *genericMsg) *genericMsgChan {
	return &genericMsgChan{id, reflect.ValueOf(ch)}
}

/* delegate following calls to embeded Channel's api
func (gch *genericMsgChan) Type() reflect.Type
func (gch *genericMsgChan) IsNil() bool
func (gch *genericMsgChan) Len() int
func (gch *genericMsgChan) Cap() int
func (gch *genericMsgChan) Recv() reflect.Value
func (gch *genericMsgChan) TryRecv() reflect.Value
*/

//overwrite the following calls for new behaviour
func (gch *genericMsgChan) Interface() interface{} { return gch }

func (gch *genericMsgChan) Close() {
	//sending chanCloseMsg, do not do the real closing of genericMsgChan 
	id1, _ := gch.id.Clone(NumScope, NumMembership) //special id to mark chan close
	gch.Channel.Send(reflect.ValueOf(&genericMsg{id1, nil}))
}

func (gch *genericMsgChan) Send(v reflect.Value) {
	gch.Channel.Send(reflect.ValueOf(&genericMsg{gch.id, v.Interface()}))
}

func (gch *genericMsgChan) TrySend(v reflect.Value) bool {
	return gch.Channel.TrySend(reflect.ValueOf(&genericMsg{gch.id, v.Interface()}))
}

/*
 asyncChan: a trivial async chan
 . unlimited internal buffering
 . senders never block
*/
type asyncChan struct {
	Channel
	sync.Mutex
	buffer *list.List //when buffer!=nil, background forwarding active
	closed bool
}

func (ac *asyncChan) Close() {
	ac.Lock()
	defer ac.Unlock()
	if ac.closed {
		return
	}
	ac.closed = true
	if ac.buffer == nil { //no background forwarder running
		ac.Channel.Close()
	}
}

func (ac *asyncChan) Interface() interface{} {
	return ac
}

func (ac *asyncChan) Cap() int {
	return UnlimitedBuffer //unlimited
}

func (ac *asyncChan) Len() int {
	l := ac.Channel.Len()
	ac.Lock()
	defer ac.Unlock()
	if ac.buffer == nil {
		return l
	}
	return l + ac.buffer.Len()
}

//for async chan, Send() never block because of unlimited buffering
func (ac *asyncChan) Send(v reflect.Value) {
	ac.Lock()
	defer ac.Unlock()
	if ac.closed {
		return
	}
	if ac.buffer == nil {
		if ac.Channel.TrySend(v) {
			return
		}
		ac.buffer = new(list.List)
		ac.buffer.PushBack(v)
		//spawn forwarder
		go func() {
			for {
				ac.Lock()
				l := ac.buffer
				if l.Len() == 0 {
					ac.buffer = nil
					if ac.closed {
						ac.Channel.Close()
					}
					ac.Unlock()
					return
				}
				ac.buffer = new(list.List)
				ac.Unlock()
				for e := l.Front(); e != nil; e = l.Front() {
					ac.Channel.Send(e.Value.(reflect.Value))
					l.Remove(e)
				}
			}
		}()
	} else {
		ac.buffer.PushBack(v)
	}
}

func (ac *asyncChan) TrySend(v reflect.Value) bool {
	ac.Send(v)
	return true
}

/*
msgHandlerChan is generic msg callback handler implementing SendChan interface.
before it becomes ready, the incoming msgs are buffered, 
when ready, buffered msgs and later msg are passed to callback handler.
callback should finish quickly, otherwise, senders could be blocked
*/
type msgHandlerChan struct {
	id     Id
	buffer *list.List
	ready  bool
	sync.Mutex
	handler func(m *genericMsg)
}

func newMsgHandlerChan(id Id, callback func(*genericMsg)) *msgHandlerChan {
	mhc := new(msgHandlerChan)
	mhc.id = id
	mhc.buffer = new(list.List)
	mhc.handler = callback
	return mhc
}

//the following implement Channel interface
func (mhc *msgHandlerChan) Send(v reflect.Value) {
	msg := &genericMsg{mhc.id, v.Interface()}
	mhc.Lock()
	if !mhc.ready {
		mhc.buffer.PushBack(msg)
	} else {
		mhc.Unlock()
		mhc.handler(msg)
		return
	}
	mhc.Unlock()
}

func (mhc *msgHandlerChan) TrySend(v reflect.Value) bool {
	mhc.Send(v)
	return true
}

// the following are most placeholders
func (mhc *msgHandlerChan) Close() {
	//do nothing
}

func (mhc *msgHandlerChan) Recv() (reflect.Value, bool) {
	//say it cannot be recved
	return reflect.Zero(reflect.TypeOf(&genericMsg{})), false
}

func (mhc *msgHandlerChan) TryRecv() (reflect.Value, bool) {
	//say it cannot be recved
	return reflect.Zero(reflect.TypeOf(&genericMsg{})), false
}

func (mhc *msgHandlerChan) Type() reflect.Type {
	//placeholder
	return reflect.TypeOf(mhc)
}

func (mhc *msgHandlerChan) Interface() interface{} {
	return mhc
}

func (mhc *msgHandlerChan) IsNil() bool {
	return false
}

func (mhc *msgHandlerChan) Cap() int {
	return UnlimitedBuffer
}

func (mhc *msgHandlerChan) Len() int {
	mhc.Lock()
	defer mhc.Unlock()
	if !mhc.ready {
		return mhc.buffer.Len()
	}
	return 0
}

//a special method to mark ready state and dispatch buffered msgs. should use Close()?
func (mhc *msgHandlerChan) Ready() {
	mhc.Lock()
	if !mhc.ready {
		mhc.ready = true
		if mhc.buffer.Len() > 0 {
			mhc.Unlock()
			for e := mhc.buffer.Front(); e != nil; e = mhc.buffer.Front() {
				mhc.handler(e.Value.(*genericMsg))
				mhc.buffer.Remove(e)
			}
			return
		}
	}
	mhc.Unlock()
}
