//
// Copyright (c) 2010 - 2012 Yigong Liu
//
// Distributed under New BSD License
//

package router

import (
	"errors"
	"reflect"
	"sync"
)

/*
 FlowControlPolicy: implement diff flow control protocols. 
 a flow control protocol has two parts:
    sender: which send msgs and recv acks from recver.
    recver: which recv msgs and send acks to sender.
 So besides wrapping a transport Channel (for send/recv msgs)
    FlowSender expose Ack(int) method to recv acks
    FlowRecver constructor will take as argument a ack(int) callback for sending acks.
 The following Windowing and XOnOff protocols are copied from Chapter 4 of 
 "Design And Validation Of Computer Protocols" by Gerard J. Holzmann.
*/
type FlowControlPolicy interface {
	NewFlowSender(ch Channel, args ...interface{}) (FlowSender, error)
	NewFlowRecver(ch Channel, ack func(int), args ...interface{}) (FlowRecver, error)
	//Stringer interface, return name of FlowControlPolicy
	String() string
}

type FlowSender interface {
	Channel
	Ack(int)
}

type FlowRecver interface {
	Channel
}

/*
 WindowFlowController: simple window flow control protocol for lossless transport 
 the transport Channel between Sender, Recver should have capacity >= expected credit
 Figure 4.5 in Gerard's book
*/
type WindowFlowControlPolicy byte

var WindowFlowController FlowControlPolicy = WindowFlowControlPolicy(0)

func (wfc WindowFlowControlPolicy) NewFlowSender(ch Channel, args ...interface{}) (FlowSender, error) {
	credit, ok := args[0].(int)
	if !ok {
		return nil, errors.New("WindowFlowController: invalid arguments in constructor")
	}
	fc := new(windowFlowChanSender)
	fc.Channel = ch
	if credit <= 0 {
		return nil, errors.New("Flow Controlled Chan: invalid credit")
	}
	fc.credit = credit
	fc.creditCap = credit
	//for unlimited buffer, ch.Cap() return UnlimitedBuffer(-1)
	if ch.Cap() != UnlimitedBuffer && ch.Cap() < credit {
		return nil, errors.New("Flow Controlled Chan: do not have enough buffering")
	}
	fc.cond = sync.NewCond(&fc.lock)
	return fc, nil
}

func (wfc WindowFlowControlPolicy) NewFlowRecver(ch Channel, ack func(int), args ...interface{}) (FlowRecver, error) {
	return &windowFlowChanRecver{ch, ack}, nil
}

func (wfc WindowFlowControlPolicy) String() string {
	return "WindowFlowControlPolicy"
}

type windowFlowChanSender struct {
	Channel
	creditCap int
	credit    int
	cond      *sync.Cond
	lock      sync.Mutex //protect credit/creditChan change
}

func (fc *windowFlowChanSender) Send(v reflect.Value) {
	fc.lock.Lock()
	for fc.credit <= 0 {
		fc.cond.Wait()
	}
	fc.credit--
	fc.lock.Unlock()
	fc.Channel.Send(v)
}

func (fc *windowFlowChanSender) TrySend(v reflect.Value) bool {
	fc.lock.Lock()
	if fc.credit <= 0 {
		fc.lock.Unlock()
		return false
	}
	fc.credit--
	fc.lock.Unlock()
	return fc.Channel.TrySend(v)
}

func (fc *windowFlowChanSender) Len() int {
	fc.lock.Lock()
	defer fc.lock.Unlock()
	return fc.creditCap - fc.credit
}

func (fc *windowFlowChanSender) Cap() int {
	return fc.creditCap
}

func (fc *windowFlowChanSender) Ack(n int) {
	fc.lock.Lock()
	fc.credit += n
	if fc.credit > fc.creditCap {
		fc.credit = fc.creditCap
	}
	fc.cond.Broadcast()
	fc.lock.Unlock()
}

func (fc *windowFlowChanSender) Interface() interface{} {
	return fc
}

type windowFlowChanRecver struct {
	Channel
	ack func(int)
}

func (fc *windowFlowChanRecver) Recv() (v reflect.Value, ok bool) {
	v, ok = fc.Channel.Recv()
	if ok {
		fc.ack(1)
	}
	return
}

func (fc *windowFlowChanRecver) TryRecv() (v reflect.Value, ok bool) {
	v, ok = fc.Channel.TryRecv()
	if v.IsValid() && ok {
		fc.ack(1)
	}
	return
}

func (fc *windowFlowChanRecver) Interface() interface{} {
	return fc
}

