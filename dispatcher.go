//
// Copyright (c) 2010 - 2012 Yigong Liu
//
// Distributed under New BSD License
//

package router

import (
	"math/rand"
	"reflect"
	"time"
)

//
// The programming of Dispatchers:
// 1. do not depend on specific chan types
// 2. messages sent are represented as reflect.Value
// 3. receivers are array of RoutedChans with Channel interface of Send()/Recv()/...
//
//DispatchPolicy is used to generate concrete dispatcher instances.
//For the kind of dispatcher which has no internal state, the same instance
//can be returned.
type DispatchPolicy interface {
	NewDispatcher() Dispatcher
}

//Dispatcher is the common interface of all dispatchers
type Dispatcher interface {
	Dispatch(v reflect.Value, recvers []*RoutedChan)
}

//DispatchFunc is a wrapper to convert a plain function into a dispatcher
type DispatchFunc func(v reflect.Value, recvers []*RoutedChan)

func (f DispatchFunc) Dispatch(v reflect.Value, recvers []*RoutedChan) {
	f(v, recvers)
}

type PolicyFunc func() Dispatcher

func (f PolicyFunc) NewDispatcher() Dispatcher {
	return f()
}

/*
 simple dispatching algorithms which are the base of more practical ones:
 broadcast, roundrobin, etc
*/

//Simple broadcast is a plain function
func Broadcast(v reflect.Value, recvers []*RoutedChan) {
	for _, rc := range recvers {
		rc.Send(v)
	}
}

//BroadcastPolicy is used to generate broadcast dispatcher instances
var BroadcastPolicy DispatchPolicy = PolicyFunc(func() Dispatcher { return DispatchFunc(Broadcast) })

//KeepLastBroadcast never block. if running out of Chan buffer, drop old items and keep the latest items
func KeepLatestBroadcast(v reflect.Value, recvers []*RoutedChan) {
	for _, rc := range recvers {
		for !rc.TrySend(v) {
			rc.TryRecv()
		}
	}
}

//KeepLatestBroadcastPolicy is used to generate KeepLatest broadcast dispatcher instances
var KeepLatestBroadcastPolicy DispatchPolicy = PolicyFunc(func() Dispatcher { return DispatchFunc(KeepLatestBroadcast) })

//Roundrobin dispatcher will keep the "next" index as its state
type Roundrobin int

func NewRoundrobin() *Roundrobin { return new(Roundrobin) }

func (r *Roundrobin) Dispatch(v reflect.Value, recvers []*RoutedChan) {
	start := *r
	for {
		rc := recvers[*r]
		*r = (*r + 1) % Roundrobin(len(recvers))
		if rc.TrySend(v) {
			break
		}
		if *r == start {
			break
		}
	}
}

//RoundRobinPolicy is ued to generate roundrobin dispatchers
var RoundRobinPolicy DispatchPolicy = PolicyFunc(func() Dispatcher { return NewRoundrobin() })

//Random dispatcher
type RandomDispatcher rand.Rand

func NewRandomDispatcher() *RandomDispatcher {
	return (*RandomDispatcher)(rand.New(rand.NewSource(time.Now().UnixNano())))
}

func (rd *RandomDispatcher) Dispatch(v reflect.Value, recvers []*RoutedChan) {
	for {
		ind := ((*rand.Rand)(rd)).Intn(len(recvers))
		rc := recvers[ind]
		rc.Send(v)
		break
	}
}

//RandomPolicy is used to generate random dispatchers
var RandomPolicy DispatchPolicy = PolicyFunc(func() Dispatcher { return NewRandomDispatcher() })