/*
 X-on/X-off protocol
 Figure 4.2 and Figure 4.3 in Gerard's book
*/
type XOnOffFlowControlPolicy struct {
	max float32
	min float32
}

var XOnOffFlowController FlowControlPolicy = &XOnOffFlowControlPolicy{0.75, 0.25}

func (fcp *XOnOffFlowControlPolicy) NewFlowSender(ch Channel, args ...interface{}) (FlowSender, error) {
	xfs := &xOnOffFlowSender{}
	xfs.Channel = ch
	xfs.active = true
	xfs.cond = sync.NewCond(&xfs.lock)
	return xfs, nil
}

func (fcp *XOnOffFlowControlPolicy) NewFlowRecver(ch Channel, ack func(int), args ...interface{}) (FlowRecver, error) {
	xfr := &xOnOffFlowRecver{}
	//since fast sender could overflow recver before recving "OFF" signal from recver,
	//we attach an async chan to allow unlimited buffering
	xfr.Channel = &asyncChan{Channel: ch}
	xfr.sendActive = true
	xfr.ack = ack
	xfr.maxCount = int(fcp.max * float32(ch.Cap()))
	xfr.minCount = int(fcp.min * float32(ch.Cap()))
	if xfr.minCount < 1 {
		xfr.minCount = 1
	}
	return xfr, nil
}

func (fcp *XOnOffFlowControlPolicy) String() string {
	return "XOnOffFlowControlPolicy"
}

type xOnOffFlowSender struct {
	Channel
	active bool
	lock   sync.Mutex
	cond   *sync.Cond
}

func (fc *xOnOffFlowSender) Send(v reflect.Value) {
	fc.lock.Lock()
	for !fc.active {
		fc.cond.Wait()
	}
	fc.lock.Unlock()
	fc.Channel.Send(v)
}

func (fc *xOnOffFlowSender) TrySend(v reflect.Value) bool {
	fc.lock.Lock()
	act := fc.active
	fc.lock.Unlock()
	if !act {
		return false
	}
	return fc.Channel.TrySend(v)
}

func (fc *xOnOffFlowSender) Ack(n int) {
	fc.lock.Lock()
	defer fc.lock.Unlock()
	if n > 0 { //resume
		fc.active = true
		fc.cond.Broadcast()
	} else { //suspend
		fc.active = false
	}
}

func (fc *xOnOffFlowSender) Interface() interface{} {
	return fc
}

type xOnOffFlowRecver struct {
	Channel
	sendActive bool
	lock       sync.Mutex //prot sendActive
	maxCount   int
	minCount   int
	ack        func(int)
}

func (fc *xOnOffFlowRecver) Send(v reflect.Value) {
	needAck := false
	fc.lock.Lock()
	if fc.Channel.Len() > fc.maxCount && fc.sendActive {
		needAck = true
		fc.sendActive = false
	}
	fc.lock.Unlock()
	if needAck {
		fc.ack(-1) //ask xOnOffFlowSender to suspend
	}
	fc.Channel.Send(v)
}

func (fc *xOnOffFlowRecver) TrySend(v reflect.Value) bool {
	needAck := false
	fc.lock.Lock()
	if fc.Channel.Len() > fc.maxCount && fc.sendActive {
		needAck = true
		fc.sendActive = false
	}
	fc.lock.Unlock()
	if needAck {
		fc.ack(-1) //ask xOnOffFlowSender to suspend
	}
	return fc.Channel.TrySend(v)
}

func (fc *xOnOffFlowRecver) Recv() (v reflect.Value, ok bool) {
	v, ok = fc.Channel.Recv()
	needAck := false
	fc.lock.Lock()
	if ok && fc.Channel.Len() < fc.minCount && !fc.sendActive {
		needAck = true
		fc.sendActive = true
	}
	fc.lock.Unlock()
	if needAck {
		fc.ack(1) //ask xOnOffFlowSender to resume
	}
	return
}

func (fc *xOnOffFlowRecver) TryRecv() (v reflect.Value, ok bool) {
	v, ok = fc.Channel.TryRecv()
	needAck := false
	fc.lock.Lock()
	if ok && fc.Channel.Len() < fc.minCount && !fc.sendActive {
		needAck = true
		fc.sendActive = true
	}
	fc.lock.Unlock()
	if needAck {
		fc.ack(1) //ask xOnOffFlowSender to resume
	}
	return
}

func (fc *xOnOffFlowRecver) Interface() interface{} {
	return fc
}
